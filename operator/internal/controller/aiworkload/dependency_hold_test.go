package aiworkload

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/naming"
)

type holdFixture struct {
	t      *testing.T
	r      *AIWorkloadReconciler
	w      *aiplatformv1alpha1.AIWorkload
	remote string // GitOps only
}

// newHoldFixture builds a Blueprint with "cluster" dependsOn "control-plane"
// on an HTTP repo, targeting only the local cluster.
func newHoldFixture(t *testing.T, clusterDeps []string, overrides []aiplatformv1alpha1.ComponentValueOverride) *holdFixture {
	t.Helper()
	return newHoldFixtureFor(t, aiplatformv1alpha1.AIWorkloadDeployFleetBundle, clusterDeps, overrides)
}

func newHoldFixtureFor(t *testing.T, strategy aiplatformv1alpha1.AIWorkloadDeployStrategy, clusterDeps []string, overrides []aiplatformv1alpha1.ComponentValueOverride) *holdFixture {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)
	addBundleScheme(scheme)

	w := newBlueprintWorkload(time.Now())
	w.Spec.DeployStrategy = strategy
	w.Spec.Source.Blueprint = &aiplatformv1alpha1.BlueprintSource{Name: "hold", Version: "1.0.0"}
	w.Spec.ComponentValues = overrides

	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: naming.BlueprintCRName("hold", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: "application-collection", ChartName: "cluster", ChartVersion: "1.0.0", Vendor: "suse", DependsOn: clusterDeps},
		{ChartRepo: "application-collection", ChartName: "control-plane", ChartVersion: "1.0.0", Vendor: "suse"},
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	objs := []client.Object{w, bp, repo}
	var remote string
	if strategy == aiplatformv1alpha1.AIWorkloadDeployGitOps {
		remote = newBlueprintGitOpsRemote(t)
		objs = append(objs, &aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: "aif-operator"},
			Spec: aiplatformv1alpha1.SettingsSpec{
				Fleet: aiplatformv1alpha1.FleetSettings{GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: remote, Branch: "main"}},
			},
		})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &holdFixture{t: t, r: &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}, w: w, remote: remote}
}

func (f *holdFixture) reconcile() {
	f.t.Helper()
	if _, _, err := f.r.reconcileBlueprintStatus(context.Background(), f.w); err != nil {
		f.t.Fatalf("reconcileBlueprintStatus: %v", err)
	}
}

func (f *holdFixture) helmOp(component string) *unstructured.Unstructured {
	f.t.Helper()
	ho := &unstructured.Unstructured{}
	ho.SetGroupVersionKind(helmOpGVK)
	err := f.r.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(f.w.Name, component)}, ho)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		f.t.Fatalf("get HelmOp %s: %v", component, err)
	}
	return ho
}

func addBundleScheme(scheme *kruntime.Scheme) {
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	bl := bundleGVK
	bl.Kind += "List"
	scheme.AddKnownTypeWithName(bl, &unstructured.UnstructuredList{})
}

func (f *holdFixture) setBundle(component string, ready int64) {
	f.t.Helper()
	ho := f.helmOp(component)
	if ho == nil {
		f.t.Fatalf("no HelmOp for %s", component)
	}
	putBundle(f.t, f.r, ho, ready)
}

func (f *holdFixture) gitFile(component string) string {
	return "workloads/" + blueprintBundleName(f.w.Name, component) + ".yaml"
}

func (f *holdFixture) gitDigest(component string) string {
	f.t.Helper()
	docs := gitOpsDocs(f.t, f.remote, blueprintBundleName(f.w.Name, component))
	d, _, _ := unstructured.NestedString(docs[0], "spec", "labels", renderDigestLabel)
	return d
}

