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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

func inlineOverlay(dir, body string) *aiplatformv1alpha1.ComponentKustomize {
	return &aiplatformv1alpha1.ComponentKustomize{Dir: dir, Files: map[string]string{dir + "/kustomization.yaml": body}}
}

// compChartTgz is the archive the fake puller serves for chart "comp".
func compChartTgz(t *testing.T) []byte {
	return makeChartTgz(t, map[string]string{
		"comp/Chart.yaml":         "apiVersion: v2\nname: comp\nversion: 1.0.0\n",
		"comp/templates/svc.yaml": "kind: Service\n",
	})
}

// reconcileOverlay reconciles a one-component Blueprint on an HTTP repo with the
// given Blueprint and install overlays (either may be nil); the chart is served
// by a fake direct puller.
func reconcileOverlay(t *testing.T, bpOverlay, install *aiplatformv1alpha1.ComponentKustomize, epoch string) (*aiplatformv1alpha1.AIWorkload, client.Client) {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)

	w := newBlueprintWorkload(time.Now())
	w.Spec.Source.Blueprint = &aiplatformv1alpha1.BlueprintSource{Name: "ov", Version: "1.0.0"}
	if epoch != "" {
		w.Annotations = map[string]string{retryEpochAnnotation: epoch}
	}
	if install != nil {
		w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "comp", Kustomize: install}}
	}
	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: bpCRName("ov", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: "application-collection", ChartName: "comp", ChartVersion: "1.0.0", Vendor: "suse", Kustomize: bpOverlay},
	}
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	tgz := compChartTgz(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo).Build()
	r := &AIWorkloadReconciler{
		Client: c, Scheme: scheme, OperatorNamespace: "aif-operator",
		ChartPuller: func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { return tgz, nil },
	}
	if _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcileBlueprintStatus: %v", err)
	}
	return w, c
}

func fleetObject(t *testing.T, c client.Client, kind, name string) (*unstructured.Unstructured, bool) {
	t.Helper()
	gvk := bundleGVK
	if kind == "HelmOp" {
		gvk = helmOpGVK
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: name}, u)
	return u, err == nil
}

func helmOpResources(t *testing.T, spec map[string]any) map[string]string {
	t.Helper()
	raw, _, _ := unstructured.NestedSlice(spec, "resources")
	out := map[string]string{}
	for _, r := range raw {
		m := r.(map[string]any)
		out[m["name"].(string)], _ = m["content"].(string)
	}
	return out
}

func TestRender_OverlayComponentOnHTTPRepoIsEmbeddedBundle(t *testing.T) {
	w, c := reconcileOverlay(t, nil, inlineOverlay("inst", emptyPatches), "2")
	name := blueprintBundleName(w.Name, "comp")
	if _, ok := fleetObject(t, c, "HelmOp", name); ok {
		t.Fatalf("a component with an overlay must not render a HelmOp")
	}
	b, ok := fleetObject(t, c, "Bundle", name)
	if !ok {
		t.Fatalf("embedded Bundle %s not found", name)
	}
	spec := b.Object["spec"].(map[string]any)
	res := helmOpResources(t, spec)
	if _, ok := res["comp/Chart.yaml"]; !ok || res["inst/kustomization.yaml"] != emptyPatches {
		t.Fatalf("resources = %v, want chart files plus the install overlay", res)
	}
	if dir, _, _ := unstructured.NestedString(spec, "kustomize", "dir"); dir != "inst" {
		t.Fatalf("spec.kustomize.dir = %q, want inst", dir)
	}
	if fsg, _, _ := unstructured.NestedInt64(spec, "forceSyncGeneration"); fsg != 2 {
		t.Fatalf("forceSyncGeneration = %d, want the retry epoch 2", fsg)
	}
	if b.GetLabels()[renderDigestLabel] == "" {
		t.Fatalf("embedded Bundle must carry the render-digest label")
	}
}

func TestRender_LayeredOverlayIsEmbedded(t *testing.T) {
	w, c := reconcileOverlay(t, inlineOverlay("overlay", emptyPatches), inlineOverlay("overlay", emptyPatches), "")
	b, ok := fleetObject(t, c, "Bundle", blueprintBundleName(w.Name, "comp"))
	if !ok {
		t.Fatalf("embedded Bundle not found")
	}
	spec := b.Object["spec"].(map[string]any)
	if dir, _, _ := unstructured.NestedString(spec, "kustomize", "dir"); dir != layeredOverlayDir {
		t.Fatalf("spec.kustomize.dir = %q, want %q", dir, layeredOverlayDir)
	}
	if !strings.Contains(helmOpResources(t, spec)["overlay/kustomization.yaml"], "components:") {
		t.Fatalf("missing generated top-level kustomization")
	}
}

