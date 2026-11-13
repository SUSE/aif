package aiworkload

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
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

func TestResolveKustomizeOverlay_InlineRejectsFileOutsideDir(t *testing.T) {
	scheme := gitRepoTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}

	c := gitComponent()
	// A single overlay is not re-keyed the way layered ones are, so a key
	// outside dir would land in the Bundle beside, or over, the chart's files.
	c.Kustomize = &aiplatformv1alpha1.ComponentKustomize{
		Dir: "overlay",
		Files: map[string]string{
			"overlay/kustomization.yaml": "patches: []",
			"mychart/templates/x.yaml":   "a: 1",
		},
	}

	if _, _, err := r.resolveKustomizeOverlay(context.Background(), c); err == nil ||
		!strings.Contains(err.Error(), "outside overlay dir") {
		t.Fatalf("want a file-outside-dir error, got %v", err)
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

func overlayGitRepo(custom bool, commit string) *unstructured.Unstructured {
	// The URL is unreachable: a fingerprint that tried to clone would fail.
	repo := repoObj("overlays", map[string]any{"gitRepo": "https://127.0.0.1:1/overlays.git", "gitBranch": "main"})
	if custom {
		repo.SetLabels(map[string]string{credentials.CustomRepoLabel: credentials.LabelValueTrue})
	}
	if commit != "" {
		_ = unstructured.SetNestedField(repo.Object, commit, "status", "commit")
	}
	return repo
}

func gitOverlayComponent() aiplatformv1alpha1.BlueprintComponent {
	c := gitComponent()
	c.Kustomize = &aiplatformv1alpha1.ComponentKustomize{
		Dir:    "overlay",
		GitRef: &aiplatformv1alpha1.ComponentKustomizeGitRef{ChartRepo: "overlays", Path: "prod"},
	}
	return c
}

func TestResolveKustomizeOverlay_GitRefRequiresCustomRepo(t *testing.T) {
	scheme := gitRepoTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overlayGitRepo(false, "abc")).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}
	_, _, err := r.resolveKustomizeOverlay(context.Background(), gitOverlayComponent())
	if err == nil || !strings.Contains(err.Error(), "not a custom repository") {
		t.Fatalf("want a not-a-custom-repository error, got %v", err)
	}
}

func TestResolveKustomizeOverlay_GitRefRejectsSSHRepo(t *testing.T) {
	scheme := gitRepoTestScheme()
	for _, url := range []string{"git@git.example.com:org/overlays.git", "ssh://git@git.example.com/org/overlays.git"} {
		repo := overlayGitRepo(true, "abc")
		_ = unstructured.SetNestedField(repo.Object, url, "spec", "gitRepo")
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
		r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}
		_, _, err := r.resolveKustomizeOverlay(context.Background(), gitOverlayComponent())
		if err == nil || !strings.Contains(err.Error(), "SSH is not supported") {
			t.Fatalf("%s: want an SSH-not-supported error, got %v", url, err)
		}
	}
}

func TestOverlayFingerprint_GitRefUsesCommitWithoutCloning(t *testing.T) {
	scheme := gitRepoTestScheme()
	w := newBlueprintWorkload(time.Now())
	c := gitOverlayComponent()
	fingerprint := func(commit string) string {
		t.Helper()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overlayGitRepo(true, commit)).Build()
		r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}
		d, o, err := r.overlayFingerprint(context.Background(), w, c)
		if err != nil {
			t.Fatalf("overlayFingerprint: %v", err)
		}
		if d == "" || o != nil {
			t.Fatalf("digest=%q render=%v, want a digest and a deferred render", d, o)
		}
		return d
	}
	if fingerprint("abc") == fingerprint("def") {
		t.Fatalf("a new overlay repo commit must change the fingerprint")
	}
}

func TestOverlayFingerprint_GitRefNeedsIndexedRepo(t *testing.T) {
	scheme := gitRepoTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overlayGitRepo(true, "")).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}
	if _, _, err := r.overlayFingerprint(context.Background(), newBlueprintWorkload(time.Now()), gitOverlayComponent()); err == nil ||
		!strings.Contains(err.Error(), "not been indexed") {
		t.Fatalf("want a not-indexed error, got %v", err)
	}
}
