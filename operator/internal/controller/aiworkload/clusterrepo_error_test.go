/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aiworkload

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// clusterRepoErrorScheme registers every type the blueprint reconcile touches.
func clusterRepoErrorScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	scheme.AddKnownTypeWithName(helmOpGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "HelmOpList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeploymentList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(clusterRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})
	return scheme
}

func newBlueprintWorkload(created time.Time) *aiplatformv1alpha1.AIWorkload {
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "aif-operator"},
	}
	w.CreationTimestamp = metav1.NewTime(created)
	w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployFleetBundle
	w.Spec.TargetNamespace = "install-ns"
	w.Spec.TargetClusters = []string{"local"}
	w.Spec.FleetBundleNames = []string{"wl-app"} // pre-populated so Step 2 is skipped
	w.Spec.Source = aiplatformv1alpha1.AIWorkloadSource{
		SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
		Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "mini", Version: "1.0.0"},
	}
	return w
}

func newBlueprintCR(repo string) *aiplatformv1alpha1.Blueprint {
	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: bpCRName("mini", "1.0.0")},
	}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: repo, ChartName: "app", ChartVersion: "1.0.0", Vendor: "suse"},
	}
	return bp
}

func TestReconcileBlueprintStatus_MissingClusterRepo_SetsConditionAndRequeues(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now()) // within the grace window
	bp := newBlueprintCR("application-collection")
	// Note: no ClusterRepo object is created.
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	result, err := r.reconcileBlueprintStatus(context.Background(), w)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected RequeueAfter 30s, got %v", result.RequeueAfter)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil {
		t.Fatalf("expected a %q condition", conditionTypeReady)
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected condition status False, got %v", cond.Status)
	}
	if cond.Reason != reasonClusterRepoNotReady {
		t.Errorf("expected reason %q, got %q", reasonClusterRepoNotReady, cond.Reason)
	}
	if !strings.Contains(cond.Message, "application-collection") {
		t.Errorf("expected message to name the repo, got %q", cond.Message)
	}
	// Within the grace window the phase stays Pending, not Failed.
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhasePending {
		t.Errorf("expected phase Pending during grace window, got %v", w.Status.Phase)
	}
}

func TestReconcileBlueprintStatus_MissingClusterRepo_FailsAfterGrace(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now().Add(-10 * time.Minute)) // past the grace window
	bp := newBlueprintCR("application-collection")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	if _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseFailed {
		t.Errorf("expected phase Failed after grace window, got %v", w.Status.Phase)
	}
}

func TestReconcileBlueprintStatus_RepoPresent_SetsReadyTrue(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now())
	bp := newBlueprintCR("application-collection")
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	if _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True condition, got %+v", cond)
	}
	if cond.Reason != reasonReconciled {
		t.Errorf("expected reason %q, got %q", reasonReconciled, cond.Reason)
	}
}

func TestReconcile_MissingClusterRepo_PersistsCondition(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now())
	w.Finalizers = []string{aiWorkloadFinalizer} // skip the finalizer-add requeue
	// Seed a distinct ObservedGeneration so we can prove the requeue path leaves
	// it untouched (a broken guard would overwrite it with the object Generation).
	const seededObservedGen int64 = 7
	w.Status.ObservedGeneration = seededObservedGen
	bp := newBlueprintCR("application-collection")
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(w, bp).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).
		Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "wl", Namespace: "aif-operator"},
	})
	if err != nil {
		t.Fatalf("expected nil error from Reconcile, got %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected RequeueAfter 30s, got %v", result.RequeueAfter)
	}

	var got aiplatformv1alpha1.AIWorkload
	if err := c.Get(context.Background(), types.NamespacedName{Name: "wl", Namespace: "aif-operator"}, &got); err != nil {
		t.Fatalf("get workload: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected persisted Ready=False condition, got %+v", cond)
	}
	// The guard must NOT advance ObservedGeneration on the requeue path — it must
	// remain the seeded value. A broken guard would overwrite it with the object
	// Generation, so this catches a regression regardless of what Generation is.
	if got.Status.ObservedGeneration != seededObservedGen {
		t.Errorf("ObservedGeneration must be unchanged on the requeue path (got=%d, want %d)", got.Status.ObservedGeneration, seededObservedGen)
	}
}

