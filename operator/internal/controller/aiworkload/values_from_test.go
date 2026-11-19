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

func runaiValuesFrom() []aiplatformv1alpha1.ComponentValuesFrom {
	return []aiplatformv1alpha1.ComponentValuesFrom{{
		SecretKeyRef: &aiplatformv1alpha1.ValuesFromKeyRef{
			Name: "runai-cluster-values", Namespace: "runai-backend", Key: "values.yaml",
		},
	}}
}

func wantRunaiValuesFrom() []any {
	return []any{map[string]any{"secretKeyRef": map[string]any{
		"name": "runai-cluster-values", "namespace": "runai-backend", "key": "values.yaml",
	}}}
}

func TestValuesFromSpec_EmptyIsNil(t *testing.T) {
	if got := valuesFromSpec(aiplatformv1alpha1.BlueprintComponent{ChartName: "runai-cluster"}); got != nil {
		t.Fatalf("want nil, got %#v", got)
	}
}

func TestValuesFromSpec_SecretRef(t *testing.T) {
	got := valuesFromSpec(aiplatformv1alpha1.BlueprintComponent{ChartName: "runai-cluster", ValuesFrom: runaiValuesFrom()})
	if !reflect.DeepEqual(got, wantRunaiValuesFrom()) {
		t.Fatalf("got %#v\nwant %#v", got, wantRunaiValuesFrom())
	}
}

func TestValuesFromSpec_OmitsEmptyFields(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ValuesFrom: []aiplatformv1alpha1.ComponentValuesFrom{{
		ConfigMapKeyRef: &aiplatformv1alpha1.ValuesFromKeyRef{Name: "cfg"},
	}}}
	want := []any{map[string]any{"configMapKeyRef": map[string]any{"name": "cfg"}}}
	if got := valuesFromSpec(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestHelmOp_ValuesFromCarried(t *testing.T) {
	r := newRepoFakeClient(t)
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "aif-operator"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: "install-ns", TargetClusters: []string{"local"}},
	}
	c := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo: "suse-ai", ChartName: "runai-cluster", ChartVersion: "1.0.0",
		ValuesFrom: runaiValuesFrom(),
	}
	if _, err := r.ensureBlueprintHelmOp(context.Background(), w, c, "wl-runai"); err != nil {
		t.Fatalf("ensureBlueprintHelmOp: %v", err)
	}
	ho := &unstructured.Unstructured{}
	ho.SetGroupVersionKind(helmOpGVK)
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "wl-runai"}, ho); err != nil {
		t.Fatalf("get HelmOp: %v", err)
	}
	got, found, err := unstructured.NestedSlice(ho.Object, "spec", "helm", "valuesFrom")
	if err != nil || !found {
		t.Fatalf("spec.helm.valuesFrom: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(got, wantRunaiValuesFrom()) {
		t.Fatalf("got %#v\nwant %#v", got, wantRunaiValuesFrom())
	}
}

func TestGitOpsHelmOp_ValuesFromCarried(t *testing.T) {
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
	component := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo: "suse-ai", ChartName: "milvus", ChartVersion: "1.0.0",
		ValuesFrom: runaiValuesFrom(),
	}
	if _, err := reconciler.ensureBlueprintGitFile(ctx, workload, component, "private-source-workload-milvus"); err != nil {
		t.Fatalf("ensureBlueprintGitFile: %v", err)
	}
	content := readBlueprintGitOpsFile(t, remoteURL, "workloads/private-source-workload-milvus.yaml")
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.Split(content, "\n---\n")[0]), &doc); err != nil {
		t.Fatalf("decode git file: %v", err)
	}
	got, found, err := unstructured.NestedSlice(doc, "spec", "helm", "valuesFrom")
	if err != nil || !found {
		t.Fatalf("spec.helm.valuesFrom: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(got, wantRunaiValuesFrom()) {
		t.Fatalf("got %#v\nwant %#v", got, wantRunaiValuesFrom())
	}
}

