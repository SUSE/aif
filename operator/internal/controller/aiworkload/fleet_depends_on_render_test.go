package aiworkload

import (
	"context"
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
)

func acceptedDependsOn(bundle string) []any {
	return []any{map[string]any{"name": bundle, "acceptedStates": []any{"Ready", "Modified"}}}
}

// assertDependsOn checks spec.dependsOn of a rendered object: the
// acceptedStates entries when Fleet supports them, otherwise no field at all.
func assertDependsOn(t *testing.T, kind string, spec map[string]any, acceptedStates bool, bundle string) {
	t.Helper()
	got, found, _ := unstructured.NestedSlice(spec, "dependsOn")
	if !acceptedStates {
		if found {
			t.Fatalf("%s spec.dependsOn = %v, want absent on Fleet without acceptedStates", kind, got)
		}
		return
	}
	if want := acceptedDependsOn(bundle); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s spec.dependsOn = %v, want %v", kind, got, want)
	}
}

type helmOpDepsEnv struct {
	r *AIWorkloadReconciler
	c client.Client
	w *aiplatformv1alpha1.AIWorkload
}

func newHelmOpDepsEnv(t *testing.T, acceptedStates bool) *helmOpDepsEnv {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)
	addBundleScheme(scheme)
	w := newBlueprintWorkload(time.Now())
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, repo).Build()
	tgz := compChartTgz(t)
	r := &AIWorkloadReconciler{
		Client: c, Scheme: scheme, OperatorNamespace: "aif-operator",
		ChartPuller:       func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { return tgz, nil },
		FleetCapabilities: staticFleetCapabilities{acceptedStates: acceptedStates},
	}
	return &helmOpDepsEnv{r: r, c: c, w: w}
}

func depComponent() aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{ChartRepo: "application-collection", ChartName: "comp", ChartVersion: "1.0.0", Vendor: "suse", DependsOn: []string{"dep"}}
}

func (e *helmOpDepsEnv) render(t *testing.T, c aiplatformv1alpha1.BlueprintComponent) string {
	t.Helper()
	digest, err := e.r.ensureBlueprintHelmOp(context.Background(), e.w, c, blueprintBundleName(e.w.Name, c.ChartName))
	if err != nil {
		t.Fatalf("ensureBlueprintHelmOp: %v", err)
	}
	return digest
}

func TestRender_HelmOpDependsOnFollowsFleetCapabilities(t *testing.T) {
	for _, acceptedStates := range []bool{true, false} {
		e := newHelmOpDepsEnv(t, acceptedStates)
		e.render(t, depComponent())
		ho, ok := fleetObject(t, e.c, "HelmOp", blueprintBundleName(e.w.Name, "comp"))
		if !ok {
			t.Fatal("HelmOp not rendered")
		}
		assertDependsOn(t, "HelmOp", ho.Object["spec"].(map[string]any), acceptedStates, blueprintBundleName(e.w.Name, "dep"))
	}
}

func TestRender_EmbeddedBundleDependsOnFollowsFleetCapabilities(t *testing.T) {
	for _, acceptedStates := range []bool{true, false} {
		e := newHelmOpDepsEnv(t, acceptedStates)
		c := depComponent()
		c.Kustomize = inlineOverlay("ov", emptyPatches)
		e.render(t, c)
		b, ok := fleetObject(t, e.c, "Bundle", blueprintBundleName(e.w.Name, "comp"))
		if !ok {
			t.Fatal("embedded Bundle not rendered")
		}
		assertDependsOn(t, "Bundle", b.Object["spec"].(map[string]any), acceptedStates, blueprintBundleName(e.w.Name, "dep"))
	}
}

