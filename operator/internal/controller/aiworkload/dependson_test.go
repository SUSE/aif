package aiworkload

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func depComp(chart string, deps ...string) aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{ChartRepo: "suse-ai", ChartName: chart, ChartVersion: "1.0.0", DependsOn: deps}
}

func TestValidateComponentDependencies(t *testing.T) {
	cases := []struct {
		name    string
		comps   []aiplatformv1alpha1.BlueprintComponent
		wantErr string
	}{
		{name: "no dependencies", comps: []aiplatformv1alpha1.BlueprintComponent{depComp("a"), depComp("b")}},
		{name: "chain", comps: []aiplatformv1alpha1.BlueprintComponent{depComp("a"), depComp("b", "a"), depComp("c", "b", "a")}},
		{name: "self", comps: []aiplatformv1alpha1.BlueprintComponent{depComp("a", "a")}, wantErr: `component "a" depends on itself`},
		{name: "unknown", comps: []aiplatformv1alpha1.BlueprintComponent{depComp("a", "missing")}, wantErr: `"missing", which is not a component`},
		{name: "two-cycle", comps: []aiplatformv1alpha1.BlueprintComponent{depComp("a", "b"), depComp("b", "a")}, wantErr: "cycle: a -> b -> a"},
		{name: "three-cycle", comps: []aiplatformv1alpha1.BlueprintComponent{depComp("a", "c"), depComp("b", "a"), depComp("c", "b")}, wantErr: "cycle: a -> c -> b -> a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateComponentDependencies(tc.comps)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestPruneDisabledDependencies(t *testing.T) {
	in := []aiplatformv1alpha1.BlueprintComponent{depComp("b", "a", "disabled"), depComp("c", "disabled")}
	out := pruneDisabledDependencies(append(in, depComp("a")))
	if got := out[0].DependsOn; !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("b dependsOn = %v, want [a]", got)
	}
	if got := out[1].DependsOn; got != nil {
		t.Errorf("c dependsOn = %v, want nil", got)
	}
	if got := in[0].DependsOn; !reflect.DeepEqual(got, []string{"a", "disabled"}) {
		t.Errorf("input mutated: %v", got)
	}
}

func TestDependsOnRefs(t *testing.T) {
	if refs := dependsOnRefs("wl", nil); refs != nil {
		t.Errorf("no deps: got %v, want nil", refs)
	}
	got := dependsOnRefs("wl", []string{"nfd", "cert-manager"})
	want := []any{
		map[string]any{"name": blueprintBundleName("wl", "cert-manager")},
		map[string]any{"name": blueprintBundleName("wl", "nfd")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

func TestEnsureBlueprintHelmOp_DependsOn(t *testing.T) {
	w := &aiplatformv1alpha1.AIWorkload{ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "aif-operator"}}
	w.Spec.TargetNamespace = "dep-ns"
	w.Spec.TargetClusters = []string{"local"}

	get := func(t *testing.T, r *AIWorkloadReconciler, name string) *unstructured.Unstructured {
		t.Helper()
		ho := &unstructured.Unstructured{}
		ho.SetGroupVersionKind(helmOpGVK)
		if err := r.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: name}, ho); err != nil {
			t.Fatalf("get HelmOp %s: %v", name, err)
		}
		return ho
	}

	t.Run("dependency rendered as Fleet bundle ref", func(t *testing.T) {
		r := newRepoFakeClient(t)
		if _, err := r.ensureBlueprintHelmOp(context.Background(), w, depComp("gpu-operator", "nfd"), "wl-gpu-operator"); err != nil {
			t.Fatalf("ensureBlueprintHelmOp: %v", err)
		}
		deps, found, err := unstructured.NestedSlice(get(t, r, "wl-gpu-operator").Object, "spec", "dependsOn")
		if err != nil || !found {
			t.Fatalf("spec.dependsOn: found=%v err=%v", found, err)
		}
		want := []any{map[string]any{"name": blueprintBundleName("wl", "nfd")}}
		if !reflect.DeepEqual(deps, want) {
			t.Errorf("spec.dependsOn = %v, want %v", deps, want)
		}
	})

	t.Run("no dependsOn field without dependencies", func(t *testing.T) {
		r := newRepoFakeClient(t)
		if _, err := r.ensureBlueprintHelmOp(context.Background(), w, depComp("nfd"), "wl-nfd"); err != nil {
			t.Fatalf("ensureBlueprintHelmOp: %v", err)
		}
		if _, found, _ := unstructured.NestedFieldNoCopy(get(t, r, "wl-nfd").Object, "spec", "dependsOn"); found {
			t.Error("spec.dependsOn set for a component without dependencies")
		}
	})
}

func TestEnsureBlueprintGitFile_DependsOn(t *testing.T) {
	ctx := context.Background()
	remoteURL := newBlueprintGitOpsRemote(t)
	scheme := gitRepoTestScheme()
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: "aif-operator"},
		Spec:       aiplatformv1alpha1.SettingsSpec{Fleet: aiplatformv1alpha1.FleetSettings{GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: remoteURL, Branch: "main"}}},
	}
	source := repoObj("suse-ai", map[string]any{"url": "https://charts.example.com"})
	workload := newGitOpsTestWorkload()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(settings, source, workload).Build()
	reconciler := &AIWorkloadReconciler{Client: fakeClient, Scheme: scheme, OperatorNamespace: "aif-operator"}
	bundleName := blueprintBundleName(workload.Name, "milvus")

	if _, err := reconciler.ensureBlueprintGitFile(ctx, workload, depComp("milvus", "etcd"), bundleName); err != nil {
		t.Fatalf("ensureBlueprintGitFile: %v", err)
	}
	content := readBlueprintGitOpsFile(t, remoteURL, "workloads/"+bundleName+".yaml")
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.Split(content, "\n---\n")[0]), &doc); err != nil {
		t.Fatalf("decode git file: %v", err)
	}
	deps, found, err := unstructured.NestedSlice(doc, "spec", "dependsOn")
	if err != nil || !found {
		t.Fatalf("spec.dependsOn: found=%v err=%v", found, err)
	}
	want := []any{map[string]any{"name": blueprintBundleName(workload.Name, "etcd")}}
	if !reflect.DeepEqual(deps, want) {
		t.Errorf("spec.dependsOn = %v, want %v", deps, want)
	}
}