func TestEmbeddedBundle_ValuesFromCarried(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "demo", ValuesFrom: runaiValuesFrom()}
	overlay := testOverlay(t, "overlay", map[string]string{"overlay/kustomization.yaml": emptyPatches})
	b, err := buildEmbeddedChartBundle("wl-demo", "ns", "fp", embeddedTestChart(t), c, nil, nil, nil, overlay, 0, true)
	if err != nil {
		t.Fatalf("buildEmbeddedChartBundle: %v", err)
	}
	got, found, err := unstructured.NestedSlice(b.Object, "spec", "helm", "valuesFrom")
	if err != nil || !found {
		t.Fatalf("spec.helm.valuesFrom: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(got, wantRunaiValuesFrom()) {
		t.Fatalf("got %#v\nwant %#v", got, wantRunaiValuesFrom())
	}

	plain, err := buildEmbeddedChartBundle("wl-demo", "ns", "fp", embeddedTestChart(t), aiplatformv1alpha1.BlueprintComponent{ChartName: "demo"}, nil, nil, nil, overlay, 0, true)
	if err != nil {
		t.Fatalf("buildEmbeddedChartBundle: %v", err)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(plain.Object, "spec", "helm", "valuesFrom"); found {
		t.Fatalf("valuesFrom must be omitted when the component has none")
	}
}

// goldenNoOverlayInputs mirrors the fixture of TestRenderDigest_NoOverlayIsUnchanged.
func goldenNoOverlayInputs() ComponentRenderInputs {
	return ComponentRenderInputs{
		ChartRepo: "application-collection", ChartName: "comp", ChartVersion: "1.0.0",
		Namespace: "install-ns", Vendor: "suse", RepoURL: "https://charts.example.com",
		Targets: []string{"local"}, Values: map[string]any{},
	}
}

const (
	goldenNoOverlayDigest = "fbf82471011f857db5f1324ceb48c966e149e3b4ef5a3ddbe622e09980b841d"
	goldenGitFingerprint  = "041d71a593b2fd69128e0beb736e1f780ad47210c4e002bd4f0b5d90931814f6"
)

// fingerprintGoldenGit mirrors TestEmbeddedChartFingerprint_GitNoOverlayUnchanged.
func fingerprintGoldenGit(c aiplatformv1alpha1.BlueprintComponent, valuesFrom []any) string {
	local := []any{map[string]any{"clusterName": "local"}}
	return embeddedChartFingerprint(c, "ns", "commit-abc", nil, nil, valuesFrom, "", 0, local, nil)
}

func goldenGitComponent() aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{ChartRepo: "git-repo", ChartName: "demo", ChartVersion: "1.2.3", ReleaseName: "rel"}
}

func TestDigest_UnchangedWithoutValuesFrom(t *testing.T) {
	if got := renderDigestLabelValue(perHelmOpRenderDigest(goldenNoOverlayInputs())); got != goldenNoOverlayDigest {
		t.Fatalf("perHelmOpRenderDigest changed: got %s want %s", got, goldenNoOverlayDigest)
	}
	c := goldenGitComponent()
	if got := fingerprintGoldenGit(c, valuesFromSpec(c)); got != goldenGitFingerprint {
		t.Fatalf("embeddedChartFingerprint changed: got %s want %s", got, goldenGitFingerprint)
	}
}

func TestDigest_ChangesWithValuesFrom(t *testing.T) {
	vf := wantRunaiValuesFrom()
	in := goldenNoOverlayInputs()
	in.ValuesFrom = vf
	if got := renderDigestLabelValue(perHelmOpRenderDigest(in)); got == goldenNoOverlayDigest {
		t.Fatalf("perHelmOpRenderDigest must change with valuesFrom")
	}
	c := goldenGitComponent()
	c.ValuesFrom = runaiValuesFrom()
	if got := fingerprintGoldenGit(c, valuesFromSpec(c)); got == goldenGitFingerprint {
		t.Fatalf("embeddedChartFingerprint must change with valuesFrom")
	}
}

func TestReconcileBlueprintStatus_ValuesFromNamespaceRestricted(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")
	for _, tc := range []struct {
		name, refNS string
		refused     bool
	}{
		{"foreign namespace", "kube-system", true},
		{"workload target namespace", "install-ns", false},
		{"release namespace", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newBlueprintWorkload(time.Now().Add(-24 * time.Hour))
			bp := newBlueprintCR("application-collection")
			bp.Spec.Components[0].ValuesFrom = []aiplatformv1alpha1.ComponentValuesFrom{{
				SecretKeyRef: &aiplatformv1alpha1.ValuesFromKeyRef{Name: "vals", Namespace: tc.refNS},
			}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo.DeepCopy()).
				WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).Build()
			r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

			if _, _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
				t.Fatal(err)
			}
			cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
			refused := cond != nil && cond.Reason == reasonInvalidValuesFrom
			if refused != tc.refused {
				t.Fatalf("refused = %v, want %v (condition %+v)", refused, tc.refused, cond)
			}
			helmOp := &unstructured.Unstructured{}
			helmOp.SetGroupVersionKind(helmOpGVK)
			err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(w.Name, "app")}, helmOp)
			if rendered := err == nil; rendered == tc.refused {
				t.Fatalf("rendered = %v with refused = %v", rendered, tc.refused)
			}
		})
	}
}
