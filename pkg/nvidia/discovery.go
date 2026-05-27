package nvidia

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SUSE/aif/pkg/oci"
)

// nvidiaChartPrefix is the SUSE Registry repo path under which the
// SUSE-managed mirror process places NVIDIA Helm charts (per
// ARCHITECTURE.md §13.1 "Mirror path convention").
const nvidiaChartPrefix = "ai/charts/nvidia/"

// discoveryImpl is the production Discovery. It composes:
//   - pkg/oci.Walker        (HTTP adapter to OCI Distribution v2, shared with siblings)
//   - pkg/oci.AnnotationReader (manifest + Chart.yaml annotation fetch with digest cache)
//   - classifyChart          (pure chart-name → Type heuristic)
//   - an in-memory cache keyed by "<chart>:<version>"
//
// Lifecycle: NewDiscovery returns an impl with no settings; the cache is
// empty and Refresh returns ErrNotConfigured. SettingsReconciler calls
// UpdateSettings to install credentials + endpoint; subsequent Refresh
// calls then walk the registry catalog via the shared pkg/oci.Walker.
type discoveryImpl struct {
	logger *slog.Logger

	mu sync.RWMutex
	// cache is keyed by ID = "<chart>:<version>". Lifecycle invariant:
	// nil before the first successful Refresh, then replaced *atomically*
	// (never mutated incrementally) on every subsequent Refresh — see the
	// `d.cache = next` swap below. Reads (Index, Get) are safe on nil
	// (range and lookup return zero-values). Do NOT add incremental writes
	// to this field; if a use case ever needs them, replace the whole map.
	cache    map[string]NIMEntry
	settings EngineSettings

	walker oci.Walker
	annR   oci.AnnotationReader
}

// NewDiscovery returns the engine bound to the given logger as both a
// Discovery and an AnnotationReader. The same backing struct satisfies
// both ports — Interface Segregation at the consumer boundary, single
// shared state internally (walker, settings, cache). Walker +
// AnnotationReader come from pkg/oci so NIMs and SUSE-published charts
// share the OCI client implementation.
func NewDiscovery(logger *slog.Logger) (Discovery, AnnotationReader) {
	w := oci.NewWalker(logger)
	impl := &discoveryImpl{
		logger: logger,
		walker: w,
		annR:   oci.NewAnnotationReader(logger, w),
	}
	return impl, impl
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

// Refresh re-reads the SUSE Registry catalog (delegated to pkg/oci.Walker)
// and atomically replaces the cache. Holds the cache mutex only during
// the swap; the HTTP calls run without it, so concurrent Index() calls
// see the previous cache until the new one is ready.
//
// Returns ErrNotConfigured if UpdateSettings has not yet supplied a
// non-empty RegistryEndpoint. Wraps registry HTTP failures via the
// sentinel errors in errors.go (ErrUnreachable / ErrUnauthorized /
// ErrChartNotFound / ErrUnexpectedResponse) via translateOCIError, so
// callers using errors.Is(err, nvidia.Err…) continue to work unchanged.
func (d *discoveryImpl) Refresh(ctx context.Context) error {
	d.mu.RLock()
	endpoint := d.settings.RegistryEndpoint
	d.mu.RUnlock()
	if endpoint == "" {
		return ErrNotConfigured
	}

	start := time.Now()
	all, err := d.walker.EnumerateCharts(ctx, nvidiaChartPrefix, nil)
	if err != nil {
		return translateOCIError(err)
	}

	next := make(map[string]NIMEntry)
	for _, c := range all {
		chart := strings.TrimPrefix(c.Repository, nvidiaChartPrefix)
		id := chart + ":" + c.Tag
		next[id] = NIMEntry{
			ID:          id,
			Chart:       chart,
			Version:     c.Tag,
			DisplayName: chart,
			Type:        classifyChart(chart),
			ChartRef:    "oci://" + oci.StripScheme(endpoint) + "/" + c.Repository + ":" + c.Tag,
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

// UpdateSettings installs credentials + endpoint and forwards them to the
// underlying pkg/oci.Walker. Synchronous; never reads K8s Secrets directly.
// Empty RegistryEndpoint clears the walker (subsequent Refresh returns
// ErrNotConfigured).
func (d *discoveryImpl) UpdateSettings(s EngineSettings) {
	d.mu.Lock()
	d.settings = s
	d.mu.Unlock()
	d.walker.UpdateSettings(oci.EngineSettings{
		Endpoint:        s.RegistryEndpoint,
		Username:        s.Username,
		Token:           s.Token,
		RefreshInterval: s.RefreshInterval,
	})
}

// translateOCIError re-wraps pkg/oci sentinels into the existing
// nvidia.* sentinels so callers using errors.Is(err, nvidia.ErrUnauthorized)
// continue to work without change. New callers should prefer the nvidia
// sentinels for any code consuming this package; pkg/oci sentinels are an
// internal implementation detail.
func translateOCIError(err error) error {
	switch {
	case errors.Is(err, oci.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, oci.ErrUnreachable):
		return ErrUnreachable
	case errors.Is(err, oci.ErrNotFound):
		return ErrChartNotFound
	case errors.Is(err, oci.ErrNotConfigured):
		return ErrNotConfigured
	default:
		return ErrUnexpectedResponse
	}
}
