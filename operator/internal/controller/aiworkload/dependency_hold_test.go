package aiworkload

import (
	"context"
	"fmt"
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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
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
	for _, gvk := range []schema.GroupVersionKind{bundleGVK, bundleDeploymentGVK} {
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		l := gvk
		l.Kind += "List"
		scheme.AddKnownTypeWithName(l, &unstructured.UnstructuredList{})
	}
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

// digestOf is component's current render digest on either strategy: the
// HelmOp's label, or for GitOps the label on its published Bundle.
func (f *holdFixture) digestOf(component string) string {
	f.t.Helper()
	if f.remote != "" {
		return f.gitDigest(component)
	}
	ho := f.helmOp(component)
	if ho == nil {
		f.t.Fatalf("no HelmOp for %s", component)
	}
	return helmOpDigest(ho)
}

// settle stands in for Fleet: component's Bundle and BundleDeployment are
// rolled out at its current render digest.
func (f *holdFixture) settle(component string) {
	f.t.Helper()
	key := types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(f.w.Name, component)}
	putBundleStatus(f.t, f.r, key, f.digestOf(component), 1, 0)
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
// given digest label and ready/modified counts, and its BundleDeployment
// applied at that digest in the matching state.
func putBundleStatus(t *testing.T, r *AIWorkloadReconciler, key types.NamespacedName, digest string, ready, modified int64) {
	t.Helper()
	state := "NotReady"
	switch {
	case ready > 0:
		state = "Ready"
	case modified > 0:
		state = "Modified"
	}
	putBundleDeployment(t, r, settledBD(key.Namespace, key.Name, "local", digest, "s-"+digest, "s-"+digest, state))
	putBundleOnly(t, r, key, digest, ready, modified)
}

// putBundleOnly is putBundleStatus without touching the BundleDeployment.
func putBundleOnly(t *testing.T, r *AIWorkloadReconciler, key types.NamespacedName, digest string, ready, modified int64) {
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

// putBundleDeployment creates or replaces bd.
func putBundleDeployment(t *testing.T, r *AIWorkloadReconciler, bd *unstructured.Unstructured) {
	t.Helper()
	ctx := context.Background()
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(bundleDeploymentGVK)
	if err := r.Get(ctx, client.ObjectKeyFromObject(bd), existing); err == nil {
		if err := r.Delete(ctx, existing); err != nil {
			t.Fatalf("delete bundle deployment: %v", err)
		}
	}
	if err := r.Create(ctx, bd); err != nil {
		t.Fatalf("create bundle deployment: %v", err)
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

// Fleet's first Bundle status write after a spec change carries the new
// observedGeneration and digest with a summary of the old BundleDeployments.
func TestDependencyHold_StaleBundleSummaryHeld(t *testing.T) {
	f := newHoldFixture(t, []string{"control-plane"}, nil)
	f.reconcile()
	f.setBundle("control-plane", 1)
	f.reconcile()
	f.setBundle("cluster", 1)
	f.reconcile()
	before := f.helmOp("cluster")

	f.w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "control-plane", Values: &apixv1.JSON{Raw: []byte(`{"v":2}`)}},
		{ComponentName: "cluster", Values: &apixv1.JSON{Raw: []byte(`{"v":2}`)}},
	}
	f.reconcile()
	cp := f.helmOp("control-plane")
	key := types.NamespacedName{Namespace: cp.GetNamespace(), Name: cp.GetName()}
	putBundleOnly(t, f.r, key, helmOpDigest(cp), 1, 0)
	putBundleDeployment(t, f.r, settledBD(key.Namespace, key.Name, "local", helmOpDigest(cp), "s-new", "s-old", "Ready"))
	f.reconcile()
	if helmOpDigest(f.helmOp("cluster")) != helmOpDigest(before) {
		t.Fatal("cluster released while control-plane's BundleDeployment still applied the previous deployment")
	}
	if cond := f.ready(); cond == nil || cond.Reason != "WaitingForDependencies" {
		t.Fatalf("expected WaitingForDependencies, got %+v", cond)
	}

	putBundleDeployment(t, f.r, settledBD(key.Namespace, key.Name, "local", helmOpDigest(cp), "s-new", "s-new", "Ready"))
	f.reconcile()
	if helmOpDigest(f.helmOp("cluster")) == helmOpDigest(before) {
		t.Fatal("cluster must be released once control-plane's BundleDeployment applied the new deployment")
	}
}

// A BundleDeployment Fleet has not yet updated is Ready at its own, previous
// deployment: its IDs agree with each other, so only its render digest label
// tells it from this render's.
func TestDependencyHold_UntouchedPreviousBundleDeploymentHeld(t *testing.T) {
	f := newHoldFixture(t, []string{"control-plane"}, nil)
	f.reconcile()
	f.setBundle("control-plane", 1)
	f.reconcile()
	f.setBundle("cluster", 1)
	f.reconcile()
	before := f.helmOp("cluster")

	f.w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "control-plane", Values: &apixv1.JSON{Raw: []byte(`{"v":2}`)}},
		{ComponentName: "cluster", Values: &apixv1.JSON{Raw: []byte(`{"v":2}`)}},
	}
	f.reconcile()
	cp := f.helmOp("control-plane")
	key := types.NamespacedName{Namespace: cp.GetNamespace(), Name: cp.GetName()}
	// The Bundle moves to the new render; its BundleDeployment, left from the last one, does not.
	putBundleOnly(t, f.r, key, helmOpDigest(cp), 1, 0)
	f.reconcile()
	if helmOpDigest(f.helmOp("cluster")) != helmOpDigest(before) {
		t.Fatal("cluster released while control-plane's BundleDeployment still belongs to the previous render")
	}
	if cond := f.ready(); cond == nil || cond.Reason != "WaitingForDependencies" {
		t.Fatalf("expected WaitingForDependencies, got %+v", cond)
	}

	putBundleStatus(t, f.r, key, helmOpDigest(cp), 1, 0)
	f.reconcile()
	if helmOpDigest(f.helmOp("cluster")) == helmOpDigest(before) {
		t.Fatal("cluster must be released once control-plane's BundleDeployment belongs to the new render")
	}
}

