package catalog

import (
	"net/url"
	"sort"
	"strings"
)

// NGCHost is the single NGC Helm registry host. Only URLs on this host are ever
// considered for team-repo provisioning or auth attachment (security invariant
// S1: never attach the NGC token to a URL off this host).
const NGCHost = "helm.ngc.nvidia.com"

// Org repos are provisioned by existing operator code (anonymous) and must be
// excluded from the dynamic team sets.
var orgNGCPaths = map[string]bool{
	"/nvidia":           true,
	"/nvidia/blueprint": true,
}

// Public team repos serve their Helm index anonymously (HTTP 302 on
// GET index.yaml, probed 2026-07-20). They are created WITHOUT a clientSecret —
// attaching an NGC key not entitled to the path would 403 (surfaced by Rancher
// as the misleading "no API version specified").
var publicNGCPaths = map[string]bool{
	"/nvidia/doca":               true,
	"/nvidia/nemo-microservices": true,
	"/nvidia/omniverse":          true,
}

// Gated team repos require NGC auth (HTTP 403 anonymously, probed 2026-07-20).
// They are created WITH the ngc-helm-auth clientSecret.
var gatedNGCPaths = map[string]bool{
	"/nim/baidu":                true,
	"/nim/mit":                  true,
	"/nim/nvidia":               true,
	"/nvidia/ace":             true,
	"/nvidia/cuopt":             true,
	"/nvidia/omniverse-usdcode": true,
	"/nvidia/riva":              true,
	"/nvidia/runai":             true,
}

// excludedNGCPaths return an invalid Helm index and must never be provisioned.
// Defense-in-depth: these are not in the curated catalog today.
var excludedNGCPaths = map[string]bool{
	"/nim":                            true,
	"/nim/snowflake":                  true,
	"/eevaigoeixww/animation":         true,
	"/eevaigoeixww/conversational-ai": true,
}

// NGCTeamRepos is the classified set of NGC team-repo URLs found in the catalog.
type NGCTeamRepos struct {
	Public []string
	Gated  []string
}

// IsNGCURL reports whether u parses and its host is the NGC registry host.
func IsNGCURL(u string) bool {
	parsed, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return false
	}
	return parsed.Host == NGCHost
}

// classifyNGCTeamRepos is the internal classification logic, exposed for testing
// the fail-safe behavior with synthetic catalog items.
func classifyNGCTeamRepos(items []Item) NGCTeamRepos {
	pubSeen := map[string]bool{}
	gatSeen := map[string]bool{}
	var out NGCTeamRepos

	for _, it := range items {
		u := strings.TrimRight(strings.TrimSpace(it.RepositoryURL), "/")
		if !IsNGCURL(u) {
			continue
		}
		parsed, err := url.Parse(u)
		if err != nil {
			continue
		}
		path := parsed.Path
		switch {
		case orgNGCPaths[path], excludedNGCPaths[path]:
			continue
		case gatedNGCPaths[path]:
			if !gatSeen[u] {
				gatSeen[u] = true
				out.Gated = append(out.Gated, u)
			}
		default: // public set OR unclassified → anonymous fail-safe
			if !pubSeen[u] {
				pubSeen[u] = true
				out.Public = append(out.Public, u)
			}
		}
	}

	sort.Strings(out.Public)
	sort.Strings(out.Gated)
	return out
}

// ClassifyNGCTeamRepos scans the embedded (bundled) catalog and returns the
// distinct NGC team-repo URLs split into Public (anonymous) and Gated
// (ngc-helm-auth), excluding org repos and the invalid-index exclusion set.
//
// Security invariant (S1): this consumes ONLY Bundled() — never a remote/admin
// catalog — so no untrusted URL can ever reach the auth-attachment path.
//
// Fail-safe: an NGC URL in the catalog that matches none of the known path sets
// is placed in Public (anonymous). Attaching auth is the operation with the
// documented dangerous side-effect, so an unknown path must never get a secret;
// a genuinely-gated new repo simply 403s anonymously until it is classified.
func ClassifyNGCTeamRepos() NGCTeamRepos {
	return classifyNGCTeamRepos(Bundled())
}
