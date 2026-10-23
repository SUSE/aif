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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// TestExcludedDependencyEdges_ReportsDroppedEdges is a plain unit test of the
// helper that feeds the DependencyExcluded condition.
func TestExcludedDependencyEdges_ReportsDroppedEdges(t *testing.T) {
	w := &aiplatformv1alpha1.AIWorkload{Spec: aiplatformv1alpha1.AIWorkloadSpec{
		ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "b", Enabled: ptrBool(false)}},
	}}
	components := []aiplatformv1alpha1.BlueprintComponent{comp("a", "b"), comp("b")}
	got := excludedDependencyEdges(w, components)
	want := []string{"a -> b"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("excludedDependencyEdges = %v, want %v", got, want)
	}
}

// TestReconcileBlueprintStatus_ExcludedDependencyDroppedFromRender exercises
// reconcileBlueprintStatus end to end (fake client, no envtest) for a Blueprint
// where enabled component "a" dependsOn excluded component "b": Ready must stay
// True, DependencyExcluded must go True naming "a -> b", and a's rendered HelmOp
// must carry no spec.dependsOn entry for b's bundle.
func TestReconcileBlueprintStatus_ExcludedDependencyDroppedFromRender(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)

	w := newBlueprintWorkload(time.Now())
	w.Spec.Source.Blueprint = &aiplatformv1alpha1.BlueprintSource{Name: "deps", Version: "1.0.0"}
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "b", Enabled: ptrBool(false)}}

	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: bpCRName("deps", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: "application-collection", ChartName: "a", ChartVersion: "1.0.0", Vendor: "suse", DependsOn: []string{"b"}},
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

	readyCond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %+v", readyCond)
	}
	if w.Status.Phase == aiplatformv1alpha1.AIWorkloadPhaseFailed {
		t.Fatalf("expected phase not Failed, got %v", w.Status.Phase)
	}

	excludedCond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeDependencyExcluded)
	if excludedCond == nil || excludedCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected %q=True, got %+v", conditionTypeDependencyExcluded, excludedCond)
	}
	if !strings.Contains(excludedCond.Message, "a -> b") {
		t.Errorf("expected message to name the dropped edge \"a -> b\", got %q", excludedCond.Message)
	}

	helmOpKey := types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(w.Name, "a")}
	helmOp := &unstructured.Unstructured{}
	helmOp.SetGroupVersionKind(helmOpGVK)
	if err := c.Get(context.Background(), helmOpKey, helmOp); err != nil {
		t.Fatalf("get HelmOp for component a: %v", err)
	}
	if dep, ok, _ := unstructured.NestedSlice(helmOp.Object, "spec", "dependsOn"); ok && len(dep) > 0 {
		t.Fatalf("expected no spec.dependsOn on a's HelmOp (b is excluded), got %v", dep)
	}

	// b is excluded entirely, so its HelmOp must never be created.
	bHelmOp := &unstructured.Unstructured{}
	bHelmOp.SetGroupVersionKind(helmOpGVK)
	bKey := types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(w.Name, "b")}
	if err := c.Get(context.Background(), bKey, bHelmOp); err == nil {
		t.Fatalf("expected no HelmOp for excluded component b, found one")
	}
}