// forbiddenClusterRepoReconciler builds a reconciler whose ClusterRepo reads
// fail with Forbidden — an error matching none of the four sentinels, so it
// exercises the generic fall-through.
func forbiddenClusterRepoReconciler(t *testing.T, scheme *kruntime.Scheme, objs ...client.Object) *AIWorkloadReconciler {
	t.Helper()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(objs...).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*unstructured.Unstructured); ok {
					gvk := obj.GetObjectKind().GroupVersionKind()
					if gvk.Group == "catalog.cattle.io" && gvk.Kind == "ClusterRepo" {
						return apierrors.NewForbidden(schema.GroupResource{Group: gvk.Group, Resource: "clusterrepos"}, key.Name, nil)
					}
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()
	return &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}
}

func TestReconcileBlueprintStatus_ForbiddenClusterRepo_PropagatesError(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now())
	bp := newBlueprintCR("application-collection")
	r := forbiddenClusterRepoReconciler(t, scheme, w, bp)

	result, err := r.reconcileBlueprintStatus(context.Background(), w)
	if err == nil {
		t.Fatalf("expected a propagated error, got nil")
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue on non-sentinel error, got %v", result.RequeueAfter)
	}
	// The error matches no sentinel, but the object must still say it is not
	// ready: returning bare would leave whatever the last pass wrote in place.
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected Ready=False on a propagated error, got %+v", cond)
	}
	if cond.Reason != "ComponentReconcileFailed" {
		t.Errorf("reason = %q, want ComponentReconcileFailed", cond.Reason)
	}
	if !strings.Contains(cond.Message, "application-collection") {
		t.Errorf("expected message to name the repo, got %q", cond.Message)
	}
}

// A generic (non-sentinel) failure used to return bare, leaving the previous
// pass's Ready=True / "Component bundles reconciled" on the object: the workload
// retried forever while the UI showed it as reconciled and healthy.
func TestReconcileBlueprintStatus_GenericError_ClearsStaleReadyTrue(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now().Add(-10 * time.Minute)) // past the grace window
	bp := newBlueprintCR("application-collection")
	// Seed the state a previously-healthy workload would carry.
	setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionTrue, reasonReconciled,
		"Component bundles reconciled", w.Generation)
	w.Status.Phase = aiplatformv1alpha1.AIWorkloadPhaseRunning
	r := forbiddenClusterRepoReconciler(t, scheme, w, bp)

	if _, err := r.reconcileBlueprintStatus(context.Background(), w); err == nil {
		t.Fatal("expected the generic error to be propagated")
	}

	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil {
		t.Fatalf("expected a %q condition", conditionTypeReady)
	}
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("stale Ready=True survived a failed reconcile: %+v", cond)
	}
	if cond.Message == "Component bundles reconciled" {
		t.Errorf("stale message survived: %q", cond.Message)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseFailed {
		t.Errorf("phase = %v, want Failed", w.Status.Phase)
	}
}

// An error can carry a whole HTML page from an ingress; the CRD caps
// condition.message at 32768 bytes.
func TestTruncateForCondition(t *testing.T) {
	if got := truncateForCondition("short"); got != "short" {
		t.Errorf("short message was altered: %q", got)
	}
	long := strings.Repeat("x", 64<<10)
	got := truncateForCondition(long)
	if len(got) >= len(long) {
		t.Fatalf("len = %d, want it bounded well under %d", len(got), len(long))
	}
	if !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("truncation is not signalled in the message: %q", got[len(got)-32:])
	}
}
