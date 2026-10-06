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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestResolveWorkloadScope_App(t *testing.T) {
	scheme := newAppTestScheme(t)
	r := &AIWorkloadReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme}

	scope, err := r.resolveWorkloadScope(context.Background(), newCustomTestWorkload(nil))
	if err != nil {
		t.Fatalf("resolveWorkloadScope: %v", err)
	}
	if !scope.resolved {
		t.Error("scope not resolved")
	}
	if !scope.repos[customTestRepo] || len(scope.repos) != 1 {
		t.Errorf("repos = %v, want only %q", scope.repos, customTestRepo)
	}
	if !scope.releases[customTestTargetNS]["demo"] {
		t.Errorf("releases = %v, want release demo in %s", scope.releases, customTestTargetNS)
	}
}

// A Blueprint workload's scope covers its enabled components only, with each
// component's repository, namespace and release name.
func TestResolveWorkloadScope_Blueprint(t *testing.T) {
	scheme := newAppTestScheme(t)
	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: bpCRName("rag", "1.0.0")},
		Spec: aiplatformv1alpha1.BlueprintSpec{Components: []aiplatformv1alpha1.BlueprintComponent{
			{ChartRepo: "private-charts", ChartName: "vector-db", TargetNamespace: "db-ns", ReleaseName: "vdb"},
			{ChartRepo: "application-collection", ChartName: "open-webui"},
			{ChartRepo: "other-charts", ChartName: "disabled-one"},
		}},
	}
	disabled := false
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "app-ns",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: "1.0.0"},
			},
			ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "disabled-one", Enabled: &disabled}},
		},
	}
	r := &AIWorkloadReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(bp).Build(), Scheme: scheme}

	scope, err := r.resolveWorkloadScope(context.Background(), w)
	if err != nil {
		t.Fatalf("resolveWorkloadScope: %v", err)
	}
	if !scope.repos["private-charts"] || !scope.repos["application-collection"] || scope.repos["other-charts"] {
		t.Errorf("repos = %v, want the enabled components' repositories only", scope.repos)
	}
	if !scope.releases["db-ns"]["vdb"] || !scope.releases["app-ns"]["open-webui"] {
		t.Errorf("releases = %v, want vdb in db-ns and open-webui in app-ns", scope.releases)
	}
}

// A Blueprint that cannot be found yields an empty scope: nothing is delivered
// or recreated on the workload's behalf until it resolves.
func TestResolveWorkloadScope_MissingBlueprintIsEmpty(t *testing.T) {
	scheme := newAppTestScheme(t)
	r := &AIWorkloadReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme}
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{Source: aiplatformv1alpha1.AIWorkloadSource{
			SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
			Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "gone", Version: "1.0.0"},
		}},
	}
	scope, err := r.resolveWorkloadScope(context.Background(), w)
	if err != nil {
		t.Fatalf("resolveWorkloadScope: %v", err)
	}
	if len(scope.repos) != 0 || len(scope.releases) != 0 || scope.resolved {
		t.Errorf("scope = %+v, want empty and unresolved", scope)
	}
}

// The factory builds a custom repository's pull secret only for a repository
// the workload uses, whatever names its status lists.
func TestPullSecretFactory_RefusesRepositoryOutsideTheWorkload(t *testing.T) {
	scheme := newAppTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		newCustomTestRepo("oci://ghcr.io/example/charts"),
		newCustomTestAuthSecret("repo-user", "repo-token")).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: customTestOpNS}

	scope := workloadScope{repos: map[string]bool{"some-other-repo": true}}
	sec, err := r.pullSecretFactory(context.Background(), scope)(customTestTargetNS, customRepoPullSecretName(customTestRepo))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if sec != nil {
		t.Errorf("factory built %s for a repository the workload does not use", sec.Name)
	}
}

// When a workload's scope cannot be resolved (its Blueprint is gone), the
// existing deliveries are left as they are: re-applying them with a narrowed
// scope would withdraw a custom repository's secret from running workloads.
func TestReconcileDeliveredPullSecrets_LeavesDeliveriesAloneWhenScopeUnresolved(t *testing.T) {
	scheme := newAppTestScheme(t)
	bundleGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleGVK.GroupVersion().WithKind("BundleList"), &unstructured.UnstructuredList{})
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(bundleGVK)
	existing.SetNamespace("fleet-default")
	existing.SetName("ai-pullsecrets-wl-c-aaa-db-ns")
	_ = unstructured.SetNestedField(existing.Object, "previously delivered", "spec", "marker")

	// SUSE credentials exist, so the combined secret would still build: a
	// re-apply would ship it alone and withdraw the custom repository's one.
	objs := append(newSUSESettingsWithAppCo(), existing,
		newCustomTestRepo("oci://ghcr.io/example/charts"), newCustomTestAuthSecret("u", "p"))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: customTestOpNS}
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "db-ns", TargetClusters: []string{"c-aaa"},
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "gone", Version: "1.0.0"},
			},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
			{Namespace: "db-ns", Names: []string{combinedPullSecretName, customRepoPullSecretName(customTestRepo)}},
		}},
	}

	settled, err := r.reconcileDeliveredPullSecrets(context.Background(), w)
	if err != nil {
		t.Fatalf("reconcileDeliveredPullSecrets: %v", err)
	}
	if !settled {
		t.Error("settled = false, want true (nothing to do until the scope resolves)")
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(bundleGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-default", Name: existing.GetName()}, got); err != nil {
		t.Fatalf("get bundle: %v", err)
	}
	if resources, found, _ := unstructured.NestedSlice(got.Object, "spec", "resources"); found {
		t.Errorf("existing bundle was re-applied with %d resources", len(resources))
	}
}
