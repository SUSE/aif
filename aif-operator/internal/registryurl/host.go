// Package registryurl derives registry hostnames from chart-repo URLs.
package registryurl

import "strings"

// Host extracts the registry host from a chart-repo URL, e.g.
// "oci://registry.example.com/charts" or "https://helm.example.com/x" ->
// "registry.example.com" / "helm.example.com". A bare host (no scheme) is
// returned unchanged, so an OCI/HTTP(S) chart-repo override doubles as a valid
// image-pull-secret host.
//
// net/url.Parse is intentionally avoided: it puts the host in Path (leaving
// Host empty) for scheme-less inputs like "registry.example.com/charts", which
// would break the bare-host case this helper must support.
func Host(repoURL string) string {
	host := repoURL
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	return host
}