func TestRender_InstallOverlayEditChangesDigest(t *testing.T) {
	digest := func(body string) string {
		w, c := reconcileOverlay(t, inlineOverlay("overlay", emptyPatches), inlineOverlay("overlay", body), "")
		b, _ := fleetObject(t, c, "Bundle", blueprintBundleName(w.Name, "comp"))
		return b.GetLabels()[renderDigestLabel]
	}
	if a, b := digest(emptyPatches), digest("commonLabels: {team: x}\n"); a == "" || a == b {
		t.Fatalf("install overlay edit must change the render digest (a=%q b=%q)", a, b)
	}
}

func TestRender_NoOverlayStaysHelmOpWithoutResources(t *testing.T) {
	w, c := reconcileOverlay(t, nil, nil, "")
	h, ok := fleetObject(t, c, "HelmOp", blueprintBundleName(w.Name, "comp"))
	if !ok {
		t.Fatalf("overlay-free component must render a HelmOp")
	}
	for _, field := range []string{"resources", "kustomize"} {
		if _, found, _ := unstructured.NestedFieldNoCopy(h.Object, "spec", field); found {
			t.Fatalf("HelmOp must not carry spec.%s", field)
		}
	}
}

func TestRender_GitRepoComponentAcceptsOverlay(t *testing.T) {
	scheme := gitRepoTestScheme()
	repo := repoObj("rancher-charts", map[string]any{"gitRepo": "https://git.rancher.io/charts", "gitBranch": "release-v2.14"})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	holder := rancher.NewHolder()
	c := gitComponent()
	holder.Set(fakeCatalog{tgz: makeChartTgz(t, map[string]string{c.ChartName + "/Chart.yaml": "apiVersion: v2\nname: x\nversion: 1\n"})})
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme, CatalogClient: holder}

	w := &aiplatformv1alpha1.AIWorkload{}
	w.Name = "wl"
	w.Spec.TargetClusters = []string{"local"}
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: c.ChartName, Kustomize: inlineOverlay("overlay", emptyPatches)}}

	if _, err := r.ensureBlueprintHelmOp(context.Background(), w, c, "wl-agent"); err != nil {
		t.Fatalf("overlay on a git-backed chart must be accepted, got %v", err)
	}
	b, ok := fleetObject(t, cl, "Bundle", "wl-agent")
	if !ok {
		t.Fatalf("embedded Bundle not found")
	}
	if dir, _, _ := unstructured.NestedString(b.Object, "spec", "kustomize", "dir"); dir != "overlay" {
		t.Fatalf("spec.kustomize.dir = %q, want overlay", dir)
	}
}

func TestRender_GitOpsOverlayPublishesEmbeddedBundle(t *testing.T) {
	ctx := context.Background()
	remoteURL := newBlueprintGitOpsRemote(t)
	scheme := gitRepoTestScheme()
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: "aif-operator"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: remoteURL, Branch: "main"}},
		},
	}
	source := repoObj("private-charts", map[string]any{"url": "oci://registry.example/charts"})
	workload := newGitOpsTestWorkload()
	component := aiplatformv1alpha1.BlueprintComponent{ChartRepo: "private-charts", ChartName: "comp", ChartVersion: "1.0.0"}
	workload.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: component.ChartName, Kustomize: inlineOverlay("inst", emptyPatches)}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(settings, source, workload).Build()
	tgz := compChartTgz(t)
	r := &AIWorkloadReconciler{
		Client: cl, Scheme: scheme, OperatorNamespace: "aif-operator",
		ChartPuller: func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { return tgz, nil },
	}

	const bundleName = "private-source-workload-comp"
	if _, err := r.ensureBlueprintGitFile(ctx, workload, component, bundleName); err != nil {
		t.Fatalf("ensureBlueprintGitFile: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(readBlueprintGitOpsFile(t, remoteURL, "workloads/"+bundleName+".yaml")), &object); err != nil {
		t.Fatalf("decode published object: %v", err)
	}
	if object["kind"] != "Bundle" {
		t.Fatalf("published kind = %v, want Bundle", object["kind"])
	}
	spec := object["spec"].(map[string]any)
	if dir, _, _ := unstructured.NestedString(spec, "kustomize", "dir"); dir != "inst" {
		t.Fatalf("GitOps spec.kustomize.dir = %q, want inst", dir)
	}
	if got := helmOpResources(t, spec); got["inst/kustomization.yaml"] != emptyPatches {
		t.Fatalf("GitOps spec.resources = %v, want the install overlay", got)
	}
}

