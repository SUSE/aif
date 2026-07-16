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

package rancher

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

var testClusterRepoGVK = schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"}

func newClusterRepoTestScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	scheme := kruntime.NewScheme()
	scheme.AddKnownTypeWithName(testClusterRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})
	return scheme
}

func newHelmExtension(name, version string) *v1alpha1.InstallAIExtension {
	return &v1alpha1.InstallAIExtension{
		Spec: v1alpha1.InstallAIExtensionSpec{
			Extension: v1alpha1.ExtensionConfig{Name: name, Version: version},
			Source: v1alpha1.ExtensionSource{
				Kind: v1alpha1.ExtensionSourceKindHelm,
				Helm: &v1alpha1.HelmSource{ChartURL: "https://example.com/chart", Version: version},
			},
		},
	}
}

func getClusterRepo(t *testing.T, c client.Client, name string) *unstructured.Unstructured {
	t.Helper()
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(testClusterRepoGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Name: name}, repo); err != nil {
		t.Fatalf("get ClusterRepo %q: %v", name, err)
	}
	return repo
}

func forceUpdateValue(t *testing.T, repo *unstructured.Unstructured) string {
	t.Helper()
	v, _, err := unstructured.NestedString(repo.Object, "spec", "forceUpdate")
	if err != nil {
		t.Fatalf("read spec.forceUpdate: %v", err)
	}
	return v
}

// On the first ensure of a brand-new ClusterRepo, the operator must set
// spec.forceUpdate so Rancher downloads the index immediately, and record the
// synced version so subsequent reconciles are idempotent.
func TestEnsureClusterRepo_SetsForceUpdateOnFirstEnsure(t *testing.T) {
	scheme := newClusterRepoTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	m := NewManager(c)

	ext := newHelmExtension("my-plugin", "1.0.0")
	if err := m.EnsureClusterRepo(context.Background(), ext, "http://svc.ns:8080"); err != nil {
		t.Fatalf("EnsureClusterRepo: %v", err)
	}

	repo := getClusterRepo(t, c, "my-plugin")

	if fu := forceUpdateValue(t, repo); fu == "" {
		t.Fatalf("expected spec.forceUpdate to be set, got empty")
	}
	if got := repo.GetAnnotations()[annotationSyncedVersion]; got != "1.0.0" {
		t.Fatalf("expected synced-version annotation %q, got %q", "1.0.0", got)
	}
}

// When reconciling the same version repeatedly, the operator must NOT re-stamp
// forceUpdate — otherwise Rancher re-downloads the index on every 60s health
// check.
func TestEnsureClusterRepo_IdempotentWhenVersionUnchanged(t *testing.T) {
	scheme := newClusterRepoTestScheme(t)

	const preexistingForceUpdate = "2020-01-01T00:00:00Z"
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(testClusterRepoGVK)
	existing.SetName("my-plugin")
	existing.SetAnnotations(map[string]string{annotationSyncedVersion: "1.0.0"})
	_ = unstructured.SetNestedField(existing.Object, "http://svc.ns:8080", "spec", "url")
	_ = unstructured.SetNestedField(existing.Object, preexistingForceUpdate, "spec", "forceUpdate")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	m := NewManager(c)

	ext := newHelmExtension("my-plugin", "1.0.0")
	if err := m.EnsureClusterRepo(context.Background(), ext, "http://svc.ns:8080"); err != nil {
		t.Fatalf("EnsureClusterRepo: %v", err)
	}

	repo := getClusterRepo(t, c, "my-plugin")
	if fu := forceUpdateValue(t, repo); fu != preexistingForceUpdate {
		t.Fatalf("expected forceUpdate unchanged (%q), got %q", preexistingForceUpdate, fu)
	}
}

// When the extension version changes (the upgrade case), the operator must
// re-stamp forceUpdate so Rancher re-downloads the index and the UI shows the
// new version, and update the recorded synced version.
func TestEnsureClusterRepo_BumpsForceUpdateOnVersionChange(t *testing.T) {
	scheme := newClusterRepoTestScheme(t)

	const oldForceUpdate = "2000-01-01T00:00:00Z"
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(testClusterRepoGVK)
	existing.SetName("my-plugin")
	existing.SetAnnotations(map[string]string{annotationSyncedVersion: "1.0.0"})
	_ = unstructured.SetNestedField(existing.Object, "http://svc.ns:8080", "spec", "url")
	_ = unstructured.SetNestedField(existing.Object, oldForceUpdate, "spec", "forceUpdate")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	m := NewManager(c)

	ext := newHelmExtension("my-plugin", "2.0.0")
	if err := m.EnsureClusterRepo(context.Background(), ext, "http://svc.ns:8080"); err != nil {
		t.Fatalf("EnsureClusterRepo: %v", err)
	}

	repo := getClusterRepo(t, c, "my-plugin")
	if fu := forceUpdateValue(t, repo); fu == oldForceUpdate || fu == "" {
		t.Fatalf("expected forceUpdate to be re-stamped, got %q", fu)
	}
	if got := repo.GetAnnotations()[annotationSyncedVersion]; got != "2.0.0" {
		t.Fatalf("expected synced-version annotation %q, got %q", "2.0.0", got)
	}
}
