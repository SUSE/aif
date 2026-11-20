package aiworkload

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

var heldWorkloadKey = types.NamespacedName{Name: "wl", Namespace: "aif-operator"}

// startOperation journals an in-flight op on the stored workload and seeds its status.
func startOperation(t *testing.T, c client.Client, opType string, requestedAt time.Time, seed func(*aiplatformv1alpha1.AIWorkloadStatus)) {
	t.Helper()
	ctx := context.Background()
	var w aiplatformv1alpha1.AIWorkload
	if err := c.Get(ctx, heldWorkloadKey, &w); err != nil {
		t.Fatal(err)
	}
	inFlightVersionOp(&w, opType, w.Spec.Source.Blueprint.Version, "", requestedAt)
	if err := c.Update(ctx, &w); err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		seed(&w.Status)
		if err := c.Status().Update(ctx, &w); err != nil {
			t.Fatal(err)
		}
	}
}

func helmOpFor(t *testing.T, c client.Client, component string) *unstructured.Unstructured {
	t.Helper()
	ho := &unstructured.Unstructured{}
	ho.SetGroupVersionKind(helmOpGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName("wl", component)}, ho); err != nil {
		t.Fatalf("get HelmOp %s: %v", component, err)
	}
	return ho
}

// putErrAppliedBundleDeployment stands in for Fleet failing to apply the bundle on "local".
func putErrAppliedBundleDeployment(t *testing.T, r *AIWorkloadReconciler, bundleName string) {
	t.Helper()
	r.Scheme.AddKnownTypeWithName(bundleDeploymentGVK, &unstructured.Unstructured{})
	bd := &unstructured.Unstructured{Object: map[string]any{}}
	bd.SetGroupVersionKind(bundleDeploymentGVK)
	bd.SetNamespace("cluster-fleet-local-local")
	bd.SetName(bundleName)
	bd.SetLabels(map[string]string{
		"fleet.cattle.io/bundle-name":      bundleName,
		"fleet.cattle.io/bundle-namespace": "fleet-local",
		"fleet.cattle.io/cluster":          "local",
	})
	_ = unstructured.SetNestedField(bd.Object, "d1", "spec", "deploymentID")
	_ = unstructured.SetNestedField(bd.Object, "d1", "status", "appliedDeploymentID")
	_ = unstructured.SetNestedField(bd.Object, "ErrApplied", "status", "display", "state")
	putBundleDeployment(t, r, bd)
}

func activeOperation(t *testing.T, w *aiplatformv1alpha1.AIWorkload) *aiplatformv1alpha1.AIWorkloadOperation {
	t.Helper()
	if _, ok := readJournal(w); ok {
		t.Fatal("terminal operation must clear the journal")
	}
	if w.Status.ActiveOperation == nil {
		t.Fatal("expected a terminal activeOperation")
	}
	return w.Status.ActiveOperation
}

func TestReconcile_HeldUpgradeTimesOut(t *testing.T) {
	r, c := newHeldDeliveryReconciler(t, "local", false)
	reconcileHeldWorkload(t, r)
	startOperation(t, c, aiplatformv1alpha1.OperationTypeUpgrade, time.Now().Add(-defaultOperationDeadline-time.Minute), nil)

	res, got := reconcileHeldWorkload(t, r)
	if op := activeOperation(t, got); op.State != aiplatformv1alpha1.OperationStateFailed || op.Reason != reasonOperationTimedOut {
		t.Fatalf("expected Failed/TimedOut, got %+v", op)
	}
	if res != (ctrl.Result{RequeueAfter: dependencyHoldRequeue}) {
		t.Fatalf("expected the hold requeue, got %+v", res)
	}
}

func TestReconcile_HeldUpgradeFailsOnFailedComponent(t *testing.T) {
	r, c := newHeldDeliveryReconciler(t, "local", false)
	reconcileHeldWorkload(t, r)
	putBundle(t, r, helmOpFor(t, c, "control-plane"), 0)
	putErrAppliedBundleDeployment(t, r, blueprintBundleName("wl", "control-plane"))
	startOperation(t, c, aiplatformv1alpha1.OperationTypeUpgrade, time.Now().Add(-time.Minute), nil)

	_, got := reconcileHeldWorkload(t, r)
	if op := activeOperation(t, got); op.State != aiplatformv1alpha1.OperationStateFailed || op.Reason == reasonOperationTimedOut {
		t.Fatalf("expected Failed on the failed component, not a timeout, got %+v", op)
	}
}

func TestReconcile_HeldRollbackNeverSucceeds(t *testing.T) {
	r, c := newHeldDeliveryReconciler(t, "local", false)
	reconcileHeldWorkload(t, r)
	startOperation(t, c, aiplatformv1alpha1.OperationTypeRollback, time.Now().Add(-time.Minute), func(s *aiplatformv1alpha1.AIWorkloadStatus) {
		s.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{
			Version: "1.0.0", RenderDigest: "sha256:old", CertifiedAt: metav1.NewTime(time.Now().Add(-time.Hour)),
		}
	})

	res, got := reconcileHeldWorkload(t, r)
	if _, ok := readJournal(got); !ok {
		t.Fatalf("held rollback must stay in progress, got %+v", got.Status.ActiveOperation)
	}
	if res.RequeueAfter == 0 || res.RequeueAfter > dependencyHoldRequeue {
		t.Fatalf("expected a requeue no later than the hold, got %+v", res)
	}
}

// An early requeue (here ClusterRepoNotReady) leaves the previous render's cells in
// status; an operation must not be judged against them.
func TestReconcile_EarlyRequeueKeepsOperationInProgress(t *testing.T) {
	r, c := newHeldDeliveryReconciler(t, "local", false)
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	if err := c.Delete(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	startOperation(t, c, aiplatformv1alpha1.OperationTypeUpgrade, time.Now().Add(-time.Minute), func(s *aiplatformv1alpha1.AIWorkloadStatus) {
		s.ComponentStatuses = []aiplatformv1alpha1.AIWorkloadComponentStatus{
			{ComponentName: "control-plane", ClusterID: "local", Phase: aiplatformv1alpha1.AIWorkloadClusterPhaseFailed},
		}
	})

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: heldWorkloadKey})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var got aiplatformv1alpha1.AIWorkload
	if err := c.Get(context.Background(), heldWorkloadKey, &got); err != nil {
		t.Fatal(err)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, conditionTypeReady); cond == nil || cond.Reason != reasonClusterRepoNotReady {
		t.Fatalf("expected Ready reason %s, got %+v", reasonClusterRepoNotReady, cond)
	}
	if _, ok := readJournal(&got); !ok {
		t.Fatalf("operation must stay in progress on stale cells, got %+v", got.Status.ActiveOperation)
	}
	if res != (ctrl.Result{RequeueAfter: 30 * time.Second}) {
		t.Fatalf("expected the ClusterRepoNotReady requeue, got %+v", res)
	}
}
