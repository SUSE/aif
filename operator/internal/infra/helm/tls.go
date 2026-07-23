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
// in-memory basic auth and/or TLS. Returns c.registry when neither is set.
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

// loadChart resolves and loads the chart for spec, applying auth and TLS across
// OCI and HTTPS. setRegistry sets the registry client on the calling action.
func (c *helmClient) loadChart(setRegistry func(*registry.Client), opts *action.ChartPathOptions, spec ReleaseSpec) (*chart.Chart, error) {
	ref := spec.ChartRef
	if strings.HasPrefix(ref, ociSchemePrefix) {
		reg, err := c.ociRegistryClient(spec.RegistryAuth, spec.TLSConfig)
		if err != nil {
			return nil, err
		}
		setRegistry(reg)
		ch, _, err := resolveChart(opts, c.settings, ref)
		return ch, err
	}
	// HTTPS with in-memory TLS: bypass file-based ChartPathOptions.
	if spec.TLSConfig != nil {
		return c.loadChartHTTPSWithTLS(ref, spec.RegistryAuth, spec.TLSConfig)
	}
	// HTTPS without TLS: existing path; basic auth via ChartPathOptions.
	if spec.RegistryAuth != nil {
		opts.Username = spec.RegistryAuth.Username
		opts.Password = spec.RegistryAuth.Password
	}
	ch, _, err := resolveChart(opts, c.settings, ref)
	return ch, err
}
