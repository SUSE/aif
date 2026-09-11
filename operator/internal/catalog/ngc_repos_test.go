package catalog

import (
	"net/url"
	"strings"
	"testing"
)

// The bundled catalog now includes the full NVAIE-supported set, which references
// NGC team repos (both anonymous "public" and auth-gated). Two invariants must
// hold for that data:
//
//  1. Every NGC repo URL in the bundled catalog is explicitly classified — none
//     falls through to NGCPathUnknown. An unknown path silently fail-safes to
//     anonymous Public (see classifyNGCTeamRepos), which would be wrong for a
//     genuinely gated repo. When a weekly refresh introduces a new NGC path this
//     test fails, signalling it must be classified in ngc_repos.go before the
//     refresh PR is merged (mirrors the refresh-catalog workflow's guidance).
//  2. Connected-mode team-repo provisioning is now active (no longer dormant): the
//     catalog yields at least one team repo to provision.
//
// The split logic itself (public vs gated vs excluded, fail-safe) is covered by
// TestClassifyNGCTeamRepos_UnclassifiedURLLandsInPublic with synthetic items.
func TestClassifyNGCTeamRepos_BundledCatalogClassified(t *testing.T) {
	for _, it := range Bundled() {
		u := strings.TrimRight(strings.TrimSpace(it.RepositoryURL), "/")
		if !IsNGCURL(u) {
			continue
		}
		parsed, err := url.Parse(u)
		if err != nil {
			t.Errorf("bundled NGC URL %q failed to parse: %v", u, err)
			continue
		}
		if ClassifyNGCPath(parsed.Path) == NGCPathUnknown {
			t.Errorf("bundled catalog references unclassified NGC path %q; classify it in ngc_repos.go", parsed.Path)
		}
	}

	got := ClassifyNGCTeamRepos()
	if len(got.Public) == 0 && len(got.Gated) == 0 {
		t.Error("expected the bundled NVAIE catalog to yield team repos to provision, got none")
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

func TestNGCClusterRepoName(t *testing.T) {
	cases := map[string]string{
		"https://helm.ngc.nvidia.com/nvidia":                      "nvidia",
		"https://helm.ngc.nvidia.com/nvidia/blueprint/":           "nvidia-blueprints",
		"https://helm.ngc.nvidia.com/nvidia/omniverse":            "nvidia-omniverse",
		"https://helm.ngc.nvidia.com/nim/nvidia":                  "nim-nvidia",
		"https://helm.ngc.nvidia.com/some/unclassified-team-repo": "some-unclassified-team-repo",
	}
	for in, want := range cases {
		got, err := NGCClusterRepoName(in)
		if err != nil {
			t.Fatalf("NGCClusterRepoName(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("NGCClusterRepoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNGCClusterRepoNameRejectsNonNGCAndExcludedPaths(t *testing.T) {
	for _, in := range []string{
		"oci://registry.internal/nvidia",
		"https://example.com/nvidia/omniverse",
		"https://helm.ngc.nvidia.com/nim/snowflake",
	} {
		if _, err := NGCClusterRepoName(in); err == nil {
			t.Errorf("NGCClusterRepoName(%q) unexpectedly succeeded", in)
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

// The four path sets must be mutually disjoint. ClassifyNGCPath checks them in a
// fixed order (org, excluded, public, gated), so a path accidentally left in two
// sets would be classified by whichever is checked first — e.g. a gated repo also
// left in publicNGCPaths would classify as Public, get provisioned with no auth,
// and its chart .tgz would 403 (gzip: invalid header) at install. Since a weekly
// refresh hand-maintains these sets, guard the invariant directly.
func TestNGCPathSetsAreDisjoint(t *testing.T) {
	sets := []struct {
		kind NGCPathKind
		set  map[string]bool
	}{
		{NGCPathOrg, orgNGCPaths},
		{NGCPathExcluded, excludedNGCPaths},
		{NGCPathPublic, publicNGCPaths},
		{NGCPathGated, gatedNGCPaths},
	}
	owner := map[string]NGCPathKind{}
	for _, s := range sets {
		for path := range s.set {
			if other, ok := owner[path]; ok {
				t.Errorf("NGC path %q appears in both the %q and %q sets; it must be in exactly one", path, other, s.kind)
			}
			owner[path] = s.kind
		}
	}
}

func TestClassifyNGCPath(t *testing.T) {
	cases := map[string]NGCPathKind{
		"/nvidia":                         NGCPathOrg,
		"/nvidia/blueprint":               NGCPathOrg,
		"/nvidia/doca":                    NGCPathPublic,
		"/nvidia/nemo-microservices":      NGCPathGated, // public index, gated charts
		"/nvidia/omniverse":               NGCPathGated, // public index, gated charts
		"/nim/nvidia":                     NGCPathGated,
		"/nvidia/runai":                   NGCPathGated,
		"/nim":                            NGCPathExcluded,
		"/eevaigoeixww/animation":         NGCPathExcluded,
		"/eevaigoeixww/conversational-ai": NGCPathExcluded,
		"/some/brand-new-team":            NGCPathUnknown,
	}
	for path, want := range cases {
		if got := ClassifyNGCPath(path); got != want {
			t.Errorf("ClassifyNGCPath(%q) = %v, want %v", path, got, want)
		}
	}
	if NGCPathExcluded.String() != "excluded" {
		t.Errorf("String() = %q, want excluded", NGCPathExcluded.String())
	}
}
