package catalog

import (
	"testing"
)

// The bundled catalog references the two org repos (/nvidia and /nvidia/blueprint,
// both excluded from team classification) plus the gated /nvidia/runai team repo.
// So classifying it yields exactly one Gated repo and no Public repos: the
// connected-mode team-repo provisioning is active for runai. The split logic
// itself (public vs gated vs excluded, fail-safe) is covered by
// TestClassifyNGCTeamRepos_UnclassifiedURLLandsInPublic with synthetic items.
func TestClassifyNGCTeamRepos_BundledCatalogClassifiesGatedRunai(t *testing.T) {
	got := ClassifyNGCTeamRepos()

	if len(got.Public) != 0 {
		t.Errorf("expected no Public team repos from bundled catalog, got %v", got.Public)
	}
	if len(got.Gated) != 1 || got.Gated[0] != "https://helm.ngc.nvidia.com/nvidia/runai" {
		t.Errorf("expected exactly the gated runai repo, got %v", got.Gated)
	}
}

func TestIsNGCURL(t *testing.T) {
	cases := map[string]bool{
		"https://helm.ngc.nvidia.com/nvidia/omniverse": true,
		"https://helm.ngc.nvidia.com/nim/nvidia/":      true,
		"http://helm.ngc.nvidia.com/nvidia/omniverse":  false, // S1: never over plaintext http
		"oci://registry.internal/nvidia":               false,
		"oci://dp.apps.rancher.io/charts":              false,
		"not a url":                                    false,
		"":                                             false,
	}
	for in, want := range cases {
		if got := IsNGCURL(in); got != want {
			t.Errorf("IsNGCURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Fail-safe: an unclassified NGC URL (not in public/gated/excluded path sets)
// lands in Public, NEVER Gated. Attaching auth to an unknown path is the dangerous
// operation (documented NGC 403 side-effect), so the fail-safe prevents it.
func TestClassifyNGCTeamRepos_UnclassifiedURLLandsInPublic(t *testing.T) {
	synthetic := []Item{
		{RepositoryURL: "https://helm.ngc.nvidia.com/nvidia/brand-new-thing"}, // unclassified
		{RepositoryURL: "https://helm.ngc.nvidia.com/nvidia/cuopt"},           // gated
		{RepositoryURL: "https://helm.ngc.nvidia.com/nim/snowflake"},          // excluded
		{RepositoryURL: "oci://registry.internal/nvidia"},                     // not NGC
	}

	got := classifyNGCTeamRepos(synthetic)

	// The unclassified URL must land in Public (fail-safe).
	pub := toSet(got.Public)
	if !pub["https://helm.ngc.nvidia.com/nvidia/brand-new-thing"] {
		t.Errorf("unclassified NGC URL must land in Public (fail-safe), got Public=%v", got.Public)
	}

	// The gated URL must land in Gated.
	gat := toSet(got.Gated)
	if !gat["https://helm.ngc.nvidia.com/nvidia/cuopt"] {
		t.Errorf("gated URL missing from Gated, got Gated=%v", got.Gated)
	}

	// The excluded URL must not appear in either set.
	if pub["https://helm.ngc.nvidia.com/nim/snowflake"] || gat["https://helm.ngc.nvidia.com/nim/snowflake"] {
		t.Errorf("excluded URL must not be provisioned")
	}

	// The non-NGC URL must not appear.
	if pub["oci://registry.internal/nvidia"] || gat["oci://registry.internal/nvidia"] {
		t.Errorf("non-NGC URL must not be classified")
	}

	// The unclassified URL must NEVER land in Gated (binding fail-safe constraint).
	if gat["https://helm.ngc.nvidia.com/nvidia/brand-new-thing"] {
		t.Errorf("FAIL-SAFE VIOLATED: unclassified URL landed in Gated (dangerous)")
	}
}
