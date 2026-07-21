package aiworkload

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newRepoReconciler(objs ...*unstructured.Unstructured) *AIWorkloadReconciler {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	scheme.AddKnownTypeWithName(clusterRepoGVK, &unstructured.Unstructured{})
	b := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		b = b.WithObjects(o)
	}
	return &AIWorkloadReconciler{Client: b.Build()}
}

func repoObj(name string, spec map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterRepoGVK)
	u.SetName(name)
	_ = unstructured.SetNestedMap(u.Object, spec, "spec")
	return u
}

func TestResolveClusterRepo_Kinds(t *testing.T) {
	cases := []struct {
		name     string
		spec     map[string]any
		wantKind repoKind
		wantURL  string
		wantGit  string
	}{
		{"http", map[string]any{"url": "https://charts.example.com"}, repoKindHTTP, "https://charts.example.com", ""},
		{"oci-url", map[string]any{"url": "oci://reg.example.com/charts"}, repoKindOCI, "oci://reg.example.com/charts", ""},
		{"ocirepo", map[string]any{"ociRepo": "oci://reg.example.com/charts"}, repoKindOCI, "oci://reg.example.com/charts", ""},
		{"git", map[string]any{"gitRepo": "https://git.rancher.io/charts", "gitBranch": "release-v2.14"}, repoKindGit, "", "https://git.rancher.io/charts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRepoReconciler(repoObj("repo", tc.spec))
			got, err := r.resolveClusterRepo(context.Background(), "repo")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Kind != tc.wantKind || got.URL != tc.wantURL || got.GitRepo != tc.wantGit {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestResolveClusterRepo_GitBranch(t *testing.T) {
	r := newRepoReconciler(repoObj("repo", map[string]any{
		"gitRepo": "https://git.rancher.io/charts", "gitBranch": "release-v2.14",
	}))
	got, err := r.resolveClusterRepo(context.Background(), "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GitBranch != "release-v2.14" {
		t.Fatalf("GitBranch = %q", got.GitBranch)
	}
}

func TestResolveClusterRepo_NoSource(t *testing.T) {
	r := newRepoReconciler(repoObj("repo", map[string]any{}))
	if _, err := r.resolveClusterRepo(context.Background(), "repo"); err == nil {
		t.Fatal("expected error for repo with no url/ociRepo/gitRepo")
	}
}
