package apps

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// catalogImpl is the production Catalog. It owns a slice of registered
// Sources (added via AddSource) and fans out to them on every public
// Catalog method. Caching lives in each Source adapter; catalogImpl
// holds no cache of its own.
type catalogImpl struct {
	logger          *slog.Logger
	refreshInterval time.Duration

	mu      sync.RWMutex
	sources []Source
}

// New returns an Aggregator (Catalog plus AddSource + Start) ready to
// be wired at bootstrap. The refreshInterval is the default tick
// cadence handed to each Source when no per-Source override is
// provided via UpdateSettings. Consumers downstream of bootstrap
// (HTTP handlers, SettingsReconciler) accept the narrower Catalog
// interface.
func New(logger *slog.Logger, refreshInterval time.Duration) Aggregator {
	return &catalogImpl{
		logger:          logger,
		refreshInterval: refreshInterval,
	}
}

// AddSource registers a Source. Called from cmd/operator/main.go at
// bootstrap. NOT part of the Catalog interface — this is a struct
// method per the registry-pattern decision (P2-3 plan, decision d).
func (c *catalogImpl) AddSource(s Source) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sources = append(c.sources, s)
}

// snapshot returns a copy of the registered Source slice so methods
// can fan out without holding the mutex during downstream calls.
func (c *catalogImpl) snapshot() []Source {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Source, len(c.sources))
	copy(out, c.sources)
	return out
}

// List fans out to every registered Source's cache (never blocks on
// upstream — Source.List is cache-only), concatenates, dedupes by
// App.ID with first-source-wins, sorts by ID, and applies the opts
// filters. A Source whose List returns an error is logged and
// skipped — the call still returns whatever the other Sources had
// (stale-but-good per P2-3 plan decision (b)).
func (c *catalogImpl) List(ctx context.Context, opts ListOpts) ([]App, error) {
	sources := c.snapshot()
	seen := make(map[string]struct{})
	var all []App

	for _, s := range sources {
		apps, err := s.List(ctx)
		if err != nil {
			if c.logger != nil {
				c.logger.Warn("apps.Catalog: source list failed; serving prior data only",
					"source", s.Name(), "error", err)
			}
			continue
		}
		for _, a := range apps {
			if _, dup := seen[a.ID]; dup {
				continue // first-source-wins dedupe
			}
			if !matchesOpts(a, opts) {
				continue
			}
			seen[a.ID] = struct{}{}
			all = append(all, a)
		}
	}

	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, nil
}

// matchesOpts returns true when the App passes all filters in opts.
// Empty filter fields match everything.
func matchesOpts(a App, opts ListOpts) bool {
	if opts.Source != "" && a.Source != opts.Source {
		return false
	}
	if opts.Category != "" {
		hit := false
		for _, c := range a.Categories {
			if c == opts.Category {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if !opts.IncludeReferenceBlueprints && a.ReferenceBlueprint {
		return false
	}
	return true
}

// Get parses the namespace prefix in id ("<source>.<chart>:<version>")
// and dispatches to the matching Source's cache. Returns
// ErrUnknownSource when the prefix doesn't match any registered Source,
// ErrAppNotFound when it matches but the Source has no entry with that
// ID. Dot is the separator (NOT slash) so the REST route is a plain
// `/api/v1/apps/{id}` path-segment match rather than a `{id...}`
// wildcard — Helm chart names and Application Collection slug_names are
// DNS-1123 (lowercase alphanumeric + dashes; no dots), so the prefix
// split is unambiguous.
func (c *catalogImpl) Get(ctx context.Context, id string) (App, error) {
	prefix, _, ok := strings.Cut(id, ".")
	if !ok {
		return App{}, fmt.Errorf("%w: %q has no '.' separator", ErrUnknownSource, id)
	}

	for _, s := range c.snapshot() {
		if s.Name() != prefix {
			continue
		}
		apps, err := s.List(ctx)
		if err != nil {
			return App{}, fmt.Errorf("source %q list failed: %w", prefix, err)
		}
		for _, a := range apps {
			if a.ID == id {
				return a, nil
			}
		}
		return App{}, fmt.Errorf("%w: %q", ErrAppNotFound, id)
	}
	return App{}, fmt.Errorf("%w: %q", ErrUnknownSource, prefix)
}

// Refresh fans out to every Source.Refresh in parallel. Partial failure
// is logged but non-fatal; only returns an error when ALL Sources fail
// (in which case the first error is returned for diagnostic purposes).
func (c *catalogImpl) Refresh(ctx context.Context) error {
	sources := c.snapshot()
	if len(sources) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errs := make([]error, len(sources))
	for i, s := range sources {
		wg.Add(1)
		go func(i int, s Source) {
			defer wg.Done()
			if err := s.Refresh(ctx); err != nil {
				errs[i] = err
				if c.logger != nil {
					c.logger.Warn("apps.Catalog: source refresh failed",
						"source", s.Name(), "error", err)
				}
			}
		}(i, s)
	}
	wg.Wait()

	failures := 0
	var firstErr error
	for _, err := range errs {
		if err != nil {
			failures++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if failures == len(sources) && firstErr != nil {
		return fmt.Errorf("apps.Catalog: all sources failed; first error: %w", firstErr)
	}
	return nil
}

// UpdateSettings synchronously fans out to every Source's
// UpdateSettings. Each Source slices off its engine-relevant section
// internally.
func (c *catalogImpl) UpdateSettings(s EngineSettings) {
	for _, src := range c.snapshot() {
		src.UpdateSettings(s)
	}
}

// Start kicks off the per-Source background ticker for every Source
// that implements Lifecycle. Sources without Lifecycle (e.g. test
// doubles) are silently skipped. The provided context governs the
// lifetime of every adapter's ticker goroutine.
func (c *catalogImpl) Start(ctx context.Context) {
	for _, s := range c.snapshot() {
		l, ok := s.(Lifecycle)
		if !ok {
			continue
		}
		l.Start(ctx)
	}
}