// certifyRenderedBundles stands in for Fleet: every fleet-local HelmOp gets a
// Bundle fully rolled out at its current render digest.
func certifyRenderedBundles(t *testing.T, r *AIWorkloadReconciler) {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(helmOpGVK.GroupVersion().WithKind("HelmOpList"))
	if err := r.List(context.Background(), list, client.InNamespace("fleet-local")); err != nil {
		t.Fatalf("list HelmOps: %v", err)
	}
	for i := range list.Items {
		putBundle(t, r, &list.Items[i], 1)
	}
}

// putBundle creates or replaces the HelmOp's Bundle, labelled with the
// HelmOp's render digest and rolled out to ready of one cluster.
func putBundle(t *testing.T, r *AIWorkloadReconciler, ho *unstructured.Unstructured, ready int64) {
	t.Helper()
	putBundleStatus(t, r, types.NamespacedName{Namespace: ho.GetNamespace(), Name: ho.GetName()}, helmOpDigest(ho), ready, 0)
}

// putBundleStatus creates or replaces a Bundle desired on one cluster with the
// given digest label and ready/modified counts.
func putBundleStatus(t *testing.T, r *AIWorkloadReconciler, key types.NamespacedName, digest string, ready, modified int64) {
	t.Helper()
	ctx := context.Background()
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(bundleGVK)
	if err := r.Get(ctx, key, existing); err == nil {
		if err := r.Delete(ctx, existing); err != nil {
			t.Fatalf("delete bundle: %v", err)
		}
	}
	b := &unstructured.Unstructured{Object: map[string]any{}}
	b.SetGroupVersionKind(bundleGVK)
	b.SetNamespace(key.Namespace)
	b.SetName(key.Name)
	b.SetLabels(map[string]string{renderDigestLabel: digest})
	_ = unstructured.SetNestedField(b.Object, int64(0), "status", "observedGeneration")
	_ = unstructured.SetNestedField(b.Object, int64(0), "status", "unavailable")
	_ = unstructured.SetNestedField(b.Object, ready, "status", "summary", "ready")
	_ = unstructured.SetNestedField(b.Object, modified, "status", "summary", "modified")
	_ = unstructured.SetNestedField(b.Object, int64(1), "status", "summary", "desiredReady")
	if err := r.Create(ctx, b); err != nil {
		t.Fatalf("create bundle: %v", err)
	}
}

func helmOpDigest(ho *unstructured.Unstructured) string {
	d, _, _ := unstructured.NestedString(ho.Object, "spec", "labels", renderDigestLabel)
	return d
}

func (f *holdFixture) ready() *metav1.Condition {
	return meta.FindStatusCondition(f.w.Status.Conditions, conditionTypeReady)
}

func TestDependencyHold_HeldUntilCertified(t *testing.T) {
	f := newHoldFixture(t, []string{"control-plane"}, nil)
	f.reconcile()
	if f.helmOp("control-plane") == nil {
		t.Fatal("control-plane must render")
	}
	f.setBundle("control-plane", 0)
	f.reconcile()

	if f.helmOp("cluster") != nil {
		t.Fatal("cluster must not render while control-plane is rolling out")
	}
	cond := f.ready()
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "WaitingForDependencies" {
		t.Fatalf("expected Ready=False/WaitingForDependencies, got %+v", cond)
	}
	if !strings.Contains(cond.Message, "waiting for control-plane to roll out") {
		t.Fatalf("message %q does not name the pending dependency", cond.Message)
	}
	if f.w.Status.DeployedSource != nil {
		t.Fatalf("workload must not be certified, got %+v", f.w.Status.DeployedSource)
	}
}

func TestDependencyHold_ReleasedWhenCertified(t *testing.T) {
	f := newHoldFixture(t, []string{"control-plane"}, nil)
	f.reconcile()
	f.setBundle("control-plane", 1)
	f.reconcile()

	if f.helmOp("cluster") == nil {
		t.Fatal("cluster must render once control-plane is certified")
	}
	if cond := f.ready(); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %+v", cond)
	}
}

