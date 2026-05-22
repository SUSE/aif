package fleet

import (
	"strings"
	"testing"
)

func TestFleetBundleName_BasicShape(t *testing.T) {
	got := fleetBundleName("team-a", "demo-workload")
	want := "team-a-demo-workload"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFleetBundleName_LowercasesAndSanitizes(t *testing.T) {
	got := fleetBundleName("Team_A", "Demo.Workload")
	if got != "team-a-demo-workload" {
		t.Fatalf("expected sanitized lowercase, got %q", got)
	}
}

func TestFleetBundleName_TruncatesWithStableSuffix(t *testing.T) {
	longID := strings.Repeat("x", 80)
	got := fleetBundleName("ns", longID)
	if len(got) > 63 {
		t.Fatalf("length %d > 63: %q", len(got), got)
	}
	// Same input twice → identical output (suffix is stable hash).
	if fleetBundleName("ns", longID) != got {
		t.Fatal("fleetBundleName is not deterministic")
	}
}

func TestFleetBundleName_CollisionResistantAfterTruncation(t *testing.T) {
	// Two long names that share the first 55 chars but differ further out
	// MUST yield different bundle names.
	a := fleetBundleName("ns", strings.Repeat("a", 55)+"foo")
	b := fleetBundleName("ns", strings.Repeat("a", 55)+"bar")
	if a == b {
		t.Fatalf("collision: %q == %q", a, b)
	}
}
