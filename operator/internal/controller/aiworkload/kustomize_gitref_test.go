package aiworkload

import (
	"context"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestResolveKustomizeOverlay_InlineFilesReturnedVerbatim(t *testing.T) {
	scheme := gitRepoTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}

	c := gitComponent()
	c.Kustomize = &aiplatformv1alpha1.ComponentKustomize{
		Dir:   "overlay",
		Files: map[string]string{"overlay/kustomization.yaml": "a: 1"},
	}

	files, commit, err := r.resolveKustomizeOverlay(context.Background(), c)
	if err != nil {
		t.Fatalf("resolveKustomizeOverlay: %v", err)
	}
	if commit != "" {
		t.Fatalf("commit = %q, want empty for inline files", commit)
	}
	if len(files) != 1 || files["overlay/kustomization.yaml"] != "a: 1" {
		t.Fatalf("files = %v, want inline Files returned verbatim", files)
	}
}

func TestResolveKustomizeOverlay_InlineRejectsMissingKustomization(t *testing.T) {
	scheme := gitRepoTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}

	c := gitComponent()
	c.Kustomize = &aiplatformv1alpha1.ComponentKustomize{
		Dir:   "overlay",
		Files: map[string]string{"overlay/patch.yaml": "a: 1"}, // no kustomization.yaml
	}

	if _, _, err := r.resolveKustomizeOverlay(context.Background(), c); err == nil ||
		!strings.Contains(err.Error(), "no kustomization.yaml") {
		t.Fatalf("want a clear missing-kustomization.yaml error, got %v", err)
	}
}

func TestResolveKustomizeOverlay_GitRefRequiresGitBackedRepo(t *testing.T) {
	scheme := gitRepoTestScheme()
	// An http-backed ClusterRepo (has spec.url, no gitRepo) — not git-backed.
	repo := repoObj("app-collection", map[string]any{"url": "https://charts.example.com"})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}

	c := gitComponent()
	c.Kustomize = &aiplatformv1alpha1.ComponentKustomize{
		Dir: "overlay",
		GitRef: &aiplatformv1alpha1.ComponentKustomizeGitRef{
			ChartRepo: "app-collection",
			Path:      "overlays/prod",
		},
	}

	_, _, err := r.resolveKustomizeOverlay(context.Background(), c)
	if err == nil {
		t.Fatal("expected an error for a non-git-backed ClusterRepo")
	}
	if !strings.Contains(err.Error(), "not git-backed") {
		t.Fatalf("error = %v, want a clear not-git-backed message", err)
	}
}

func TestRekeyUnderDir(t *testing.T) {
	in := map[string]string{"kustomization.yaml": "x", "sub/patch.yaml": "y"}
	want := map[string]string{"overlay/kustomization.yaml": "x", "overlay/sub/patch.yaml": "y"}
	got := rekeyUnderDir(in, "overlay")
	if len(got) != len(want) {
		t.Fatalf("rekeyUnderDir(%v, %q) = %v, want %v", in, "overlay", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("rekeyUnderDir(%v, %q) = %v, want %v", in, "overlay", got, want)
		}
	}

	same := rekeyUnderDir(in, "")
	if len(same) != len(in) {
		t.Fatalf("rekeyUnderDir(in, \"\") = %v, want unchanged %v", same, in)
	}
	for k, v := range in {
		if same[k] != v {
			t.Fatalf("rekeyUnderDir(in, \"\") = %v, want unchanged %v", same, in)
		}
	}
}

func TestResolveKustomizeOverlay_NilKustomizeReturnsNil(t *testing.T) {
	scheme := gitRepoTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}

	files, commit, err := r.resolveKustomizeOverlay(context.Background(), gitComponent())
	if err != nil || files != nil || commit != "" {
		t.Fatalf("files=%v commit=%q err=%v, want all zero values for nil Kustomize", files, commit, err)
	}
}
