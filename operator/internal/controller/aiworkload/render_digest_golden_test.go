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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
)

// TestRenderDigest_BlueprintOverlayOnlyIsUnchanged pins the render-digest for a
// component with a Blueprint overlay but no install-time override. This test must
// pass both before and after Task 1 changes to prove the upgradability constraint:
// a workload that sets neither new field must render byte-identical HelmOp/Bundle
// specs and digests to today's (no redeploy after an operator upgrade).
func TestRenderDigest_BlueprintOverlayOnlyIsUnchanged(t *testing.T) {
	// Re-pinned deliberately: overlays now render as embedded-chart Bundles
	// (spec 2026-10-23-overlay-rendering-embedded-bundles-design.md), because Fleet
	// HelmOps drop spec.resources. The previous HelmOp-based value never shipped
	// (commit 01f0f961 was unreleased). Any further change is a regression.
	const expectedDigest = "b122373948a66b8ca4f35ddf44bfe5cac6edc0e2f95ba8f9f421f8cee050673"

	scheme := clusterRepoErrorScheme(t)

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "golden-wl", Namespace: "aif-operator"},
	}
	w.CreationTimestamp = metav1.NewTime(time.Now())
	w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployFleetBundle
	w.Spec.TargetNamespace = "install-ns"
	w.Spec.TargetClusters = []string{"local"}
	w.Spec.FleetBundleNames = []string{"golden-wl-comp"} // pre-populated so Step 2 is skipped
	w.Spec.Source = aiplatformv1alpha1.AIWorkloadSource{
		SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
		Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "test-bp", Version: "1.0.0"},
	}

	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: bpCRName("test-bp", "1.0.0")},
	}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{
			ChartRepo:    "application-collection",
			ChartName:    "comp",
			ChartVersion: "1.0.0",
			Vendor:       "suse",
			Kustomize: &aiplatformv1alpha1.ComponentKustomize{
				Dir: "overlay",
				Files: map[string]string{
					"overlay/kustomization.yaml": "resources: []\n",
				},
			},
		},
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo).Build()
	tgz := makeChartTgz(t, map[string]string{"comp/Chart.yaml": "apiVersion: v2\nname: comp\nversion: 1.0.0\n"})
	r := &AIWorkloadReconciler{
		Client: c, Scheme: scheme, OperatorNamespace: "aif-operator",
		ChartPuller: func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { return tgz, nil },
	}

	if _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcileBlueprintStatus failed: %v", err)
	}

	// A component with an overlay renders as an embedded-chart Bundle.
	helmOp := &unstructured.Unstructured{}
	helmOp.SetGroupVersionKind(bundleGVK)
	key := types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(w.Name, "comp")}
	if err := c.Get(context.Background(), key, helmOp); err != nil {
		t.Fatalf("get embedded Bundle for component comp: %v", err)
	}

	// An embedded Bundle carries the render-digest label in metadata.labels.
	gotDigest, ok := helmOp.GetLabels()[renderDigestLabel]
	if !ok || gotDigest == "" {
		t.Fatalf("HelmOp spec.labels missing render-digest label (ok=%v, digest=%q)", ok, gotDigest)
	}

	// On the first run, this will fail and print the actual digest. Copy that value
	// to the expectedDigest constant above.
	if gotDigest != expectedDigest {
		t.Fatalf("render-digest changed: got %q, want %q (if this is the first run, copy the 'got' value to expectedDigest)", gotDigest, expectedDigest)
	}
}

// TestRenderDigest_NoOverlayIsUnchanged pins the render-digest for a component
// with no Blueprint overlay and no install-time override. This is the baseline
// case: no kustomize field is set anywhere.
func TestRenderDigest_NoOverlayIsUnchanged(t *testing.T) {
	const expectedDigest = "fbf82471011f857db5f1324ceb48c966e149e3b4ef5a3ddbe622e09980b841d"

	scheme := clusterRepoErrorScheme(t)

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "golden-no-overlay", Namespace: "aif-operator"},
	}
	w.CreationTimestamp = metav1.NewTime(time.Now())
	w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployFleetBundle
	w.Spec.TargetNamespace = "install-ns"
	w.Spec.TargetClusters = []string{"local"}
	w.Spec.FleetBundleNames = []string{"golden-no-overlay-comp"} // pre-populated so Step 2 is skipped
	w.Spec.Source = aiplatformv1alpha1.AIWorkloadSource{
		SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
		Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "test-bp-plain", Version: "1.0.0"},
	}

	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: bpCRName("test-bp-plain", "1.0.0")},
	}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{
			ChartRepo:    "application-collection",
			ChartName:    "comp",
			ChartVersion: "1.0.0",
			Vendor:       "suse",
			// No Kustomize field set
		},
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	if _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcileBlueprintStatus failed: %v", err)
	}

	helmOp := &unstructured.Unstructured{}
	helmOp.SetGroupVersionKind(helmOpGVK)
	key := types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(w.Name, "comp")}
	if err := c.Get(context.Background(), key, helmOp); err != nil {
		t.Fatalf("get HelmOp for component comp: %v", err)
	}

	gotDigest, ok, _ := unstructured.NestedString(helmOp.Object, "spec", "labels", renderDigestLabel)
	if !ok || gotDigest == "" {
		t.Fatalf("HelmOp spec.labels missing render-digest label (ok=%v, digest=%q)", ok, gotDigest)
	}

	if gotDigest != expectedDigest {
		t.Fatalf("render-digest changed: got %q, want %q (if this is the first run, copy the 'got' value to expectedDigest)", gotDigest, expectedDigest)
	}
}
