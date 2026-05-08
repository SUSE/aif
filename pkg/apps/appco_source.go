package apps

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/SUSE/aif/pkg/source_collection"
)

// AppCoSource is the apps.Source adapter for the SUSE Application
// Collection (pkg/source_collection.Client). It owns its own cache and
// translates source_collection.CatalogApp → App with namespaced ID
// `suse/<slug>:<latestVersion>`.
//
// Unlike pkg/nvidia (whose Discovery has its own cache + Refresh), the
// upstream Application Collection client is stateless — Client.List
// hits the API on every call. So this adapter's Refresh just calls
// client.List, translates, and atomically swaps the cache. The
// adapter's List then returns from cache without hitting the upstream.
//
// This file is the SOLE place in pkg/apps that imports
// pkg/source_collection, per the Option B hexagonal contract.
type AppCoSource struct {
	client          source_collection.Client
	logger          *slog.Logger
	refreshInterval time.Duration

	mu     sync.RWMutex
	cache  []App
	status SourceStatus
}

// NewAppCoSource constructs the adapter. refreshInterval is the
// initial per-Source ticker cadence; UpdateSettings can override it.
func NewAppCoSource(c source_collection.Client, logger *slog.Logger, refreshInterval time.Duration) *AppCoSource {
	return &AppCoSource{
		client:          c,
		logger:          logger,
		refreshInterval: refreshInterval,
	}
}

// Name returns the namespace prefix used in this Source's App.IDs.
func (a *AppCoSource) Name() string { return "suse" }

// List returns the cached App slice. Snapshot copy; safe to mutate.
func (a *AppCoSource) List(_ context.Context) ([]App, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]App, len(a.cache))
	copy(out, a.cache)
	return out, nil
}

// Refresh calls the underlying Client.List, translates the result to
// []App, and atomically swaps the cache. On failure the cache is left
// intact (stale-but-good per P2-3 plan decision (b)).
func (a *AppCoSource) Refresh(ctx context.Context) error {
	upstream, err := a.client.List(ctx)
	if err != nil {
		a.recordError(err)
		return err
	}
	apps := translateCatalogApps(upstream)
	a.mu.Lock()
	a.cache = apps
	a.status = SourceStatus{
		LastSuccessAt: time.Now(),
		LastError:     nil,
		EntryCount:    len(apps),
	}
	a.mu.Unlock()
	return nil
}

// UpdateSettings slices the catalog-wide EngineSettings, taking only
// the ApplicationCollection section, translates to the
// source_collection-native EngineSettings shape, and forwards to the
// underlying Client. The SUSERegistry slice is ignored — that's
// NVIDIASource's territory. RefreshInterval is held internally for the
// per-Source ticker (source_collection.EngineSettings has no such
// field).
func (a *AppCoSource) UpdateSettings(s EngineSettings) {
	if s.RefreshInterval > 0 {
		a.mu.Lock()
		a.refreshInterval = s.RefreshInterval
		a.mu.Unlock()
	}
	a.client.UpdateSettings(source_collection.EngineSettings{
		APIURL:   s.ApplicationCollection.APIURL,
		OCIHost:  s.ApplicationCollection.OCIHost,
		Username: s.ApplicationCollection.Username,
		Token:    s.ApplicationCollection.Token,
	})
}

// Status returns a snapshot of the per-Source health state.
func (a *AppCoSource) Status() SourceStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// Start implements Lifecycle: spawns the per-Source ticker goroutine.
// Refreshes immediately on launch, then on every refreshInterval tick;
// exits when ctx is canceled. Refresh errors are deliberately not
// propagated here — SourceStatus records LastError for diagnostics.
func (a *AppCoSource) Start(ctx context.Context) {
	go func() {
		_ = a.Refresh(ctx)
		a.mu.RLock()
		interval := a.refreshInterval
		a.mu.RUnlock()
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
				_ = a.Refresh(ctx)
			}
		}
	}()
}

func (a *AppCoSource) recordError(err error) {
	a.mu.Lock()
	a.status.LastError = err
	a.mu.Unlock()
}

// translateCatalogApps converts source_collection.CatalogApp slice
// into the canonical []App. ID namespaced as `suse/<slug>:<version>`.
func translateCatalogApps(upstream []source_collection.CatalogApp) []App {
	out := make([]App, 0, len(upstream))
	for _, u := range upstream {
		out = append(out, App{
			ID:          "suse/" + u.ID + ":" + u.LatestVersion,
			Name:        u.ID,
			DisplayName: u.DisplayName,
			Description: u.Description,
			Publisher:   u.Publisher,
			Version:     u.LatestVersion,
			Source:      "suse",
			AssetType:   "chart",
			Categories:  append([]string(nil), u.Categories...),
			ChartRef:    parseAppCoChartRef(u),
		})
	}
	return out
}

// parseAppCoChartRef splits the engine's combined OCI reference
// ("<repo>/<chart>:<version>") into the {repo, chart, version}
// structure App expects. Falls back to leaving Repo as the full string
// if the suffix doesn't match (defensive — should not happen given how
// pkg/source_collection composes the ref).
func parseAppCoChartRef(u source_collection.CatalogApp) ChartRef {
	suffix := "/" + u.ID + ":" + u.LatestVersion
	repo := strings.TrimSuffix(u.ChartRef, suffix)
	return ChartRef{Repo: repo, Chart: u.ID, Version: u.LatestVersion}
}
