package source_collection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

type apiClient struct {
	httpClient *http.Client
	limiter    *rate.Limiter
	log        *slog.Logger

	mu       sync.RWMutex
	settings EngineSettings
	annCache map[string]annotationCacheEntry
}

// NewClient returns a Client that talks to the SUSE Application Collection HTTP API.
func NewClient(log *slog.Logger) (Client, AnnotationReader) {
	c := &apiClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		limiter:    rate.NewLimiter(rate.Every(2*time.Second), 1),
		log:        log,
		annCache:   make(map[string]annotationCacheEntry),
	}
	return c, c
}

func (c *apiClient) UpdateSettings(s EngineSettings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings = s
}

func (c *apiClient) effectiveSettings() (EngineSettings, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.settings.APIURL == "" {
		return EngineSettings{}, ErrNotConfigured
	}
	return c.settings, nil
}

const appCoMaxPageSize = 100
const appCoDetailConcurrency = 8

func (c *apiClient) List(ctx context.Context) ([]CatalogApp, error) {
	settings, err := c.effectiveSettings()
	if err != nil {
		return nil, err
	}

	listItems, err := c.listAllPages(ctx, settings)
	if err != nil {
		return nil, err
	}

	apps := make([]CatalogApp, len(listItems))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(appCoDetailConcurrency)
	for i := range listItems {
		i := i
		g.Go(func() error {
			detail, derr := c.fetchAppDetail(gctx, settings, listItems[i].SlugName)
			if derr != nil {
				// Detail-fetch failures degrade gracefully: the app still
				// shows up in the catalog with empty version/categories.
				// The list flow itself stays successful so partial upstream
				// outages do not blank the UI.
				c.log.Warn("source_collection: per-app detail fetch failed; emitting app with empty version/categories",
					"slug", listItems[i].SlugName, "error", derr)
				apps[i] = c.buildCatalogApp(settings, listItems[i], apiAppDetail{})
				return nil
			}
			apps[i] = c.buildCatalogApp(settings, listItems[i], detail)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Drop non-HELM_CHART entries client-side: the upstream silently ignores
	// ?packaging_format=HELM_CHART. We keep the query param on the request
	// (cheap insurance against future upstream re-enabling it) AND filter here.
	filtered := apps[:0]
	for _, a := range apps {
		// LatestVersion is the strongest signal that the detail fetch
		// succeeded and the app is actually deployable. Items with no
		// branches at all (e.g. unpublished apps) are filtered out.
		if a.LatestVersion == "" {
			continue
		}
		filtered = append(filtered, a)
	}
	return filtered, nil
}

// listAllPages walks the page-based pagination envelope and returns the
// concatenated, de-duplicated list items.
func (c *apiClient) listAllPages(ctx context.Context, settings EngineSettings) ([]apiListItem, error) {
	seen := make(map[string]struct{})
	var items []apiListItem

	page := 1
	for {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		resp, err := c.fetchListPage(ctx, settings, page)
		if err != nil {
			return nil, err
		}

		for _, it := range resp.Items {
			if it.PackagingFormat != "" && it.PackagingFormat != "HELM_CHART" {
				continue
			}
			if _, dup := seen[it.SlugName]; dup {
				continue
			}
			seen[it.SlugName] = struct{}{}
			items = append(items, it)
		}

		// Termination: either we have all pages, or the server returned
		// fewer items than page_size (defensive — handles servers that
		// don't populate total_pages).
		if resp.TotalPages > 0 {
			if page >= resp.TotalPages {
				return items, nil
			}
		} else if len(resp.Items) < appCoMaxPageSize {
			return items, nil
		}
		page++
	}
}

var errRetryableStatus = errors.New("retryable HTTP status")

// fetchListPage performs one GET against the list endpoint for a single
// page, with one retry on transient errors.
func (c *apiClient) fetchListPage(ctx context.Context, settings EngineSettings, page int) (*apiListResponse, error) {
	u, err := url.Parse(settings.APIURL + "/v1/applications")
	if err != nil {
		return nil, fmt.Errorf("parse list URL: %w", err)
	}
	q := u.Query()
	q.Set("packaging_format", "HELM_CHART")
	q.Set("page", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(appCoMaxPageSize))
	u.RawQuery = q.Encode()

	var out apiListResponse
	if err := c.getJSON(ctx, settings, u.String(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// fetchAppDetail performs one GET against the per-app detail endpoint.
// Returns the parsed detail on success; ErrChartNotFound on 404 (caller
// degrades gracefully). One retry on transient errors.
func (c *apiClient) fetchAppDetail(ctx context.Context, settings EngineSettings, slug string) (apiAppDetail, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return apiAppDetail{}, fmt.Errorf("rate limiter: %w", err)
	}
	u := settings.APIURL + "/v1/applications/" + url.PathEscape(slug)

	var out apiAppDetail
	if err := c.getJSON(ctx, settings, u, &out); err != nil {
		return apiAppDetail{}, err
	}
	return out, nil
}

// getJSON is the shared transport: one HTTP GET, decoded into out, with
// one retry on transient (5xx / 429 / 408 / malformed-JSON) errors.
func (c *apiClient) getJSON(ctx context.Context, settings EngineSettings, urlStr string, out any) error {
	err := c.fetchAndDecode(ctx, settings, urlStr, out)
	if err == nil {
		return nil
	}
	if !isRetryable(err) {
		return err
	}
	c.log.Info("retrying after transient error", "url", urlStr, "error", err)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(1 * time.Second):
	}
	err = c.fetchAndDecode(ctx, settings, urlStr, out)
	if err != nil && errors.Is(err, errRetryableStatus) {
		return fmt.Errorf("%w", ErrUpstreamUnavailable)
	}
	return err
}

func isRetryable(err error) bool {
	return errors.Is(err, errRetryableStatus) || errors.Is(err, ErrCatalogMalformed)
}

func (c *apiClient) fetchAndDecode(ctx context.Context, settings EngineSettings, urlStr string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if settings.Username != "" || settings.Token != "" {
		req.SetBasicAuth(settings.Username, settings.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("%w: %v", ErrCatalogMalformed, err)
		}
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrChartNotFound, urlStr)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d", ErrAuthFailed, resp.StatusCode)
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: HTTP %d", errRetryableStatus, resp.StatusCode)
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: HTTP %d", ErrUpstreamUnavailable, resp.StatusCode)
	default:
		return fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}
}

// buildCatalogApp combines a list-endpoint item with its detail-endpoint
// payload into a CatalogApp. Chart ref is constructed from the documented
// convention (oci://<OCIHost>/charts/<slug>:<version>) — the helm{} field
// that used to ship in the list response is gone. Logo URL is absolutized
// against the APIURL host when relative.
func (c *apiClient) buildCatalogApp(settings EngineSettings, item apiListItem, detail apiAppDetail) CatalogApp {
	version := latestBaseline(detail.Branches)
	return CatalogApp{
		ID:            item.SlugName,
		DisplayName:   item.Name,
		Description:   item.Description,
		Categories:    categoriesFromLabels(detail.Labels),
		ChartRef:      buildChartRef(settings.OCIHost, item.SlugName, version),
		LatestVersion: version,
		Source:        "api",
		LogoURL:       absolutizeLogoURL(settings.APIURL, item.LogoURL),
		ProjectURL:    item.ProjectURL,
		LastUpdatedAt: item.LastUpdatedAt,
	}
}

// latestBaseline picks the most-recent baseline from an app's branches.
// Strategy: skip LTS branches (separately released long-term-support
// streams), then pick the lexicographically-greatest baseline string —
// the upstream branches are semver-shaped so component-by-component
// numeric comparison matches semver well enough for our display purpose.
// Returns empty string if no usable branch is found.
func latestBaseline(branches []apiBranch) string {
	var best string
	for _, b := range branches {
		if b.IsLTS {
			continue
		}
		if b.Baseline == "" {
			continue
		}
		if best == "" || compareSemverLike(b.Baseline, best) > 0 {
			best = b.Baseline
		}
	}
	// Fallback: if every branch is LTS, take the highest LTS rather than
	// returning empty (an LTS-only app should still show a version).
	if best == "" {
		for _, b := range branches {
			if b.Baseline == "" {
				continue
			}
			if best == "" || compareSemverLike(b.Baseline, best) > 0 {
				best = b.Baseline
			}
		}
	}
	return best
}

// compareSemverLike compares two semver-shaped strings (e.g. "1.2.3").
// Returns -1, 0, +1 like strings.Compare semantics. Components are
// compared numerically; non-numeric components fall back to string
// compare. Good enough for display-ordering — not a strict semver impl.
func compareSemverLike(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var ap, bp string
		if i < len(pa) {
			ap = pa[i]
		}
		if i < len(pb) {
			bp = pb[i]
		}
		ai, aerr := strconv.Atoi(ap)
		bi, berr := strconv.Atoi(bp)
		if aerr == nil && berr == nil {
			if ai < bi {
				return -1
			}
			if ai > bi {
				return 1
			}
			continue
		}
		if ap < bp {
			return -1
		}
		if ap > bp {
			return 1
		}
	}
	return 0
}

// categoriesFromLabels extracts category names from the labels array.
// Upstream encodes per-app metadata as flat strings like
// "category:observability", "license:Apache-2.0", "ecosystem:cncf".
// We pull only the category: prefixed ones (the UI category filter is
// scoped to that taxonomy).
func categoriesFromLabels(labels []string) []string {
	const prefix = "category:"
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			name := strings.TrimPrefix(l, prefix)
			if name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// buildChartRef constructs the OCI reference per the SUSE Application
// Collection convention: oci://<host>/charts/<slug>:<version>. OCIHost
// may arrive with or without a scheme; we strip http(s):// and any path
// component to get the bare host.
func buildChartRef(ociHost, slug, version string) string {
	if slug == "" || version == "" {
		return ""
	}
	host := ociHost
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		// Documented production default — keeps the catalog deployable
		// when OCIHost is not yet wired through Settings.
		host = "dp.apps.rancher.io"
	}
	return "oci://" + host + "/charts/" + slug + ":" + version
}

// absolutizeLogoURL converts upstream's relative logo paths (e.g.
// "/logos/alertmanager.png") into absolute URLs anchored at the APIURL
// host. Absolute URLs pass through unchanged. Empty stays empty.
func absolutizeLogoURL(apiURL, logoURL string) string {
	if logoURL == "" {
		return ""
	}
	if strings.HasPrefix(logoURL, "http://") || strings.HasPrefix(logoURL, "https://") {
		return logoURL
	}
	base, err := url.Parse(apiURL)
	if err != nil {
		return logoURL
	}
	rel, err := url.Parse(logoURL)
	if err != nil {
		return logoURL
	}
	return base.ResolveReference(rel).String()
}

// GetChart returns version metadata from the Application Collection API.
// The repo parameter is reserved for P5-8 (OCI fallback); currently unused.
// Annotations and Description require fetching the actual Chart.yaml from OCI,
// which is handled by the consuming code in P2-5 — this method returns only
// what the /versions API endpoint provides.
func (c *apiClient) GetChart(ctx context.Context, _, chart, version string) (*ChartMetadata, error) {
	settings, err := c.effectiveSettings()
	if err != nil {
		return nil, err
	}
	nextURL := settings.APIURL + "/v1/applications/" + chart + "/versions"

	for nextURL != "" {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		resp, err := c.fetchAndDecodeVersions(ctx, settings, nextURL)
		if err != nil {
			return nil, err
		}

		for _, entry := range resp.Items {
			if entry.Version == version {
				return &ChartMetadata{
					Name:       chart,
					Version:    entry.Version,
					AppVersion: entry.AppVersion,
				}, nil
			}
		}

		nextURL = resp.Next
	}

	return nil, fmt.Errorf("%w: version %s for chart %s", ErrVersionNotFound, version, chart)
}

func (c *apiClient) fetchAndDecodeVersions(ctx context.Context, settings EngineSettings, url string) (*apiVersionsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if settings.Username != "" || settings.Token != "" {
		req.SetBasicAuth(settings.Username, settings.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
		var result apiVersionsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCatalogMalformed, err)
		}
		return &result, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: HTTP %d", ErrAuthFailed, resp.StatusCode)
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: HTTP %d", ErrUpstreamUnavailable, resp.StatusCode)
	default:
		return nil, fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}
}
