package apps

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/SUSE/aif/pkg/source_collection"
	"golang.org/x/sync/errgroup"
)

// AppCoSource is the apps.Source adapter for the SUSE Application
// Collection (pkg/source_collection.Client). It owns its own cache and
// translates source_collection.CatalogApp → App with namespaced ID
// `suse.<slug>:<latestVersion>`.
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
	annReader       source_collection.AnnotationReader
	logger          *slog.Logger
	refreshInterval time.Duration

	mu     sync.RWMutex
	cache  []App
	status SourceStatus
}

// NewAppCoSource constructs the adapter. refreshInterval is the
// initial per-Source ticker cadence; UpdateSettings can override it.
func NewAppCoSource(c source_collection.Client, ar source_collection.AnnotationReader, logger *slog.Logger, refreshInterval time.Duration) *AppCoSource {
	return &AppCoSource{
		client:          c,
		annReader:       ar,
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
	apps := translateCatalogApps(a.logger, upstream)
	a.enrichWithAnnotations(ctx, apps)
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
// into the canonical []App. ID namespaced as `suse.<slug>:<version>`
// (single-token form so the REST surface uses a plain path-segment
// route, not a wildcard).
func translateCatalogApps(logger *slog.Logger, upstream []source_collection.CatalogApp) []App {
	out := make([]App, 0, len(upstream))
	for _, u := range upstream {
		out = append(out, App{
			ID:            "suse." + u.ID + ":" + u.LatestVersion,
			Name:          u.ID,
			DisplayName:   u.DisplayName,
			Description:   u.Description,
			Publisher:     "SUSE",
			Version:       u.LatestVersion,
			Source:        "suse",
			AssetType:     "chart",
			Categories:    append([]string(nil), u.Categories...),
			ChartRef:      parseAppCoChartRef(u),
			LogoURL:       u.LogoURL,
			ProjectURL:    u.ProjectURL,
			LastUpdatedAt: parseTimePtr(logger, u.LastUpdatedAt),
		})
	}
	return out
}

// parseAppCoChartRef splits the engine's combined OCI reference
// ("<repo>/<chart>:<chartTag>") into the {repo, chart, version}
// structure App expects. Falls back to leaving Repo as the full string
// if the suffix doesn't match (defensive — should not happen given how
// pkg/source_collection composes the ref).
//
// ChartRef.Version carries u.ChartTag (the OCI tag, e.g. "1.55.0-13.1"),
// not u.LatestVersion (the bare Chart.yaml :version): the tag is the
// only key that resolves to a chart binary in AppCo's OCI registry.
// App.Version stays bare (LatestVersion) for the UI; this split is the
// reason source_collection.CatalogApp exposes both fields.
func parseAppCoChartRef(u source_collection.CatalogApp) ChartRef {
	suffix := "/" + u.ID + ":" + u.ChartTag
	repo := strings.TrimSuffix(u.ChartRef, suffix)
	return ChartRef{Repo: repo, Chart: u.ID, Version: u.ChartTag}
}

// enrichWithAnnotations populates ReferenceBlueprint, DisplayName,
// Description, and UseCase on each app from the AnnotationReader. Bounded
// parallelism (limit 8); per-chart failures log a warn and leave the app
// at ReferenceBlueprint=false. ErrNotConfigured short-circuits the rest
// of the fan-out via context cancel and emits a single warn.
//
// LastUpdatedAt is NOT read from annotations here — the AppCo API supplies
// it directly (see translateCatalogApps). The NVIDIA path reads it from
// OCI manifest annotations because the registry has no equivalent API field.
func (a *AppCoSource) enrichWithAnnotations(ctx context.Context, apps []App) {
	if a.annReader == nil {
		return
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	var notConfiguredOnce sync.Once
	for i := range apps {
		i := i
		g.Go(func() error {
			// ChartRef.Version is the OCI tag (chartTag from the engine);
			// apps[i].Version is the bare Chart.yaml :version. OCI fetches
			// require the tag — the registry has no notion of the bare
			// version.
			ann, err := a.annReader.ChartAnnotations(gctx, apps[i].ChartRef.Repo, apps[i].ChartRef.Chart, apps[i].ChartRef.Version)
			if err != nil {
				if errors.Is(err, source_collection.ErrNotConfigured) {
					notConfiguredOnce.Do(func() {
						if a.logger != nil {
							a.logger.Warn("annotation reader not configured; reference-blueprint detection disabled this Refresh")
						}
					})
					return context.Canceled
				}
				if a.logger != nil {
					a.logger.Warn("source_collection annotations: per-chart fetch failed",
						"repo", apps[i].ChartRef.Repo, "chart", apps[i].ChartRef.Chart, "version", apps[i].ChartRef.Version, "error", err)
				}
				return nil
			}
			if ann == nil {
				return nil
			}
			apps[i].ReferenceBlueprint = ann["ai.suse.com/role"] == "reference-blueprint"
			if v, ok := ann["ai.suse.com/display-name"]; ok {
				apps[i].DisplayName = v
			}
			if v, ok := ann["ai.suse.com/description"]; ok {
				apps[i].Description = v
			}
			if v, ok := ann["ai.suse.com/use-case"]; ok {
				apps[i].UseCase = v
			}
			return nil
		})
	}
	_ = g.Wait()
}