// A new render that changes only the prerequisite's release name must hold the
// dependent until Fleet applies it, like any other change to the HelmOp spec:
// until then the previous rollout's Fleet objects still look settled.
func TestDependencyHold_ReleaseNameChangeHeld(t *testing.T) {
	for _, strategy := range []aiplatformv1alpha1.AIWorkloadDeployStrategy{aiplatformv1alpha1.AIWorkloadDeployFleetBundle, aiplatformv1alpha1.AIWorkloadDeployGitOps} {
		t.Run(string(strategy), func(t *testing.T) {
			f := newHoldFixtureFor(t, strategy, []string{"control-plane"}, nil)
			f.reconcile()
			f.settle("control-plane")
			f.reconcile()
			f.settle("cluster")
			f.reconcile()
			if cond := f.ready(); cond == nil || cond.Reason != reasonReconciled {
				t.Fatalf("expected the first rollout settled, got %+v", cond)
			}

			ctx := context.Background()
			bp := &aiplatformv1alpha1.Blueprint{}
			if err := f.r.Get(ctx, types.NamespacedName{Name: naming.BlueprintCRName("hold", "1.0.0")}, bp); err != nil {
				t.Fatal(err)
			}
			for i := range bp.Spec.Components {
				if bp.Spec.Components[i].ChartName == "control-plane" {
					bp.Spec.Components[i].ReleaseName = "runai-backend"
				}
			}
			if err := f.r.Update(ctx, bp); err != nil {
				t.Fatal(err)
			}
			before := f.digestOf("control-plane")
			f.reconcile()
			if f.digestOf("control-plane") == before {
				t.Fatal("the release name change did not change control-plane's render digest")
			}
			if cond := f.ready(); cond == nil || cond.Reason != "WaitingForDependencies" {
				t.Fatalf("cluster released before Fleet applied control-plane's new release name, Ready = %+v", cond)
			}

			f.settle("control-plane")
			f.reconcile()
			if cond := f.ready(); cond == nil || cond.Reason == "WaitingForDependencies" {
				t.Fatalf("cluster must be released once Fleet applied the new render, Ready = %+v", cond)
			}
		})
	}
}

