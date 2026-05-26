package git_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/SUSE/aif/pkg/git"
)

func TestManifestFilename_SmallIndex(t *testing.T) {
	got := git.ManifestFilename(0, "nim-llm")
	want := "10-nim-llm.yaml"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestManifestFilename_DoubleDigit(t *testing.T) {
	got := git.ManifestFilename(10, "nim-llm")
	want := "20-nim-llm.yaml"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestManifestFilename_OutOfRange(t *testing.T) {
	if got := git.ManifestFilename(-1, "x"); got != "" {
		t.Fatalf("negative index should yield empty; got %q", got)
	}
	if got := git.ManifestFilename(git.MaxComponentIndex+1, "x"); got != "" {
		t.Fatalf("over-max index should yield empty; got %q", got)
	}
}

// TestManifestFilename_SortOrder is the regression guardrail: even with
// 11+ components mixed with the engine-owned 00-namespace.yaml, a
// lexicographic sort must match the numeric ordering.
func TestManifestFilename_SortOrder(t *testing.T) {
	files := []string{"00-namespace.yaml"}
	for i := 0; i < 15; i++ {
		files = append(files, git.ManifestFilename(i, "c"))
	}
	sorted := append([]string{}, files...)
	sort.Strings(sorted)

	for i := range files {
		if files[i] != sorted[i] {
			t.Fatalf("lexicographic sort diverges from numeric order at i=%d\n  insert: %v\n  sorted: %v",
				i, files, sorted)
		}
	}
}

func TestSanitizeComponentName_LowercaseAndStrip(t *testing.T) {
	got := git.SanitizeComponentName("My Component_Name!")
	if got != "my-component-name" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeComponentName_CollisionSuffix(t *testing.T) {
	seen := map[string]struct{}{}
	a := git.SanitizeComponentNameUnique("My Component", seen)
	seen[a] = struct{}{}
	b := git.SanitizeComponentNameUnique("My_Component", seen)
	if a == b {
		t.Fatalf("expected unique names, got %q and %q", a, b)
	}
	if !strings.HasPrefix(b, "my-component-") || len(b) != len("my-component-")+4 {
		t.Fatalf("expected 4-char suffix on collision; got %q", b)
	}
}
