package suse_registry

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SUSE/aif/pkg/oci"
	"golang.org/x/sync/errgroup"
)

// suseChartsPrefix is the SUSE Registry repo path under which the
// SUSE-published Helm charts live. The nvidia/ subtree is owned by
// pkg/nvidia and excluded here via the OCI walker's exclude-first-segment.
const (
	suseChartsPrefix      = "ai/charts/"
	excludedNvidiaSubtree = "nvidia"
	annotationFanOutLimit = 8
)

type providerImpl struct {
	logger *slog.Logger
	walker oci.Walker
	annR   oci.AnnotationReader

	mu       sync.RWMutex
	cache    map[string]SUSEChart // key: ID = "<chart>:<version>"
	settings EngineSettings
}

// NewProvider returns a Provider bound to the given OCI walker and
// AnnotationReader. cmd/operator/main.go shares one walker between
// pkg/nvidia and pkg/suse_registry to avoid two HTTP clients against
// the same registry.
func NewProvider(logger *slog.Logger, walker oci.Walker, annR oci.AnnotationReader) Provider {
	return newProvider(logger, walker, annR)
}

func newProvider(logger *slog.Logger, walker oci.Walker, annR oci.AnnotationReader) *providerImpl {
	return &providerImpl{
		logger: logger,
		walker: walker,
		annR:   annR,
	}
}

func (p *providerImpl) List(_ context.Context) ([]SUSEChart, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]SUSEChart, 0, len(p.cache))
	for _, e := range p.cache {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (p *providerImpl) Get(_ context.Context, name, version string) (SUSEChart, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	entry, ok := p.cache[name+":"+version]
	if !ok {
		return SUSEChart{}, ErrNotFound
	}
	return entry, nil
}

func (p *providerImpl) Refresh(ctx context.Context) error {
	p.mu.RLock()
	endpoint := p.settings.RegistryEndpoint
	p.mu.RUnlock()
	if endpoint == "" {
		return ErrNotConfigured
	}

	start := time.Now()
	coords, err := p.walker.EnumerateCharts(ctx, suseChartsPrefix, []string{excludedNvidiaSubtree})
	if err != nil {
		return translateOCIError(err)
	}

	next := make(map[string]SUSEChart, len(coords))
	for _, c := range coords {
		chart := strings.TrimPrefix(c.Repository, suseChartsPrefix)
		id := chart + ":" + c.Tag
		next[id] = SUSEChart{
			ID:          id,
			Chart:       chart,
			Version:     c.Tag,
			DisplayName: chart,
			ChartRef:    "oci://" + oci.StripScheme(endpoint) + "/" + c.Repository + ":" + c.Tag,
		}
	}

	p.enrichWithAnnotations(ctx, next)

	p.mu.Lock()
	p.cache = next
	p.mu.Unlock()

	if p.logger != nil {
		p.logger.Debug("suse_registry.Provider refresh complete",
			"entries", len(next),
			"duration", time.Since(start))
	}
	return nil
}

func (p *providerImpl) UpdateSettings(s EngineSettings) {
	p.mu.Lock()
	p.settings = s
	p.mu.Unlock()
	p.walker.UpdateSettings(oci.EngineSettings{
		Endpoint:        s.RegistryEndpoint,
		Username:        s.Username,
		Token:           s.Token,
		RefreshInterval: s.RefreshInterval,
	})
}

func (p *providerImpl) enrichWithAnnotations(ctx context.Context, entries map[string]SUSEChart) {
	if p.annR == nil || len(entries) == 0 {
		return
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}

	g, gctx := errgroup.WithContext(ctx)
	// SetLimit MUST precede g.Go; calling it after queues throw a panic.
	g.SetLimit(annotationFanOutLimit)
	var mu sync.Mutex
	for _, k := range keys {
		g.Go(func() error {
			entry := entries[k]
			repo := suseChartsPrefix + entry.Chart
			ann, err := p.annR.ChartAnnotations(gctx, repo, entry.Version)
			if err != nil {
				if p.logger != nil {
					p.logger.Warn("suse_registry: per-chart annotation fetch failed",
						"repo", repo, "tag", entry.Version, "error", err)
				}
				return nil
			}
			if ann == nil {
				return nil
			}
			if v, ok := ann["ai.suse.com/display-name"]; ok {
				entry.DisplayName = v
			}
			if v, ok := ann["ai.suse.com/description"]; ok {
				entry.Description = v
			}
			if v, ok := ann["ai.suse.com/use-case"]; ok {
				entry.UseCase = v
			}
			entry.ReferenceBlueprint = ann["ai.suse.com/role"] == "reference-blueprint"
			if v, ok := ann["org.opencontainers.image.created"]; ok {
				if t, perr := time.Parse(time.RFC3339, v); perr == nil {
					entry.LastUpdatedAt = &t
				}
			}
			mu.Lock()
			entries[k] = entry
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
}

func translateOCIError(err error) error {
	switch {
	case errors.Is(err, oci.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, oci.ErrUnreachable):
		return ErrUnreachable
	case errors.Is(err, oci.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, oci.ErrNotConfigured):
		return ErrNotConfigured
	default:
		return ErrUnexpectedResponse
	}
}