// Components without dependencies must keep the render digest and git-chart
// fingerprint they had before the field existed, or every existing workload
// would re-render on upgrade.
func TestDependsOnKeepsDigestsStableWhenUnset(t *testing.T) {
	base := ComponentRenderInputs{ChartRepo: "r", ChartName: "c", ChartVersion: "1.0.0", Namespace: "ns"}
	b, _ := json.Marshal(base)
	if strings.Contains(string(b), "dependsOn") {
		t.Errorf("render inputs JSON includes dependsOn when unset: %s", b)
	}
	withDeps := base
	withDeps.DependsOn = []string{"x"}
	if perHelmOpRenderDigest(base) == perHelmOpRenderDigest(withDeps) {
		t.Error("render digest unchanged when dependsOn is set")
	}

	c := depComp("c")
	plain := gitChartFingerprint(c, "ns", "commit", nil)
	c.DependsOn = []string{"x"}
	if plain == gitChartFingerprint(c, "ns", "commit", nil) {
		t.Error("git-chart fingerprint unchanged when dependsOn is set")
	}
}

// An invalid dependency graph is surfaced as a condition before any HelmOp is
// rendered, so a broken blueprint never deploys half its components.
func TestReconcileBlueprintStatus_InvalidDependencies(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now().Add(-10 * time.Minute))
	bp := newBlueprintCR("suse-ai")
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{depComp("app", "db"), depComp("db", "app")}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	result, err := r.reconcileBlueprintStatus(context.Background(), w)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue for a terminal error, got %v", result.RequeueAfter)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Reason != reasonInvalidComponentDependencies {
		t.Fatalf("condition = %+v, want reason %q", cond, reasonInvalidComponentDependencies)
	}
	if !strings.Contains(cond.Message, "cycle") {
		t.Errorf("message should explain the cycle, got %q", cond.Message)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseFailed {
		t.Errorf("phase = %v, want Failed", w.Status.Phase)
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(helmOpGVK.GroupVersion().WithKind("HelmOpList"))
	if err := c.List(context.Background(), list); err != nil {
		t.Fatalf("list HelmOps: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected no HelmOps for an invalid blueprint, got %d", len(list.Items))
	}
}
