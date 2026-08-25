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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUnauthorized indicates the Rancher Steve catalog API rejected the token
// (HTTP 401/403) — a credential problem, distinct from the endpoint being
// unreachable. Callers use errors.Is to classify a failed CheckAuth.
var ErrUnauthorized = errors.New("rancher catalog API rejected the token")

// DefaultBaseURL is the in-cluster Rancher Steve endpoint.
const DefaultBaseURL = "https://rancher.cattle-system.svc"

// maxChartDownloadBytes bounds a chart download so a misbehaving endpoint can't
// exhaust memory. Set well above the embedded-bundle ceiling so oversized charts
// are still read and rejected with a clear message by the Bundle builder.
const maxChartDownloadBytes = 64 << 20 // 64 MiB

// maxAppStatusBytes bounds the App status document read by AppUninstallInProgress.
// An App resource is small JSON; 1 MiB is generous while keeping a misbehaving
// endpoint from exhausting memory on what is otherwise an unbounded body.
const maxAppStatusBytes = 1 << 20 // 1 MiB

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

// CheckAuth verifies the client can authenticate to the Rancher Steve catalog
// API by listing ClusterRepos. It returns nil on success, ErrUnauthorized when
// the token is rejected (401/403), or a wrapped error for any other failure
// (unreachable endpoint, TLS problem, unexpected status). Used by the Settings
// API to validate a Rancher catalog token before it is saved.
func (c *CatalogClient) CheckAuth(ctx context.Context) error {
	u := fmt.Sprintf("%s/v1/catalog.cattle.io.clusterrepos?limit=1", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request Rancher catalog: %w", err)
	}
	defer resp.Body.Close()
	// Drain a bounded amount so the keep-alive connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w (%s)", ErrUnauthorized, resp.Status)
	default:
		return fmt.Errorf("rancher catalog returned %s", resp.Status)
	}
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
		// A rejected token has to carry ErrUnauthorized, the same as CheckAuth:
		// this is the path a git-backed component takes on every reconcile, and
		// the AIWorkload controller keys its RancherTokenRejected condition — the
		// one that tells the user to re-authorize — off errors.Is against it.
		// Every other status stays generic and retryable.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: rancher catalog returned %s for chart %s@%s in repo %q: %s",
				ErrUnauthorized, resp.Status, chartName, version, repoName, errorBodyExcerpt(body))
		}
		return nil, fmt.Errorf("rancher catalog returned %s for chart %s@%s in repo %q: %s",
			resp.Status, chartName, version, repoName, errorBodyExcerpt(body))
	}
	return body, nil
}

// UninstallApp triggers Rancher's catalog `action=uninstall` for a Helm release
// (a catalog.cattle.io App) on the local cluster. Rancher runs the uninstall via
// its privileged helm-operation, so this succeeds for chart resource kinds the
// operator's own ServiceAccount cannot delete. The call is asynchronous on
// Rancher's side; callers poll for the release to disappear.
//
// A 404 means the App is already gone and is reported as success so the caller's
// deletion flow is idempotent. A 401/403 carries ErrUnauthorized (same contract
// as FetchChart) so callers can surface a "re-authorize" hint.
func (c *CatalogClient) UninstallApp(ctx context.Context, namespace, releaseName string) error {
	u := fmt.Sprintf("%s/v1/catalog.cattle.io.apps/%s/%s?action=uninstall",
		c.baseURL, url.PathEscape(namespace), url.PathEscape(releaseName))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(`{"timeout":"600s"}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request Rancher uninstall %s/%s: %w", namespace, releaseName, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes*2))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return nil // already uninstalled — idempotent
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: rancher uninstall %s/%s returned %s: %s",
			ErrUnauthorized, namespace, releaseName, resp.Status, errorBodyExcerpt(body))
	default:
		return fmt.Errorf("rancher uninstall %s/%s returned %s: %s",
			namespace, releaseName, resp.Status, errorBodyExcerpt(body))
	}
}

// AppUninstallInProgress reports whether Rancher already has an uninstall in
// flight for the given App (a catalog.cattle.io App) on the local cluster. The
// deletion-path finalizer uses it to avoid re-issuing action=uninstall — and
// spawning a fresh privileged helm-operation — on every reconcile while Rancher
// is still tearing the release down.
//
// A 404 means the App is already gone and is reported as not-in-progress
// (false), so the caller falls through to its release-gone check. A 401/403
// carries ErrUnauthorized (same contract as FetchChart/UninstallApp).
func (c *CatalogClient) AppUninstallInProgress(ctx context.Context, namespace, releaseName string) (bool, error) {
	u := fmt.Sprintf("%s/v1/catalog.cattle.io.apps/%s/%s",
		c.baseURL, url.PathEscape(namespace), url.PathEscape(releaseName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("request Rancher app %s/%s: %w", namespace, releaseName, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAppStatusBytes))

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil // App already gone
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return false, fmt.Errorf("%w: rancher app %s/%s returned %s: %s",
			ErrUnauthorized, namespace, releaseName, resp.Status, errorBodyExcerpt(body))
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return false, fmt.Errorf("rancher app %s/%s returned %s: %s",
			namespace, releaseName, resp.Status, errorBodyExcerpt(body))
	}

	var app struct {
		Status struct {
			Summary struct {
				State string `json:"state"`
			} `json:"summary"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &app); err != nil {
		return false, fmt.Errorf("decode rancher app %s/%s: %w", namespace, releaseName, err)
	}
	return strings.EqualFold(app.Status.Summary.State, "uninstalling"), nil
}

// maxErrorBodyBytes bounds how much of a non-200 response body is quoted back in
// an error message. The body itself is only bounded by maxChartDownloadBytes
// (64 MiB), and an ingress or service mesh answering with a large HTML error
// page would otherwise put megabytes into a log line on every backoff tick — and
// into an AIWorkload status condition, which the CRD caps at 32768 bytes.
const maxErrorBodyBytes = 1 << 10 // 1 KiB

// errorBodyExcerpt trims a response body to something safe to interpolate into
// an error message. The full-size limit still applies to the success path, where
// the body is the chart archive.
func errorBodyExcerpt(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) <= maxErrorBodyBytes {
		return s
	}
	return s[:maxErrorBodyBytes] + "… (truncated)"
}
