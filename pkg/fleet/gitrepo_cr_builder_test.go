package fleet_test

import (
	"strings"
	"testing"

	"github.com/SUSE/aif/pkg/fleet"
)

func TestGitRepoName_ShortStaysVerbatim(t *testing.T) {
	got := fleet.GitRepoNameForTest("ns-a", "wl-1", "cluster-x")
	want := "ns-a-wl-1-cluster-x"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGitRepoName_LongIsTruncatedWithSHA8(t *testing.T) {
	long := strings.Repeat("a", 80)
	got := fleet.GitRepoNameForTest("ns-a", long, "cluster-x")
	if len(got) > 63 {
		t.Fatalf("expected ≤63 chars, got %d (%q)", len(got), got)
	}
	// Suffix shape is "-<8 hex>"; ensure the dash separator is intact.
	if got[len(got)-9] != '-' {
		t.Fatalf("expected dash before 8-hex suffix in %q", got)
	}
}

func TestGitRepoPath(t *testing.T) {
	got := fleet.GitRepoPathForTest("wl-1", "cluster-x")
	want := "gitops/cluster-x/wl-1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
