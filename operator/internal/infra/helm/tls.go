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

package helm

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
)

const ociSchemePrefix = registry.OCIScheme + "://"

// defaultTransportClone returns a clone of http.DefaultTransport (preserving
// proxy/env defaults) with the given in-memory TLS config applied.
func defaultTransportClone(cfg *tls.Config) *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		tr := base.Clone()
		tr.TLSClientConfig = cfg
		return tr
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: cfg}
}

// ociRegistryClient returns the registry client for an OCI pull, applying
// in-memory basic auth and/or TLS. Returns the shared c.registry (which may carry
// an ambient/previously-established login) only when neither auth nor TLS is set.
//
// When auth or TLS is set it builds a FRESH registry.Client that starts with no
// ambient login — any credentials must come from the auth argument. This is
// intentional (an explicit tls/auth CR is expected to also carry the auth it needs),
// but it means a custom-CA-only pull does not inherit a pre-existing login.
func (c *helmClient) ociRegistryClient(auth *RegistryAuth, tlsCfg *tls.Config) (*registry.Client, error) {
	if auth == nil && tlsCfg == nil {
		return c.registry, nil
	}
	opts := []registry.ClientOption{registry.ClientOptDebug(c.settings.Debug)}
	if tlsCfg != nil {
		opts = append(opts, registry.ClientOptHTTPClient(&http.Client{Transport: defaultTransportClone(tlsCfg)}))
	}
	if auth != nil {
		opts = append(opts, registry.ClientOptBasicAuth(auth.Username, auth.Password))
	}
	return registry.NewClient(opts...)
}

// loadChartHTTPSWithTLS downloads an https chart with an in-memory TLS transport
// (and optional basic auth) and loads it, bypassing file-based ChartPathOptions.
// ref must be a direct chart-archive URL: unlike the non-TLS path, this does not
// perform repo-index resolution, so spec.RepoURL/spec.Version are not honored here.
func (c *helmClient) loadChartHTTPSWithTLS(ref string, auth *RegistryAuth, tlsCfg *tls.Config) (*chart.Chart, error) {
	gOpts := []getter.Option{getter.WithURL(ref), getter.WithTransport(defaultTransportClone(tlsCfg))}
	if auth != nil {
		gOpts = append(gOpts, getter.WithBasicAuth(auth.Username, auth.Password))
	}
	g, err := getter.NewHTTPGetter(gOpts...)
	if err != nil {
		return nil, err
	}
	data, err := g.Get(ref)
	if err != nil {
		return nil, err
	}
	ch, err := loader.LoadArchive(bytes.NewReader(data.Bytes()))
	if err != nil {
		return nil, err
	}
	if err := action.CheckDependencies(ch, ch.Metadata.Dependencies); err != nil {
		return nil, fmt.Errorf("missing dependencies: %w", err)
	}
	return ch, nil
}

// chartFetcher fetches the chart for a release spec. It returns the chart along
// with the path of the local artifact the fetch wrote, or "" when the fetch
// produced the chart without leaving one — which means there is nothing to reuse.
type chartFetcher func(setRegistry func(*registry.Client), opts *action.ChartPathOptions, spec ReleaseSpec) (*chart.Chart, string, error)

// loadChart is the single point every Helm-SDK chart fetch passes through —
// install, upgrade, and the dry-run render that gates the upgrade — so it is
// where both the pull counter and the chart cache belong. Counting here counts
// the extension chart's registry traffic exactly, and caching here covers all
// three callers without any of them knowing about it.
//
// Helm-SDK, not every byte the operator pulls: repository index fetches go out
// through helm.FetchIndex, and a git-backed blueprint chart is downloaded from
// the Rancher catalog API by rancher.ChartFetcher. Neither is counted here.
// Both are in-cluster, so neither shows up as registry egress, which is what
// this counter exists to track.
func (c *helmClient) loadChart(setRegistry func(*registry.Client), opts *action.ChartPathOptions, spec ReleaseSpec) (*chart.Chart, error) {
	host, chartLabel := pullRegistry(spec), pullChart(spec)

	key, cacheable := chartCacheKey(spec)
	if cacheable {
		if ch, ok := c.cachedChart(key); ok {
			// The registry client the OCI path would have installed on the action
			// is only ever read by LocateChart, which a hit skips, so there is
			// nothing to set up here.
			chartCacheHitsTotal.WithLabelValues(host, chartLabel, spec.Version).Inc()
			return ch, nil
		}
	}

	// Counted before the attempt, not after a success: a pull that fails still
	// cost a round trip to the registry, and a failing pull retried on every
	// reconcile is precisely the pattern this metric exists to expose.
	chartPullsTotal.WithLabelValues(host, chartLabel, spec.Version).Inc()

	ch, path, err := c.fetchChart(setRegistry, opts, spec)
	if err != nil {
		return nil, err
	}
	if cacheable {
		c.cacheChart(key, path)
	}
	return ch, nil
}

func (c *helmClient) fetchChart(
	setRegistry func(*registry.Client),
	opts *action.ChartPathOptions,
	spec ReleaseSpec,
) (*chart.Chart, string, error) {
	if c.fetchChartFn != nil {
		return c.fetchChartFn(setRegistry, opts, spec)
	}
	return c.pullChart(setRegistry, opts, spec)
}

// pullChart resolves and loads the chart for spec, applying auth and TLS across
// OCI and HTTPS. setRegistry sets the registry client on the calling action.
func (c *helmClient) pullChart(
	setRegistry func(*registry.Client),
	opts *action.ChartPathOptions,
	spec ReleaseSpec,
) (*chart.Chart, string, error) {
	ref := spec.ChartRef
	if strings.HasPrefix(ref, ociSchemePrefix) {
		reg, err := c.ociRegistryClient(spec.RegistryAuth, spec.TLSConfig)
		if err != nil {
			return nil, "", err
		}
		setRegistry(reg)
		return resolveChart(opts, c.settings, ref)
	}
	// HTTPS with in-memory TLS: bypass file-based ChartPathOptions. This one
	// never touches disk, so it has no artifact to hand back.
	if spec.TLSConfig != nil {
		ch, err := c.loadChartHTTPSWithTLS(ref, spec.RegistryAuth, spec.TLSConfig)
		return ch, "", err
	}
	// HTTPS without TLS: existing path; basic auth via ChartPathOptions.
	if spec.RegistryAuth != nil {
		opts.Username = spec.RegistryAuth.Username
		opts.Password = spec.RegistryAuth.Password
	}
	return resolveChart(opts, c.settings, ref)
}