func TestRender_GitOpsDependsOnFollowsFleetCapabilities(t *testing.T) {
	for _, overlay := range []bool{false, true} {
		for _, acceptedStates := range []bool{true, false} {
			e := newGitOpsSwitchEnv(t)
			e.r.FleetCapabilities = staticFleetCapabilities{acceptedStates: acceptedStates}
			e.comp.DependsOn = []string{"dep"}
			if overlay {
				e.addOverlay()
			}
			if err := e.render(t, gitOpsSwitchBundle); err != nil {
				t.Fatalf("ensureBlueprintGitFile: %v", err)
			}
			docs := gitOpsDocs(t, e.remote, gitOpsSwitchBundle)
			if len(docs) != 1 {
				t.Fatalf("published %d documents, want 1", len(docs))
			}
			kind, _ := docs[0]["kind"].(string)
			if want := map[bool]string{false: "HelmOp", true: "Bundle"}[overlay]; kind != want {
				t.Fatalf("published kind = %q, want %q", kind, want)
			}
			assertDependsOn(t, "GitOps "+kind, docs[0]["spec"].(map[string]any), acceptedStates, blueprintBundleName(e.w.Name, "dep"))
		}
	}
}

// Live Fleet v0.14.1 deadlock: server-side apply must strip a legacy dependsOn.
func TestRender_StaleHelmOpDependsOnRemovedOnOldFleet(t *testing.T) {
	e := newHelmOpDepsEnv(t, false)
	name := blueprintBundleName(e.w.Name, "comp")
	legacy := &unstructured.Unstructured{}
	legacy.SetGroupVersionKind(helmOpGVK)
	legacy.SetName(name)
	legacy.SetNamespace("fleet-local")
	_ = unstructured.SetNestedSlice(legacy.Object, []any{map[string]any{"name": blueprintBundleName(e.w.Name, "dep")}}, "spec", "dependsOn")
	_ = unstructured.SetNestedStringMap(legacy.Object, map[string]string{renderDigestLabel: "legacy"}, "spec", "labels")
	if err := e.c.Patch(context.Background(), legacy, client.Apply, client.ForceOwnership, client.FieldOwner("aif-operator")); err != nil {
		t.Fatalf("seed legacy HelmOp: %v", err)
	}

	digest := e.render(t, depComponent())

	ho, _ := fleetObject(t, e.c, "HelmOp", name)
	if dep, found, _ := unstructured.NestedSlice(ho.Object, "spec", "dependsOn"); found {
		t.Fatalf("spec.dependsOn = %v, want removed", dep)
	}
	if got := helmOpDigest(ho); got != renderDigestLabelValue(digest) {
		t.Fatalf("render digest label = %q, want %q", got, renderDigestLabelValue(digest))
	}
}

func TestRender_DependsOnModeFlipMovesDigestOnlyForDependents(t *testing.T) {
	digests := map[bool][2]string{}
	for _, acceptedStates := range []bool{true, false} {
		e := newHelmOpDepsEnv(t, acceptedStates)
		noDeps := depComponent()
		noDeps.ChartName, noDeps.DependsOn = "dep", nil
		digests[acceptedStates] = [2]string{e.render(t, depComponent()), e.render(t, noDeps)}
	}
	if digests[true][0] == digests[false][0] {
		t.Fatal("a dependent's render digest must change when the Fleet dependsOn mode flips")
	}
	if digests[true][1] != digests[false][1] {
		t.Fatal("a component without dependsOn must keep its render digest across modes")
	}
}

func TestEmbeddedFingerprint_DependsOnModeFlip(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ChartRepo: "r", ChartName: "x", ChartVersion: "1.0.0"}
	w := wl("wl")
	fp := func(deps []any) string {
		return embeddedChartFingerprint(c, "ns", "commit", nil, deps, nil, "", 0)
	}
	legacy := fp([]any{blueprintBundleName("wl", "dep")})
	none := fp(resolveDependsOn(w, []string{"dep"}, fleetDependsOnNone))
	accepted := fp(resolveDependsOn(w, []string{"dep"}, fleetDependsOnReadyOrModified))
	if legacy == none || legacy == accepted || none == accepted {
		t.Fatalf("fingerprints must differ across dependsOn renders: legacy=%s none=%s accepted=%s", legacy, none, accepted)
	}
	if none != fp(nil) {
		t.Fatal("with no Fleet dependsOn rendered, the fingerprint must equal a component without dependencies")
	}
}
