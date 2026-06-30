package aiworkload

import (
	"strings"
	"testing"
)

func TestCapReleaseName(t *testing.T) {
	// Names within Helm's 53-byte limit are returned unchanged.
	t.Run("passthrough when within limit", func(t *testing.T) {
		const short = "suse-ai-milvus-milvus-system"
		if got := capReleaseName(short); got != short {
			t.Errorf("expected unchanged %q, got %q", short, got)
		}
	})

	// The 56-byte name from the reported NVIDIA install must be capped.
	t.Run("caps over-long name to 53 bytes", func(t *testing.T) {
		const long = "suse-ai-nvidia-blueprint-rag-nvidia-blueprint-rag-system" // 56 bytes
		assertValidCappedName(t, capReleaseName(long))
	})

	// A name exactly at the boundary is left intact.
	t.Run("exactly 53 bytes is unchanged", func(t *testing.T) {
		name := repeat('a', helmReleaseNameMax)
		if got := capReleaseName(name); got != name {
			t.Errorf("expected unchanged 53-byte name, got %q", got)
		}
	})

	// Distinct over-long inputs sharing a long prefix must not collide.
	t.Run("distinct long inputs do not collide", func(t *testing.T) {
		a := "suse-ai-nvidia-blueprint-rag-deployment-one-extra-namespace"
		b := "suse-ai-nvidia-blueprint-rag-deployment-two-extra-namespace"
		if capReleaseName(a) == capReleaseName(b) {
			t.Errorf("expected distinct outputs for distinct inputs, both -> %q", capReleaseName(a))
		}
	})

	// Pathological inputs must still yield a valid DNS-1123 label (no leading/
	// trailing '-'), not just a short-enough string.
	t.Run("pathological inputs stay valid", func(t *testing.T) {
		cases := []string{
			repeat('-', 54),                              // all dashes -> head empties out
			"-" + repeat('a', 60),                        // leading dash preserved by old impl
			repeat('a', 200),                             // very long
			"suse-ai-" + repeat('-', 60),                 // valid prefix, dash tail at the cut
			repeat('a', 47) + "------------------------", // content then dash run at the cut
		}
		for _, in := range cases {
			assertValidCappedName(t, capReleaseName(in))
		}
	})
}

// assertValidCappedName checks the invariants every capped name must satisfy.
func assertValidCappedName(t *testing.T, got string) {
	t.Helper()
	if len(got) > helmReleaseNameMax {
		t.Errorf("expected <= %d bytes, got %d (%q)", helmReleaseNameMax, len(got), got)
	}
	if got == "" {
		t.Errorf("expected non-empty result")
	}
	if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
		t.Errorf("result %q must not start or end with '-'", got)
	}
}

func repeat(b byte, n int) string {
	return strings.Repeat(string(b), n)
}
