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
