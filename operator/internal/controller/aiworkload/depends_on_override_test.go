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

// reconcileDepsOverride reconciles a two-component Blueprint (a, b on an HTTP
// repo) with the given Blueprint dependsOn for b and install-time overrides,
// returning the reconciled workload and the client holding the rendered HelmOps.
func reconcileDepsOverride(t *testing.T, bDeps []string, overrides []aiplatformv1alpha1.ComponentValueOverride) (*aiplatformv1alpha1.AIWorkload, *AIWorkloadReconciler) {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)

	w := newBlueprintWorkload(time.Now())
	w.Spec.Source.Blueprint = &aiplatformv1alpha1.BlueprintSource{Name: "deps", Version: "1.0.0"}
	w.Spec.ComponentValues = overrides

	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: bpCRName("deps", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: "application-collection", ChartName: "a", ChartVersion: "1.0.0", Vendor: "suse"},
		{ChartRepo: "application-collection", ChartName: "b", ChartVersion: "1.0.0", Vendor: "suse", DependsOn: bDeps},
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}
	if _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcileBlueprintStatus: %v", err)
	}
	return w, r
}

func helmOpDependsOn(t *testing.T, r *AIWorkloadReconciler, w *aiplatformv1alpha1.AIWorkload, component string) []any {
	t.Helper()
	helmOp := &unstructured.Unstructured{}
	helmOp.SetGroupVersionKind(helmOpGVK)
	key := types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(w.Name, component)}
	if err := r.Get(context.Background(), key, helmOp); err != nil {
		t.Fatalf("get HelmOp for %s: %v", component, err)
	}
	dep, _, _ := unstructured.NestedSlice(helmOp.Object, "spec", "dependsOn")
	return dep
}

func depsOverride(name string, deps ...string) aiplatformv1alpha1.ComponentValueOverride {
	d := append([]string{}, deps...)
	return aiplatformv1alpha1.ComponentValueOverride{ComponentName: name, DependsOn: &d}
}

func TestReconcile_OverrideDependsOnRendersOnHelmOp(t *testing.T) {
	w, r := reconcileDepsOverride(t, nil, []aiplatformv1alpha1.ComponentValueOverride{depsOverride("b", "a")})
	got := helmOpDependsOn(t, r, w, "b")
	want := []any{map[string]any{"name": blueprintBundleName(w.Name, "a")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("b spec.dependsOn = %v, want %v", got, want)
	}
}

func TestReconcile_OverrideEmptyClearsBlueprintDeps(t *testing.T) {
	w, r := reconcileDepsOverride(t, []string{"a"}, []aiplatformv1alpha1.ComponentValueOverride{depsOverride("b")})
	if got := helmOpDependsOn(t, r, w, "b"); len(got) > 0 {
		t.Fatalf("b spec.dependsOn = %v, want none (override cleared blueprint order)", got)
	}
}

func TestReconcile_OverrideCycleFallsBackAndReports(t *testing.T) {
	w, r := reconcileDepsOverride(t, []string{"a"}, []aiplatformv1alpha1.ComponentValueOverride{depsOverride("a", "b")})

	readyCond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %+v", readyCond)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeDependencyExcluded)
	if cond == nil || cond.Status != metav1.ConditionTrue || !strings.Contains(cond.Message, "Deployed with dependency fallbacks") {
		t.Fatalf("expected %q=True naming the fallback, got %+v", conditionTypeDependencyExcluded, cond)
	}
	// a reverted to its blueprint order (none); b keeps its blueprint dependency on a.
	if got := helmOpDependsOn(t, r, w, "a"); len(got) > 0 {
		t.Fatalf("a spec.dependsOn = %v, want none after fallback", got)
	}
	if got := helmOpDependsOn(t, r, w, "b"); len(got) != 1 {
		t.Fatalf("b spec.dependsOn = %v, want [a]", got)
	}
}

func TestIsWorkloadCustomized_DependsOnOrKustomizeOverride(t *testing.T) {
	bp := &aiplatformv1alpha1.Blueprint{}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{comp("a"), comp("b", "a")}

	cases := []struct {
		name     string
		override []aiplatformv1alpha1.ComponentValueOverride
		want     bool
	}{
		{"no overrides", nil, false},
		{"enabled toggle only (still enabled)", []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "a", Enabled: ptrBool(true)}}, false},
		{"dependsOn override", []aiplatformv1alpha1.ComponentValueOverride{depsOverride("b", "a")}, true},
		{"empty dependsOn override", []aiplatformv1alpha1.ComponentValueOverride{depsOverride("b")}, true},
		{"kustomize override", []aiplatformv1alpha1.ComponentValueOverride{{
			ComponentName: "a",
			Kustomize:     &aiplatformv1alpha1.ComponentKustomize{Dir: "overlay", Files: map[string]string{"overlay/kustomization.yaml": "patches: []"}},
		}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &aiplatformv1alpha1.AIWorkload{Spec: aiplatformv1alpha1.AIWorkloadSpec{ComponentValues: tc.override}}
			got, err := isWorkloadCustomized(w, bp)
			if err != nil {
				t.Fatalf("isWorkloadCustomized: %v", err)
			}
			if got != tc.want {
				t.Fatalf("isWorkloadCustomized = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDependencyConditionMessage(t *testing.T) {
	cases := []struct {
		name         string
		edges, notes []string
		want         string
	}{
		{"both empty", nil, nil, ""},
		{"edges only", []string{"a -> b"}, nil, "Deployed without excluded dependencies: a -> b"},
		{"notes only", nil, []string{"a: x"}, "Deployed with dependency fallbacks: a: x"},
		{"both", []string{"a -> b", "c -> b"}, []string{"a: x", "c: y"},
			"Deployed without excluded dependencies: a -> b, c -> b; Deployed with dependency fallbacks: a: x, c: y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dependencyConditionMessage(tc.edges, tc.notes); got != tc.want {
				t.Fatalf("dependencyConditionMessage = %q, want %q", got, tc.want)
			}
		})
	}
}