func TestDependencyHold_MissingBundleDeploymentHeld(t *testing.T) {
	f := newHoldFixture(t, []string{"control-plane"}, nil)
	f.reconcile()
	cp := f.helmOp("control-plane")
	putBundleOnly(t, f.r, types.NamespacedName{Namespace: cp.GetNamespace(), Name: cp.GetName()}, helmOpDigest(cp), 1, 0)
	f.reconcile()
	if f.helmOp("cluster") != nil {
		t.Fatal("cluster must not render before control-plane has a BundleDeployment")
	}
}

// chainFixture is a -> b -> comp on an HTTP repo, targeting the local cluster.
func chainFixture(t *testing.T, compOverlay bool, puller chartArchivePuller) *holdFixture {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)
	addBundleScheme(scheme)
	w := newBlueprintWorkload(time.Now().Add(-time.Hour))
	w.UID = "uid-chain"
	w.Spec.Source.Blueprint = &aiplatformv1alpha1.BlueprintSource{Name: "chain", Version: "1.0.0"}
	if compOverlay {
		w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "comp", Kustomize: inlineOverlay("inst", emptyPatches)}}
	}
	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: naming.BlueprintCRName("chain", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: "application-collection", ChartName: "a", ChartVersion: "1.0.0", Vendor: "suse", DependsOn: []string{"b"}},
		{ChartRepo: "application-collection", ChartName: "b", ChartVersion: "1.0.0", Vendor: "suse", DependsOn: []string{"comp"}},
		{ChartRepo: "application-collection", ChartName: "comp", ChartVersion: "1.0.0", Vendor: "suse"},
	}
	repo := repoObj("application-collection", map[string]any{"url": "https://charts.example.com"})
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo).Build()
	return &holdFixture{t: t, r: &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator", ChartPuller: puller}, w: w}
}

func (f *holdFixture) cell(component string) *aiplatformv1alpha1.AIWorkloadComponentStatus {
	for i := range f.w.Status.ComponentStatuses {
		if f.w.Status.ComponentStatuses[i].ComponentName == component {
			return &f.w.Status.ComponentStatuses[i]
		}
	}
	return nil
}

func (f *holdFixture) expectHeld(want string) {
	f.t.Helper()
	cond := f.ready()
	if cond == nil || cond.Reason != "WaitingForDependencies" || cond.Message != want {
		f.t.Fatalf("Ready = %+v\nwant WaitingForDependencies %q", cond, want)
	}
}

func TestDependencyHold_AcceptedFalseDependencyNamed(t *testing.T) {
	f := chainFixture(t, false, nil)
	f.reconcile()
	ho := f.helmOp("comp")
	_ = unstructured.SetNestedSlice(ho.Object, []any{map[string]any{
		"type": "Accepted", "status": "False", "reason": "Error",
		"message": `chart "comp" version "9.9.9" not found`, "lastUpdateTime": "2026-01-01T00:00:00Z",
	}}, "status", "conditions")
	if err := f.r.Update(context.Background(), ho); err != nil {
		t.Fatal(err)
	}
	f.reconcile()
	if c := f.cell("comp"); c == nil || c.Phase != aiplatformv1alpha1.AIWorkloadClusterPhaseFailed || c.Message != `Error: chart "comp" version "9.9.9" not found` {
		t.Fatalf("comp cell = %+v, want Failed with the Accepted message", c)
	}
	f.expectHeld(`b: waiting for comp, which failed on local: Error: chart "comp" version "9.9.9" not found; a: waiting for b to roll out`)
}

func TestDependencyHold_ErrAppliedDependencyNamed(t *testing.T) {
	f := chainFixture(t, false, nil)
	f.reconcile()
	ho := f.helmOp("comp")
	d := helmOpDigest(ho)
	bd := settledBD("fleet-local", ho.GetName(), "local", d, "s-"+d, "s-"+d, "ErrApplied")
	l := bd.GetLabels()
	l["fleet.cattle.io/cluster"] = "local"
	bd.SetLabels(l)
	_ = unstructured.SetNestedField(bd.Object, "helm install failed: image pull secret missing", "status", "display", "message")
	putBundleDeployment(t, f.r, bd)
	putBundleOnly(t, f.r, types.NamespacedName{Namespace: "fleet-local", Name: ho.GetName()}, d, 0, 0)
	f.reconcile()
	f.expectHeld("b: waiting for comp, which failed on local: helm install failed: image pull secret missing; a: waiting for b to roll out")
}

