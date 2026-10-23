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
)

// TestReconcileBlueprintStatus_HelmOpSpecShape pins WHERE the kustomize overlay,
// resources, and dependsOn land in the rendered HelmOp. Fleet's HelmOpSpec embeds
// BundleSpec→BundleDeploymentOptions inline, so spec.kustomize / spec.resources /
// spec.dependsOn are the real fields and spec.helm has no such keys. A regression
// that nests kustomize/resources under spec.helm makes the overlay a silent no-op
// (Fleet's structural schema drops the unknown keys), which this test catches.
func TestReconcileBlueprintStatus_HelmOpSpecShape(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)

	w := newBlueprintWorkload(time.Now())
	w.Spec.Source.Blueprint = &aiplatformv1alpha1.BlueprintSource{Name: "kust", Version: "1.0.0"}

	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: bpCRName("kust", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{
			ChartRepo: "application-collection", ChartName: "a", ChartVersion: "1.0.0", Vendor: "suse",
			DependsOn: []string{"b"},
			Kustomize: &aiplatformv1alpha1.ComponentKustomize{
				Dir:   "overlay",
				Files: map[string]string{"overlay/kustomization.yaml": "resources: []\n"},
			},
		},
		{ChartRepo: "application-collection", ChartName: "b", ChartVersion: "1.0.0", Vendor: "suse"},
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	if _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	helmOp := &unstructured.Unstructured{}
	helmOp.SetGroupVersionKind(helmOpGVK)
	key := types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(w.Name, "a")}
	if err := c.Get(context.Background(), key, helmOp); err != nil {
		t.Fatalf("get HelmOp for component a: %v", err)
	}

	// kustomize at spec top level.
	dir, ok, _ := unstructured.NestedString(helmOp.Object, "spec", "kustomize", "dir")
	if !ok || dir != "overlay" {
		t.Errorf("want spec.kustomize.dir=overlay, got %q (present=%v)", dir, ok)
	}
	// resources at spec top level.
	if res, ok, _ := unstructured.NestedSlice(helmOp.Object, "spec", "resources"); !ok || len(res) == 0 {
		t.Errorf("want non-empty spec.resources, got present=%v len=%d", ok, len(res))
	}
	// dependsOn at spec top level (b's bundle; b is enabled).
	if dep, ok, _ := unstructured.NestedSlice(helmOp.Object, "spec", "dependsOn"); !ok || len(dep) != 1 {
		t.Errorf("want one spec.dependsOn entry, got present=%v len=%d", ok, len(dep))
	}

	// None of these may live under spec.helm — Fleet would silently drop them.
	if _, ok, _ := unstructured.NestedMap(helmOp.Object, "spec", "helm", "kustomize"); ok {
		t.Errorf("spec.helm.kustomize must not be set (kustomize belongs at spec.kustomize)")
	}
	if _, ok, _ := unstructured.NestedSlice(helmOp.Object, "spec", "helm", "resources"); ok {
		t.Errorf("spec.helm.resources must not be set (resources belong at spec.resources)")
	}
}
