package nvidia

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// nvidiaChartPrefix is the SUSE Registry repo path under which the
// SUSE-managed mirror process places NVIDIA Helm charts (per
// ARCHITECTURE.md §13.1 "Mirror path convention").
const nvidiaChartPrefix = "ai/charts/nvidia/"

// discoveryImpl is the production Discovery. It composes:
//   - registryClient (HTTP adapter to OCI Distribution v2)
//   - classifyChart  (pure chart-name → Type heuristic)
//   - an in-memory cache keyed by "<chart>:<version>"
//
// Lifecycle: NewDiscovery returns an impl with no settings; the cache is
// empty and Refresh returns ErrNotConfigured. SettingsReconciler calls
// UpdateSettings to install credentials + endpoint; subsequent Refresh
// calls then walk the registry catalog.
type discoveryImpl struct {
	logger     *slog.Logger
	httpClient *http.Client

	mu sync.RWMutex
	// cache is keyed by ID = "<chart>:<version>". Lifecycle invariant:
	// nil before the first successful Refresh, then replaced *atomically*
	// (never mutated incrementally) on every subsequent Refresh — see the
	// `d.cache = next` swap below. Reads (Index, Get) are safe on nil
	// (range and lookup return zero-values). Do NOT add incremental writes
	// to this field; if a use case ever needs them, replace the whole map.
	cache    map[string]NIMEntry
	settings EngineSettings
	client   *registryClient // rebuilt in UpdateSettings; nil until first call
}

// NewDiscovery returns a Discovery bound to the given logger. The HTTP
// client uses a sensible default timeout; tests in this package construct
// the impl directly with an injected httpClient (httptest.Server.Client()).
func NewDiscovery(logger *slog.Logger) Discovery {
	return &discoveryImpl{
		logger:     logger,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Index returns a snapshot of the cached NIM catalog, sorted by ID for
// deterministic ordering. Never blocks on the registry.
func (d *discoveryImpl) Index(_ context.Context) ([]NIMEntry, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	out := make([]NIMEntry, 0, len(d.cache))
	for _, e := range d.cache {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns the cached NIMEntry with the given canonical ID. The cache
// is keyed by ID natively, so this is O(1). Returns ErrNIMNotFound when
// the ID is absent (callers branch via errors.Is).
func (d *discoveryImpl) Get(_ context.Context, id string) (NIMEntry, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	entry, ok := d.cache[id]
	if !ok {
		return NIMEntry{}, ErrNIMNotFound
	}
	return entry, nil
}

// Refresh re-reads the SUSE Registry catalog and atomically replaces the
// cache. Refresh holds the cache mutex only during the swap; the HTTP
// calls run without it, so concurrent Index() calls see the previous
// cache until the new one is ready.
//
// Returns ErrNotConfigured if UpdateSettings has not yet supplied a
// non-empty RegistryEndpoint. Wraps registry HTTP failures via the
// sentinel errors in errors.go (ErrUnreachable / ErrUnauthorized /
// ErrUnexpectedResponse).
func (d *discoveryImpl) Refresh(ctx context.Context) error {
	d.mu.RLock()
	client := d.client
	endpoint := d.settings.RegistryEndpoint
	d.mu.RUnlock()

	if client == nil {
		return ErrNotConfigured
	}

	start := time.Now()
	repos, err := client.ListRepositories(ctx)
	if err != nil {
		return err
	}

	next := make(map[string]NIMEntry)
	for _, repo := range repos {
		if !strings.HasPrefix(repo, nvidiaChartPrefix) {
			continue
		}
		chart := strings.TrimPrefix(repo, nvidiaChartPrefix)
		tags, err := client.ListTags(ctx, repo)
		if err != nil {
			return err
		}
		for _, tag := range tags {
			id := chart + ":" + tag
			next[id] = NIMEntry{
				ID:          id,
				Chart:       chart,
				Version:     tag,
				DisplayName: chart,
				Type:        classifyChart(chart),
				ChartRef:    "oci://" + stripScheme(endpoint) + "/" + repo + ":" + tag,
			}
		}
	}

	d.mu.Lock()
	d.cache = next
	d.mu.Unlock()

	if d.logger != nil {
		d.logger.Debug("nvidia.Discovery refresh complete",
			"entries", len(next),
			"duration", time.Since(start))
	}
	return nil
}

// UpdateSettings installs credentials + endpoint and rebuilds the
// internal registry client. Synchronous; never reads K8s Secrets directly.
// Empty RegistryEndpoint clears the client (subsequent Refresh returns
// ErrNotConfigured).
func (d *discoveryImpl) UpdateSettings(s EngineSettings) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.settings = s
	if s.RegistryEndpoint == "" {
		d.client = nil
		return
	}
	d.client = newRegistryClient(d.httpClient, normalizeForHTTP(s.RegistryEndpoint), s.Username, s.Token, d.logger)
}

// normalizeForHTTP ensures the endpoint is a full URL the http.Client
// can dial. Bare hostnames default to https:// (the production case).
// http:// is preserved (the dev / test case).
func normalizeForHTTP(endpoint string) string {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint
	}
	return "https://" + endpoint
}

// stripScheme removes the leading http:// or https:// from an endpoint
// so it can be embedded as the host portion of an OCI reference.
func stripScheme(endpoint string) string {
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(endpoint, scheme) {
			return endpoint[len(scheme):]
		}
	}
	return endpoint
}
