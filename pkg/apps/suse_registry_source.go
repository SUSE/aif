package apps

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/SUSE/aif/pkg/suse_registry"
)

// SUSERegistrySource is the apps.Source adapter wrapping
// pkg/suse_registry.Provider. It is the SOLE place in pkg/apps that
// imports pkg/suse_registry, mirroring the Option B hexagonal contract
// established by AppCoSource and NVIDIASource.
//
// Source = "suse" (the LIBRARY); Origin = "registry" (the upstream).
// IDs: suse.registry.<slug>:<version>.
type SUSERegistrySource struct {
	provider        suse_registry.Provider
	logger          *slog.Logger
	refreshInterval time.Duration

	mu     sync.RWMutex
	cache  []App
	status SourceStatus
}

func NewSUSERegistrySource(p suse_registry.Provider, logger *slog.Logger, refreshInterval time.Duration) *SUSERegistrySource {
	return &SUSERegistrySource{
		provider:        p,
		logger:          logger,
		refreshInterval: refreshInterval,
	}
}

func (s *SUSERegistrySource) Name() string { return "suse.registry" }

func (s *SUSERegistrySource) List(_ context.Context) ([]App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]App, len(s.cache))
	copy(out, s.cache)
	return out, nil
}

func (s *SUSERegistrySource) Refresh(ctx context.Context) error {
	if err := s.provider.Refresh(ctx); err != nil {
		s.recordError(err)
		return err
	}
	charts, err := s.provider.List(ctx)
	if err != nil {
		s.recordError(err)
		return err
	}
	apps := translateSUSECharts(charts)
	s.mu.Lock()
	s.cache = apps
	s.status = SourceStatus{
		LastSuccessAt: time.Now(),
		EntryCount:    len(apps),
	}
	s.mu.Unlock()
	return nil
}

func (s *SUSERegistrySource) UpdateSettings(es EngineSettings) {
	interval := es.RefreshInterval
	s.mu.Lock()
	if interval > 0 {
		s.refreshInterval = interval
	}
	effectiveInterval := s.refreshInterval
	s.mu.Unlock()

	s.provider.UpdateSettings(suse_registry.EngineSettings{
		RegistryEndpoint: es.SUSERegistry.Endpoint,
		Username:         es.SUSERegistry.Username,
		Token:            es.SUSERegistry.Token,
		RefreshInterval:  effectiveInterval,
	})
}

func (s *SUSERegistrySource) Status() SourceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *SUSERegistrySource) Start(ctx context.Context) {
	go func() {
		_ = s.Refresh(ctx)
		s.mu.RLock()
		interval := s.refreshInterval
		s.mu.RUnlock()
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
				_ = s.Refresh(ctx)
			}
		}
	}()
}

func (s *SUSERegistrySource) recordError(err error) {
	s.mu.Lock()
	s.status.LastError = err
	s.mu.Unlock()
}

// translateSUSECharts converts the engine-native suse_registry.SUSEChart
// slice into the canonical []App. ID namespaced as suse.registry.<chart>:<version>.
func translateSUSECharts(charts []suse_registry.SUSEChart) []App {
	out := make([]App, 0, len(charts))
	for _, c := range charts {
		out = append(out, App{
			ID:                 "suse.registry." + c.Chart + ":" + c.Version,
			Name:               c.Chart,
			DisplayName:        c.DisplayName,
			Description:        c.Description,
			Publisher:          "SUSE",
			Version:            c.Version,
			Source:             "suse",
			Origin:             "registry",
			AssetType:          "chart",
			ReferenceBlueprint: c.ReferenceBlueprint,
			UseCase:            c.UseCase,
			LastUpdatedAt:      c.LastUpdatedAt,
			ChartRef:           parseSUSEChartRef(c),
		})
	}
	return out
}

// parseSUSEChartRef splits the OCI reference into {Repo, Chart, Version}.
// Falls back to leaving Repo as the full string if the suffix doesn't
// match (defensive — should not happen given how the provider composes it).
func parseSUSEChartRef(c suse_registry.SUSEChart) ChartRef {
	suffix := "/" + c.Chart + ":" + c.Version
	repo := strings.TrimSuffix(c.ChartRef, suffix)
	return ChartRef{Repo: repo, Chart: c.Chart, Version: c.Version}
}
