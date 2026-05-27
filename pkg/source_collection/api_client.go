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
//
// Rate-limit + fan-out budget: a full refresh is dominated by the
// per-app detail fan-out (~145 calls for the current SaaS catalog,
// plus 2 list-page calls). Sustained rate is 1 req/s with burst of
// appCoDetailConcurrency (8) so the worker pool actually parallelizes
// — burst < workers would make the workers idle on the limiter and
// the "concurrency" would be a lie. At 1 req/s sustained the burst
// drains in ~8 s and the remaining ~140 calls run serially, putting
// a full refresh at ~2.5 minutes — well under the default 10-minute
// refresh interval. The earlier setting (0.5 req/s, burst 1) ran ~5
// minutes per refresh, which was uncomfortably close to overlapping
// the next tick.
func NewClient(log *slog.Logger) (Client, AnnotationReader) {
	c := &apiClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		limiter:    rate.NewLimiter(rate.Every(time.Second), appCoDetailConcurrency),
		log:        log.With("component", "source_collection"),
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

// appCoSaaSAPIHost / appCoSaaSLogoHost: the public SUSE Application
// Collection splits API and logo serving across two hostnames. Air-gap
// mirrors don't necessarily follow the same split, so the logo-host
// rewrite below is gated on this exact match.
const (
	appCoSaaSAPIHost  = "api.apps.rancher.io"
	appCoSaaSLogoHost = "apps.rancher.io"
)

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
		g.Go(func() error {
			slug := listItems[i].SlugName
			detail, derr := c.fetchAppDetail(gctx, settings, slug)
			if abortErr, dropped := classifyPerAppErr(gctx, derr); abortErr != nil {
				return abortErr
			} else if dropped {
				c.log.Warn("per-app detail fetch failed; app excluded from catalog",
					"slug", slug, "error", derr)
				return nil
			}
			version, chartTag, verr := c.fetchLatestChartArtifact(gctx, settings, slug)
			// Note: /v1/artifacts 404 and empty-items both result in
			// dropping the app, but they take different paths.
			// Empty-items returns ("", "", nil) and is aggregated by the
			// post-Wait droppedSlugs warning; a 404 lands here as
			// ErrChartNotFound with only this per-slug Warn. An upstream
			// that 404s every slug would blank the catalog without an
			// aggregate signal.
			if abortErr, dropped := classifyPerAppErr(gctx, verr); abortErr != nil {
				return abortErr
			} else if dropped {
				// Same degradation policy as the detail fetch: log per-slug,
				// leave apps[i] zero-valued, let the post-Wait filter drop it.
				c.log.Warn("per-app artifact fetch failed; app excluded from catalog",
					"slug", slug, "error", verr)
				return nil
			}
			apps[i] = c.buildCatalogApp(settings, listItems[i], detail, version, chartTag)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Two reasons an app can be missing here:
	//   1. fetch failed — apps[i] is zero-valued, ID == "" (logged per
	//      slug above; skip silently to avoid double-counting).
	//   2. fetched OK but /v1/artifacts returned no chart — ID populated,
	//      ChartTag empty (rolled up into the aggregate warn below so
	//      operators can spot systemic catalog gaps). ChartTag is the
	//      load-bearing check: without it ChartRef can't address a chart
	//      in the OCI registry, so the app isn't installable.
	filtered := apps[:0]
	var droppedSlugs []string
	for _, a := range apps {
		if a.ID == "" {
			continue
		}
		if a.ChartTag == "" {
			droppedSlugs = append(droppedSlugs, a.ID)
			continue
		}
		filtered = append(filtered, a)
	}
	if len(droppedSlugs) > 0 {
		c.log.Warn("dropped apps with no published chart",
			"dropped_count", len(droppedSlugs),
			"kept_count", len(filtered),
			"sample_dropped", firstN(droppedSlugs, 10))
	}
	return filtered, nil
}

// firstN returns up to n elements from s for log readability.
func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// limiterWait wraps c.limiter.Wait with cancellation-aware error
// classification. rate.Wait returns two error shapes that callers care
// about distinguishing:
//
//	(a) ctx.Err() when the context fires while waiting (already
//	    context.Canceled or context.DeadlineExceeded — propagate as-is).
//	(b) "rate: Wait(n=1) would exceed context deadline" — the limiter's
//	    preflight check, returned when the wait time would miss the
//	    deadline even before ctx fires. This is custom-formatted and
//	    does NOT wrap context.DeadlineExceeded by default.
//
// Both cases mean "this op can't complete in the time available". We
// normalize to a context-error-wrapping value so the rest of the error
// machinery (errors.Is in the fan-out goroutine, in callers) works
// uniformly.
func (c *apiClient) limiterWait(ctx context.Context) error {
	err := c.limiter.Wait(ctx)
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return fmt.Errorf("rate limiter would exceed deadline: %w", context.DeadlineExceeded)
}

// classifyPerAppErr decides how an error from a per-app HTTP call should
// affect the fan-out. Returns (abortErr, dropped):
//
//   - abortErr != nil: ctx is firing (mid-request cancel, deadline, or
//     limiter preflight rewrap) and the whole flow must abort. Caller
//     should return abortErr from the goroutine — that cancels siblings
//     via errgroup.
//   - dropped == true: non-cancellation failure; the goroutine should
//     log with slug context and return nil so the rest of the catalog
//     can complete. apps[i] stays zero-valued and the post-Wait filter
//     drops it.
//   - Both false: err is nil.
//
// We check gctx.Err() first (cleaner identity) but fall back to err's
// chain because fetchAndDecode rewraps mid-request HTTP errors as
// ErrUpstreamUnavailable with %v, which would lose the context.Canceled
// identity if we only inspected err.
func classifyPerAppErr(gctx context.Context, err error) (abortErr error, dropped bool) {
	if err == nil {
		return nil, false
	}
	if gctx.Err() != nil {
		return gctx.Err(), false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err, false
	}
	return nil, true
}

// listAllPages walks the page-based pagination envelope and returns the
// concatenated, de-duplicated list items.
func (c *apiClient) listAllPages(ctx context.Context, settings EngineSettings) ([]apiListItem, error) {
	seen := make(map[string]struct{})
	var items []apiListItem

	page := 1
	for {
		if err := c.limiterWait(ctx); err != nil {
			return nil, err
		}

		resp, err := c.fetchListPage(ctx, settings, page)
		if err != nil {
			// Same masking risk as the fan-out: a mid-request ctx
			// cancellation gets rewrapped as ErrUpstreamUnavailable by
			// fetchAndDecode. Prefer ctx.Err() so callers can
			// errors.Is(err, context.Canceled / DeadlineExceeded).
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
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

		c.log.Info("fetched list page",
			"page_number", page, "items_in_page", len(resp.Items),
			"total_pages", resp.TotalPages, "total_size", resp.TotalSize,
			"accumulated", len(items))

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
	q.Set("packaging_formats", "HELM_CHART")
	q.Set("page_number", strconv.Itoa(page))
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
	if err := c.limiterWait(ctx); err != nil {
		return apiAppDetail{}, err
	}
	u := settings.APIURL + "/v1/applications/" + url.PathEscape(slug)

	var out apiAppDetail
	if err := c.getJSON(ctx, settings, u, &out); err != nil {
		return apiAppDetail{}, err
	}
	return out, nil
}

// fetchLatestChartArtifact returns (version, chartTag) for the most
// recently registered HELM_CHART artifact of a component:
//   - version is the chart's Chart.yaml :version (e.g. "1.55.0") — the
//     display surface.
//   - chartTag is the OCI registry tag (e.g. "1.55.0-13.1", or just
//     "1.55.0" when upstream omits a revision) — the only key that
//     resolves to a chart binary, encoded into ChartRef's ":<tag>".
//
// The /v1/artifacts endpoint sorts results by registered_at desc
// across all branches of the component, so page 1 / size 1 is the
// newest chart published for the app.
//
// Trade-off: this is *not* branch-aware. If an LTS branch ships a
// back-patch chart after a newer branch's release, that back-patch
// will surface as "latest" because it was registered most recently.
// Observed to be a non-issue across the current SUSE catalog
// (alertmanager, postgresql, redis, ollama tested 2026-05-26); the
// components-walk alternative is preserved on branch
// backup/components-walk-version-resolution if branch awareness is
// later required.
//
// 404 maps to ErrChartNotFound; empty items array returns
// ("", "", nil) (caller drops the app via the post-Wait filter as
// "no published chart"). The artifact's application_version is
// intentionally discarded — per ARCHITECTURE.md §4.3 the catalog
// surfaces chart version, not app version.
func (c *apiClient) fetchLatestChartArtifact(ctx context.Context, settings EngineSettings, slug string) (string, string, error) {
	if err := c.limiterWait(ctx); err != nil {
		return "", "", err
	}
	u, err := url.Parse(settings.APIURL + "/v1/artifacts")
	if err != nil {
		return "", "", fmt.Errorf("parse artifacts URL: %w", err)
	}
	q := u.Query()
	q.Set("component_slug_name", slug)
	q.Set("packaging_formats", "HELM_CHART")
	q.Set("page_size", "1")
	u.RawQuery = q.Encode()

	var page apiArtifactsPage
	if err := c.getJSON(ctx, settings, u.String(), &page); err != nil {
		return "", "", err
	}
	if len(page.Items) == 0 {
		return "", "", nil
	}
	a := page.Items[0]
	tag := a.Version
	if a.Revision != "" {
		tag = a.Version + "-" + a.Revision
	}
	return a.Version, tag, nil
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

// buildCatalogApp combines a list-endpoint item, its detail-endpoint
// payload (for labels → Categories), and the (version, chartTag) pair
// resolved from /v1/artifacts into a CatalogApp. Chart ref follows
// oci://<OCIHost>/charts/<slug>:<chartTag> (the OCI tag, not the bare
// version — that's how AppCo's registry indexes its charts); logo URL
// is absolutized against the APIURL host when relative. An empty
// chartTag leaves ChartRef empty and trips the "no published chart"
// filter in List.
func (c *apiClient) buildCatalogApp(settings EngineSettings, item apiListItem, detail apiAppDetail, version, chartTag string) CatalogApp {
	return CatalogApp{
		ID:            item.SlugName,
		DisplayName:   item.Name,
		Description:   item.Description,
		Categories:    categoriesFromLabels(detail.Labels),
		ChartRef:      buildChartRef(settings.OCIHost, item.SlugName, chartTag),
		LatestVersion: version,
		ChartTag:      chartTag,
		Source:        "api",
		LogoURL:       absolutizeLogoURL(settings.APIURL, item.LogoURL),
		ProjectURL:    item.ProjectURL,
		LastUpdatedAt: item.LastUpdatedAt,
	}
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
//
// Returns "" when slug, version, or host is empty. In particular, an
// empty host is treated as a misconfiguration (Settings hasn't wired
// OCIHost through) — we deliberately do NOT substitute a default like
// dp.apps.rancher.io: in an air-gapped cluster that would silently
// emit a chart ref pointing at the public SaaS, exactly the egress an
// air-gapped cluster shouldn't make. Empty ChartRef downstream
// surfaces as an obvious install failure rather than a confusing
// connection timeout to an unreachable host.
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
		return ""
	}
	return "oci://" + host + "/charts/" + slug + ":" + version
}

// absolutizeLogoURL converts upstream's relative logo paths (e.g.
// "/logos/alertmanager.png") into absolute URLs. The public SUSE
// Application Collection serves logos from a marketplace host
// (apps.rancher.io) that is distinct from its API host
// (api.apps.rancher.io); for that exact pair we rewrite the host.
// All other hosts — including air-gap mirrors that happen to share an
// "api." prefix — resolve against the raw apiURL host. Absolute URLs
// pass through unchanged. Empty stays empty.
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
	if base.Host == appCoSaaSAPIHost {
		base.Host = appCoSaaSLogoHost
	}
	rel, err := url.Parse(logoURL)
	if err != nil {
		return logoURL
	}
	return base.ResolveReference(rel).String()
}

// GetChart resolves an OCI chart tag (as stored in CatalogApp.ChartTag
// and embedded in CatalogApp.ChartRef, e.g. "1.55.0-13.1") back to its
// metadata. The repo parameter is reserved for the future OCI-fallback
// path and is currently unused. Annotations and Description require
// fetching Chart.yaml from OCI (handled by AnnotationReader); this
// method returns Name/Version/AppVersion only.
//
// Implementation walks page 1 of /v1/artifacts filtered to HELM_CHART;
// recently published charts resolve in a single call. A caller asking
// about an old chart that has rolled off page 1 will get
// ErrVersionNotFound — acceptable because every catalog consumer
// records the chart tag it was given and never asks about charts that
// fell out of the most-recent slice.
//
// ChartMetadata.Version is the chart's Chart.yaml :version (bare,
// e.g. "1.55.0"), matching the same field on CatalogApp — not the
// OCI tag that came in via the version parameter. AppVersion is the
// artifact's application_version.
func (c *apiClient) GetChart(ctx context.Context, _, chart, version string) (*ChartMetadata, error) {
	settings, err := c.effectiveSettings()
	if err != nil {
		return nil, err
	}
	if err := c.limiterWait(ctx); err != nil {
		return nil, err
	}
	u, err := url.Parse(settings.APIURL + "/v1/artifacts")
	if err != nil {
		return nil, fmt.Errorf("parse artifacts URL: %w", err)
	}
	q := u.Query()
	q.Set("component_slug_name", chart)
	q.Set("packaging_formats", "HELM_CHART")
	q.Set("page_size", strconv.Itoa(appCoMaxPageSize))
	u.RawQuery = q.Encode()

	var page apiArtifactsPage
	if err := c.getJSON(ctx, settings, u.String(), &page); err != nil {
		if errors.Is(err, ErrChartNotFound) {
			return nil, fmt.Errorf("%w: chart %s", ErrChartNotFound, chart)
		}
		return nil, err
	}
	for _, a := range page.Items {
		tag := a.Version
		if a.Revision != "" {
			tag = a.Version + "-" + a.Revision
		}
		if tag == version {
			return &ChartMetadata{
				Name:       chart,
				Version:    a.Version,
				AppVersion: a.ApplicationVersion,
			}, nil
		}
	}
	return nil, fmt.Errorf("%w: version %s for chart %s", ErrVersionNotFound, version, chart)
}
