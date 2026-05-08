package apps

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/SUSE/aif/pkg/nvidia"
)

// NVIDIASource is the apps.Source adapter for the SUSE Registry-backed
// NVIDIA NIM catalog (pkg/nvidia.Discovery). It owns its own cache and
// translates NIMEntry → App with namespaced ID `nvidia/<chart>:<ver>`.
//
// This file is the SOLE place in pkg/apps that imports pkg/nvidia, per
// the Option B hexagonal contract: the engine package stays unaware of
// pkg/apps; translation lives at the integration boundary.
type NVIDIASource struct {
	discovery       nvidia.Discovery
	logger          *slog.Logger
	refreshInterval time.Duration

	mu     sync.RWMutex
	cache  []App
	status SourceStatus
}

// NewNVIDIASource constructs the adapter. refreshInterval is the
// initial per-Source ticker cadence; UpdateSettings can override it.
func NewNVIDIASource(d nvidia.Discovery, logger *slog.Logger, refreshInterval time.Duration) *NVIDIASource {
	return &NVIDIASource{
		discovery:       d,
		logger:          logger,
		refreshInterval: refreshInterval,
	}
}

// Name returns the namespace prefix used in this Source's App.IDs.
func (n *NVIDIASource) Name() string { return "nvidia" }

// List returns the cached App slice. Snapshot copy; safe for callers
// to mutate. Never blocks on the underlying engine.
func (n *NVIDIASource) List(_ context.Context) ([]App, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make([]App, len(n.cache))
	copy(out, n.cache)
	return out, nil
}

// Refresh forces a sync of the underlying nvidia.Discovery, then
// re-reads the entire NIM index, translates it to []App, and atomically
// swaps the cache. On failure (engine refresh OR engine index read),
// the cache is left intact and SourceStatus.LastError is updated —
// stale-but-good per P2-3 plan decision (b).
func (n *NVIDIASource) Refresh(ctx context.Context) error {
	if err := n.discovery.Refresh(ctx); err != nil {
		n.recordError(err)
		return err
	}
	entries, err := n.discovery.Index(ctx)
	if err != nil {
		n.recordError(err)
		return err
	}
	apps := translateNIMEntries(entries)
	n.mu.Lock()
	n.cache = apps
	n.status = SourceStatus{
		LastSuccessAt: time.Now(),
		LastError:     nil,
		EntryCount:    len(apps),
	}
	n.mu.Unlock()
	return nil
}

// UpdateSettings slices the catalog-wide EngineSettings, taking only
// the SUSERegistry section + RefreshInterval, translates to the
// nvidia-native EngineSettings shape, and forwards to the underlying
// Discovery. The ApplicationCollection slice is ignored here — that's
// AppCoSource's territory.
func (n *NVIDIASource) UpdateSettings(s EngineSettings) {
	interval := s.RefreshInterval
	n.mu.Lock()
	if interval > 0 {
		n.refreshInterval = interval
	}
	effectiveInterval := n.refreshInterval
	n.mu.Unlock()

	n.discovery.UpdateSettings(nvidia.EngineSettings{
		RegistryEndpoint: s.SUSERegistry.Endpoint,
		Username:         s.SUSERegistry.Username,
		Token:            s.SUSERegistry.Token,
		RefreshInterval:  effectiveInterval,
	})
}

// Status returns a snapshot of the per-Source health state. Used by
// Catalog (stale-but-good logic) and by diagnostics endpoints.
func (n *NVIDIASource) Status() SourceStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.status
}

// Start implements Lifecycle: spawns the per-Source ticker goroutine.
// Refreshes immediately on launch, then on every refreshInterval tick;
// exits when ctx is canceled. Refresh errors are deliberately not
// propagated here — the per-Source ticker is best-effort, and
// SourceStatus already records the LastError for diagnostics.
func (n *NVIDIASource) Start(ctx context.Context) {
	go func() {
		_ = n.Refresh(ctx)
		n.mu.RLock()
		interval := n.refreshInterval
		n.mu.RUnlock()
		if interval <= 0 {
			interval = 10 * time.Minute
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = n.Refresh(ctx)
			}
		}
	}()
}

// recordError updates SourceStatus.LastError without touching the
// cache (stale-but-good).
func (n *NVIDIASource) recordError(err error) {
	n.mu.Lock()
	n.status.LastError = err
	n.mu.Unlock()
}

// translateNIMEntries converts the engine-native NIMEntry slice into
// the canonical []App. ID is namespaced as `nvidia/<chart>:<version>`;
// Type (LLM/VLM) becomes a single-element Categories slice.
func translateNIMEntries(entries []nvidia.NIMEntry) []App {
	out := make([]App, 0, len(entries))
	for _, e := range entries {
		out = append(out, App{
			ID:          "nvidia/" + e.ID,
			Name:        e.Chart,
			DisplayName: e.DisplayName,
			Publisher:   "NVIDIA",
			Version:     e.Version,
			Source:      "nvidia",
			AssetType:   "chart",
			Categories:  []string{string(e.Type)},
			ChartRef:    parseNVIDIAChartRef(e),
		})
	}
	return out
}

// parseNVIDIAChartRef splits the engine's combined OCI reference
// ("oci://<host>/<dir>/<chart>:<version>") into the {repo, chart,
// version} structure App expects. Falls back to leaving Repo as the
// full string if the suffix doesn't match (defensive — should not
// happen given how pkg/nvidia composes the ref).
func parseNVIDIAChartRef(e nvidia.NIMEntry) ChartRef {
	suffix := "/" + e.Chart + ":" + e.Version
	repo := strings.TrimSuffix(e.ChartRef, suffix)
	return ChartRef{Repo: repo, Chart: e.Chart, Version: e.Version}
}