func TestDependencyHold_UpgradeKeepsOldRender(t *testing.T) {
	f := newHoldFixture(t, []string{"control-plane"}, nil)
	f.reconcile()
	f.setBundle("control-plane", 1)
	f.reconcile()
	f.setBundle("cluster", 1)
	f.reconcile()
	before, beforeCP := f.helmOp("cluster"), f.helmOp("control-plane")
	if before == nil {
		t.Fatal("cluster must render before the upgrade")
	}

	f.w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "control-plane", Values: &apixv1.JSON{Raw: []byte(`{"v":2}`)}},
		{ComponentName: "cluster", Values: &apixv1.JSON{Raw: []byte(`{"v":2}`)}},
	}
	f.reconcile()

	if helmOpDigest(f.helmOp("control-plane")) == helmOpDigest(beforeCP) {
		t.Fatal("test setup: control-plane digest should change")
	}
	after := f.helmOp("cluster")
	if !reflect.DeepEqual(before.Object["spec"], after.Object["spec"]) {
		t.Fatalf("cluster HelmOp changed while control-plane was mid-rollout:\nbefore %v\nafter  %v", before.Object["spec"], after.Object["spec"])
	}
	if cond := f.ready(); cond == nil || cond.Reason != "WaitingForDependencies" {
		t.Fatalf("expected WaitingForDependencies, got %+v", cond)
	}
}

func TestDependencyHold_ExcludedDepNotWaited(t *testing.T) {
	f := newHoldFixture(t, []string{"control-plane"}, []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "control-plane", Enabled: ptrBool(false)},
	})
	f.reconcile()

	if f.helmOp("cluster") == nil {
		t.Fatal("cluster must render when its dependency is disabled")
	}
	if cond := f.ready(); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %+v", cond)
	}
}

func TestDependencyHold_NoDependsOnUnaffected(t *testing.T) {
	f := newHoldFixture(t, nil, nil)
	f.reconcile()

	for _, c := range []string{"cluster", "control-plane"} {
		if f.helmOp(c) == nil {
			t.Fatalf("%s must render in the first pass", c)
		}
	}
	if cond := f.ready(); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %+v", cond)
	}
}

func TestDependencyHold_ModifiedDepNotHeld(t *testing.T) {
	f := newHoldFixture(t, []string{"control-plane"}, nil)
	f.reconcile()
	cp := f.helmOp("control-plane")
	putBundleStatus(t, f.r, types.NamespacedName{Namespace: cp.GetNamespace(), Name: cp.GetName()}, helmOpDigest(cp), 0, 1)
	f.reconcile()

	if f.helmOp("cluster") == nil {
		t.Fatal("cluster must render when control-plane is rolled out but reported Modified")
	}
}

func TestDependencyHold_GitOpsHeld(t *testing.T) {
	f := newHoldFixtureFor(t, aiplatformv1alpha1.AIWorkloadDeployGitOps, []string{"control-plane"}, nil)
	f.reconcile()

	if !gitOpsFileExists(t, f.remote, f.gitFile("control-plane")) {
		t.Fatal("control-plane git file must be written")
	}
	if gitOpsFileExists(t, f.remote, f.gitFile("cluster")) {
		t.Fatal("cluster git file must not be written while control-plane is uncertified")
	}
	if cond := f.ready(); cond == nil || cond.Reason != "WaitingForDependencies" {
		t.Fatalf("expected WaitingForDependencies, got %+v", cond)
	}

	key := types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(f.w.Name, "control-plane")}
	putBundleStatus(t, f.r, key, f.gitDigest("control-plane"), 0, 0)
	f.reconcile()
	if gitOpsFileExists(t, f.remote, f.gitFile("cluster")) {
		t.Fatal("cluster git file must not be written while control-plane is rolling out")
	}

	putBundleStatus(t, f.r, key, f.gitDigest("control-plane"), 1, 0)
	f.reconcile()
	if !gitOpsFileExists(t, f.remote, f.gitFile("cluster")) {
		t.Fatal("cluster git file must be written once control-plane is settled")
	}
}
