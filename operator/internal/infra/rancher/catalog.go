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

// Package rancher provides a small client for Rancher's Steve catalog API,
// used to download chart archives from git-backed ClusterRepos (which have no
// HTTP/OCI URL a Fleet HelmOp could pull from).
package rancher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultBaseURL is the in-cluster Rancher Steve endpoint.
const DefaultBaseURL = "https://rancher.cattle-system.svc"

// serviceAccountTokenPath is where the operator's projected SA token is mounted.
const serviceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// maxChartDownloadBytes bounds a chart download so a misbehaving endpoint can't
// exhaust memory. Set well above the embedded-bundle ceiling so oversized charts
// are still read and rejected with a clear message by the Bundle builder.
const maxChartDownloadBytes = 64 << 20 // 64 MiB

// CatalogClient fetches chart archives from Rancher's Steve catalog API.
type CatalogClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewCatalogClient builds a client for the Rancher Steve API at baseURL,
// authenticating with the given bearer token. TLS trust: when insecure is true
// certificate verification is skipped; otherwise, if caPEM is non-empty it is
// used as the sole root, else the system roots apply.
func NewCatalogClient(baseURL, token string, caPEM []byte, insecure bool) (*CatalogClient, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	switch {
	case insecure:
		tlsCfg.InsecureSkipVerify = true
	case len(caPEM) > 0:
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse Rancher CA PEM: no certificates found")
		}
		tlsCfg.RootCAs = pool
	}
	return &CatalogClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout:   60 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}, nil
}

// FetchChart downloads the chart archive for (repoName, chartName, version) via
// the Steve link=chart action on the ClusterRepo resource.
func (c *CatalogClient) FetchChart(ctx context.Context, repoName, chartName, version string) ([]byte, error) {
	u := fmt.Sprintf("%s/v1/catalog.cattle.io.clusterrepos/%s?link=chart&chartName=%s&version=%s",
		c.baseURL, url.PathEscape(repoName), url.QueryEscape(chartName), url.QueryEscape(version))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Rancher catalog: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChartDownloadBytes))
	if err != nil {
		return nil, fmt.Errorf("read chart body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rancher catalog returned %s for chart %s@%s in repo %q: %s",
			resp.Status, chartName, version, repoName, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// ServiceAccountToken reads the operator's projected ServiceAccount token.
func ServiceAccountToken() (string, error) {
	b, err := os.ReadFile(serviceAccountTokenPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
