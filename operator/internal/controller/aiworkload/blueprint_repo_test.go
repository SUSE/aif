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

// A git-backed repo tracks a branch, so the same chart version can change
// underneath us. status.commit is the only input that reveals it, and the chart
// fingerprint depends on it — see gitChartFingerprint.
func TestResolveClusterRepo_GitCommit(t *testing.T) {
	repo := repoObj("repo", map[string]any{
		"gitRepo": "https://git.rancher.io/charts", "gitBranch": "release-v2.14",
	})
	_ = unstructured.SetNestedField(repo.Object, "0b3e1f2c4d5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c", "status", "commit")

	r := newRepoReconciler(repo)
	got, err := r.resolveClusterRepo(context.Background(), "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Commit != "0b3e1f2c4d5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c" {
		t.Fatalf("Commit = %q, want the ClusterRepo's status.commit", got.Commit)
	}
}

// A url-backed ClusterRepo carries no status.commit, and a published version is
// immutable there, so the field must stay empty rather than pick up noise.
func TestResolveClusterRepo_HTTPHasNoCommit(t *testing.T) {
	r := newRepoReconciler(repoObj("repo", map[string]any{"url": "https://charts.example.com"}))
	got, err := r.resolveClusterRepo(context.Background(), "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Commit != "" {
		t.Fatalf("Commit = %q, want empty for a url-backed repo", got.Commit)
	}
}

func TestResolveClusterRepo_NoSource(t *testing.T) {
	r := newRepoReconciler(repoObj("repo", map[string]any{}))
	if _, err := r.resolveClusterRepo(context.Background(), "repo"); err == nil {
		t.Fatal("expected error for repo with no url/ociRepo/gitRepo")
	}
}
