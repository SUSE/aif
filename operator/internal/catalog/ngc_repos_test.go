package catalog

import (
	"testing"
)

func TestClassifyNGCTeamRepos_SplitsPublicAndGated(t *testing.T) {
	got := ClassifyNGCTeamRepos()

	// Public team repos present in the bundled catalog (org repos excluded).
	wantPublic := map[string]bool{
		"https://helm.ngc.nvidia.com/nvidia/doca":               true,
		"https://helm.ngc.nvidia.com/nvidia/nemo-microservices": true,
		"https://helm.ngc.nvidia.com/nvidia/omniverse":          true,
	}
	// Gated team repos present in the bundled catalog.
	wantGated := map[string]bool{
		"https://helm.ngc.nvidia.com/nim/baidu":                true,
		"https://helm.ngc.nvidia.com/nim/mit":                  true,
		"https://helm.ngc.nvidia.com/nim/nvidia":               true,
		"https://helm.ngc.nvidia.com/nvidia/cuopt":             true,
		"https://helm.ngc.nvidia.com/nvidia/omniverse-usdcode": true,
		"https://helm.ngc.nvidia.com/nvidia/riva":              true,
		"https://helm.ngc.nvidia.com/nvidia/runai":             true,
	}

	pub := toSet(got.Public)
	gat := toSet(got.Gated)

	for u := range wantPublic {
		if !pub[u] {
			t.Errorf("expected %q in Public, got Public=%v", u, got.Public)
		}
	}
	for u := range wantGated {
		if !gat[u] {
			t.Errorf("expected %q in Gated, got Gated=%v", u, got.Gated)
		}
	}

	// Org repos must never appear in either team set.
	for _, org := range []string{
		"https://helm.ngc.nvidia.com/nvidia",
		"https://helm.ngc.nvidia.com/nvidia/blueprint",
	} {
		if pub[org] || gat[org] {
			t.Errorf("org repo %q must be excluded from team sets", org)
		}
	}

	// Excluded (invalid-index) repos must never appear.
	for _, ex := range []string{
		"https://helm.ngc.nvidia.com/nim",
		"https://helm.ngc.nvidia.com/nim/snowflake",
		"https://helm.ngc.nvidia.com/eevaigoeixww/animation",
		"https://helm.ngc.nvidia.com/eevaigoeixww/conversational-ai",
	} {
		if pub[ex] || gat[ex] {
			t.Errorf("excluded repo %q must not be provisioned", ex)
		}
	}

	// No URL appears in both sets.
	for _, u := range got.Gated {
		if pub[u] {
			t.Errorf("%q classified as both Public and Gated", u)
		}
	}
}

func TestIsNGCURL(t *testing.T) {
	cases := map[string]bool{
		"https://helm.ngc.nvidia.com/nvidia/omniverse": true,
		"https://helm.ngc.nvidia.com/nim/nvidia/":      true,
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