func TestBuildOverlayRender_LayeredDigestTracksBothCommits(t *testing.T) {
	bp := &resolvedOverlay{Dir: "overlay", Files: map[string]string{"overlay/kustomization.yaml": "patches: []\n"}, Commit: "bp1"}
	inst := func(commit string) *resolvedOverlay {
		return &resolvedOverlay{Dir: "overlay", Files: map[string]string{"overlay/kustomization.yaml": "patches: []\n"}, Commit: commit}
	}
	a, err := buildOverlayRender(bp, inst("i1"))
	if err != nil {
		t.Fatalf("buildOverlayRender: %v", err)
	}
	b, err := buildOverlayRender(bp, inst("i2"))
	if err != nil {
		t.Fatalf("buildOverlayRender: %v", err)
	}
	if a.Digest["commit"] != "bp1" || a.Digest["installCommit"] != "i1" {
		t.Fatalf("digest = %v, want commit=bp1 installCommit=i1", a.Digest)
	}
	if reflect.DeepEqual(a.Digest, b.Digest) {
		t.Fatalf("a new install commit must change the overlay digest")
	}
}

func TestBuildOverlayRender_BlueprintOnlyKeepsDigestShape(t *testing.T) {
	bp := &resolvedOverlay{Dir: "ov", Files: map[string]string{"ov/kustomization.yaml": "patches: []\n"}, Commit: "c1"}
	got, err := buildOverlayRender(bp, nil)
	if err != nil {
		t.Fatalf("buildOverlayRender: %v", err)
	}
	want := map[string]any{"dir": "ov", "files": bp.Files, "commit": "c1"}
	if got.Dir != "ov" || !reflect.DeepEqual(got.Digest, want) {
		t.Fatalf("render = %+v, want dir ov and digest %v (pre-feature shape)", got, want)
	}
	if none, err := buildOverlayRender(nil, nil); err != nil || none != nil {
		t.Fatalf("no overlays must render nil, got %+v, %v", none, err)
	}
}

func TestRender_GitOpsSkipsFetchWhenUpToDate(t *testing.T) {
	e := newGitOpsSwitchEnv(t)
	e.addOverlay()
	calls := 0
	tgz := compChartTgz(t)
	e.r.ChartPuller = func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { calls++; return tgz, nil }
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("first render: %v", err)
	}
	// The GitRepo deploys the published Bundle; it carries the fingerprint.
	doc := gitOpsDocs(t, e.remote, gitOpsSwitchBundle)[0]
	deployed := &unstructured.Unstructured{Object: doc}
	if err := e.c.Create(context.Background(), deployed); err != nil {
		t.Fatal(err)
	}
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("second render: %v", err)
	}
	if calls != 1 {
		t.Fatalf("chart fetched %d times, want 1 (the deployed Bundle is already at this fingerprint)", calls)
	}
}

func TestCleanup_RemovesEmbeddedBundleOfDisabledComponent(t *testing.T) {
	w, c := reconcileOverlay(t, nil, inlineOverlay("inst", emptyPatches), "")
	name := blueprintBundleName(w.Name, "comp")
	if _, ok := fleetObject(t, c, "Bundle", name); !ok {
		t.Fatalf("setup: embedded Bundle not created")
	}
	w.Spec.ComponentValues = append(w.Spec.ComponentValues, aiplatformv1alpha1.ComponentValueOverride{ComponentName: "comp", Enabled: ptrBool(false)})
	w.Spec.ComponentValues[0].Enabled = ptrBool(false)
	r := &AIWorkloadReconciler{Client: c, Scheme: c.Scheme(), OperatorNamespace: "aif-operator"}
	if err := r.cleanupStaleHelmOps(context.Background(), w, nil); err != nil {
		t.Fatalf("cleanupStaleHelmOps: %v", err)
	}
	if _, ok := fleetObject(t, c, "Bundle", name); ok {
		t.Fatalf("the embedded Bundle of a disabled component must be removed")
	}
}
