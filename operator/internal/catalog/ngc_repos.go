/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

// Public team repos serve BOTH their Helm index AND their chart tarballs
// anonymously (index 200 + chart .tgz 200, valid gzip). They are created WITHOUT
// a clientSecret. A repo whose index is anonymous but whose
// chart tarballs 403 anonymously is NOT public — it is gated (see below): a
// no-auth ClusterRepo downloads the chart, gets a JSON 403 body, tries to gunzip
// it and fails with "gzip: invalid header" at install time.
var publicNGCPaths = map[string]bool{
	"/nvidia/doca": true,
}

// Gated team repos require NGC auth for their chart tarballs (chart .tgz 403
// anonymously). They are created WITH the ngc-helm-auth clientSecret. Attaching
// auth is safe for these paths — the index still returns 200 with an NGC key —
// so the documented "auth breaks the index" NGC side-effect does not apply here.
//
// Note: omniverse and nemo-microservices serve a public index but GATED charts,
// so they live here, not in publicNGCPaths (chart .tgz 403 anonymously; 200 with
// an entitled key). Whether a given customer's NGC key is
// entitled to a path is orthogonal to classification: a non-entitled key still
// 403s the chart even with auth, which is a per-customer entitlement gap, not a
// reason to treat the repo as anonymous.
var gatedNGCPaths = map[string]bool{
	"/nim/baidu":                 true,
	"/nim/mit":                   true,
	"/nim/nvidia":                true,
	"/nvidia/ace":                true,
	"/nvidia/cuopt":              true,
	"/nvidia/nemo-microservices": true,
	"/nvidia/omniverse":          true,
	"/nvidia/omniverse-usdcode":  true,
	"/nvidia/riva":               true,
	"/nvidia/runai":              true,
}

// excludedNGCPaths return an invalid Helm index and must never be provisioned.
// Defense-in-depth: these are not in the curated catalog today.
var excludedNGCPaths = map[string]bool{
	"/nim":                            true,
	"/nim/snowflake":                  true,
	"/eevaigoeixww/animation":         true,
	"/eevaigoeixww/conversational-ai": true,
}

// NGCPathKind is how an NGC repo path (e.g. "/nim/nvidia") is classified for
// provisioning and for catalog generation.
type NGCPathKind int

const (
	NGCPathUnknown  NGCPathKind = iota // not in any known set — fail-safe to anonymous
	NGCPathOrg                         // provisioned by existing org-level code
	NGCPathPublic                      // team repo, anonymous
	NGCPathGated                       // team repo, requires ngc-helm-auth
	NGCPathExcluded                    // invalid Helm index — never provision or list
)

func (k NGCPathKind) String() string {
	switch k {
	case NGCPathOrg:
		return "org"
	case NGCPathPublic:
		return "public"
	case NGCPathGated:
		return "gated"
	case NGCPathExcluded:
		return "excluded"
	default:
		return "unknown"
	}
}

// ClassifyNGCPath maps an NGC repo path (parsed url.Path, e.g. "/nim/nvidia") to
// its NGCPathKind. An unknown path is NGCPathUnknown: callers must fail safe by
// never attaching auth. The provisioning caller then treats it as anonymous
// public (a genuinely gated new repo simply 403s until it is classified); the
// catalog generator instead warns and skips it.
func ClassifyNGCPath(path string) NGCPathKind {
	switch {
	case orgNGCPaths[path]:
		return NGCPathOrg
	case excludedNGCPaths[path]:
		return NGCPathExcluded
	case publicNGCPaths[path]:
		return NGCPathPublic
	case gatedNGCPaths[path]:
		return NGCPathGated
	default:
		return NGCPathUnknown
	}
}

// NGCTeamRepos is the classified set of NGC team-repo URLs found in the catalog.
type NGCTeamRepos struct {
	Public []string
	Gated  []string
}

// IsNGCURL reports whether u parses, is served over https, and its host is the
// NGC registry host. The https requirement is part of security invariant S1: the
// NGC token is only ever attached to an https URL on this host, never one that
// would send basic-auth in the clear.
func IsNGCURL(u string) bool {
	parsed, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" && parsed.Host == NGCHost
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
		switch ClassifyNGCPath(path) {
		case NGCPathOrg, NGCPathExcluded:
			continue
		case NGCPathGated:
			if !gatSeen[u] {
				gatSeen[u] = true
				out.Gated = append(out.Gated, u)
			}
		default: // public OR unknown → anonymous fail-safe
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