// Fleet 0.14.1 reports a failed upgrade as ErrApplied while the BundleDeployment
// still runs the previous deployment. The prerequisite stays Pending (a stale
// failure must not fail a new render) and the dependent held, but both say why.
func TestDependencyHold_FailedRolloutExplainsPendingDependency(t *testing.T) {
	f := chainFixture(t, false, nil)
	f.reconcile()
	ho := f.helmOp("comp")
	d := helmOpDigest(ho)
	bd := settledBD("fleet-local", ho.GetName(), "local", d, "s-new", "s-old", "ErrApplied")
	l := bd.GetLabels()
	l["fleet.cattle.io/cluster"] = "local"
	bd.SetLabels(l)
	_ = unstructured.SetNestedSlice(bd.Object, []any{map[string]any{
		"type": "Deployed", "status": "False", "message": "helm upgrade failed: image pull secret missing",
	}}, "status", "conditions")
	putBundleDeployment(t, f.r, bd)
	putBundleOnly(t, f.r, types.NamespacedName{Namespace: "fleet-local", Name: ho.GetName()}, d, 0, 0)
	f.reconcile()
	f.expectHeld("b: waiting for comp (on local: helm upgrade failed: image pull secret missing) to roll out; a: waiting for b to roll out")
	for _, c := range f.w.Status.ComponentStatuses {
		if c.ComponentName == "comp" && (c.Phase != aiplatformv1alpha1.AIWorkloadClusterPhasePending || c.Message != "helm upgrade failed: image pull secret missing") {
			t.Fatalf("comp cell = %s %q, want Pending with Fleet's Deployed message", c.Phase, c.Message)
		}
	}
}

func TestDependencyHold_PendingDependencyKeepsWording(t *testing.T) {
	f := chainFixture(t, false, nil)
	f.reconcile()
	f.setBundle("comp", 0)
	f.reconcile()
	f.expectHeld("b: waiting for comp to roll out; a: waiting for b to roll out")
}

func TestDependencyHold_FetchErrorUnchanged(t *testing.T) {
	f := chainFixture(t, true, func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) {
		return nil, fmt.Errorf("registry unreachable")
	})
	_, _, _ = f.r.reconcileBlueprintStatus(context.Background(), f.w)
	cond := f.ready()
	if cond == nil || cond.Reason != "ComponentReconcileFailed" || !strings.Contains(cond.Message, `Component "comp" from repo "application-collection" could not be reconciled`) || !strings.Contains(cond.Message, "registry unreachable") {
		t.Fatalf("Ready = %+v, want the unchanged ComponentReconcileFailed", cond)
	}
}

func TestHeldConditionMessage_MixedAndTruncated(t *testing.T) {
	cells := []aiplatformv1alpha1.AIWorkloadComponentStatus{
		{ComponentName: "x", ClusterID: "c-1", Phase: aiplatformv1alpha1.AIWorkloadClusterPhaseFailed},
		{ComponentName: "y", ClusterID: "local", Phase: aiplatformv1alpha1.AIWorkloadClusterPhasePending},
	}
	got := heldConditionMessage([]heldComponent{{name: "b", pending: []string{"x", "y"}}}, cells)
	if want := "b: waiting for x, which failed on c-1; also waiting for y to roll out"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	more := []aiplatformv1alpha1.AIWorkloadComponentStatus{cells[0], cells[1],
		{ComponentName: "x", ClusterID: "c-2", Phase: aiplatformv1alpha1.AIWorkloadClusterPhaseFailed},
		{ComponentName: "x", ClusterID: "c-3", Phase: aiplatformv1alpha1.AIWorkloadClusterPhaseFailed}}
	if got, want := heldConditionMessage([]heldComponent{{name: "b", pending: []string{"x"}}}, more), "b: waiting for x, which failed on c-1 (and 2 more)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	cells[0].Message = strings.Repeat("e", 2000)
	if n := len(truncateForCondition(heldConditionMessage([]heldComponent{{name: "b", pending: []string{"x"}}}, cells))); n > 1024+len("… (truncated)") {
		t.Fatalf("condition message length %d exceeds the cap", n)
	}
}
