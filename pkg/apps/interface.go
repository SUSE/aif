// Package apps defines the canonical Apps Catalog used by the REST
// surface and the UI. It owns the canonical App value type and two
// ports (Catalog, Source); concrete adapters in this package wrap the
// engine packages (pkg/nvidia, pkg/source_collection) and translate
// engine-native types into Apps. The engine packages MUST remain
// unaware of pkg/apps — translation lives at the integration boundary
// per the Option B hexagonal contract (P2-3 plan).
package apps

import "context"

// Catalog is the unified, source-agnostic read surface over every
// registered Source. 4 methods (within ISP target).
//
// List/Get are stale-but-good: they read the per-Source caches and do
// not block on an upstream Refresh. A Source whose last refresh failed
// continues to serve its previous successful result.
type Catalog interface {
	// List returns every cached App across registered Sources, deduped
	// by App.ID, sorted by ID, optionally filtered by ListOpts.
	List(ctx context.Context, opts ListOpts) ([]App, error)

	// Get returns a single App by namespaced ID. Parses the ID's
	// "<source>/..." prefix and dispatches to the matching Source's
	// cache. Returns ErrAppNotFound or ErrUnknownSource on miss.
	Get(ctx context.Context, id string) (App, error)

	// Refresh fans out to every Source.Refresh in parallel. Partial
	// failure is logged but non-fatal; only returns an error when all
	// Sources fail.
	Refresh(ctx context.Context) error

	// UpdateSettings receives a credential/endpoint push from
	// SettingsReconciler (P5-4) and fans out to every Source's
	// UpdateSettings, which in turn translates to the engine-native
	// settings shape.
	UpdateSettings(s EngineSettings)
}

// Source is one upstream catalog adapter (NVIDIASource, AppCoSource).
// Each adapter owns its own cache; the Catalog is a thin aggregator.
// 4 methods (within ISP target).
type Source interface {
	// Name returns the namespace prefix used in App.ID
	// ("nvidia", "suse"). Stable for the lifetime of the adapter.
	Name() string

	// List returns the adapter's cached App slice. Never blocks on the
	// underlying engine; if Refresh has never succeeded the slice is
	// empty (and SourceStatus.LastError reflects that).
	List(ctx context.Context) ([]App, error)

	// Refresh forces an immediate sync against the underlying engine
	// (which itself talks to the upstream registry/API). Replaces the
	// adapter's cache atomically on success; leaves it intact on
	// failure (stale-but-good).
	Refresh(ctx context.Context) error

	// UpdateSettings receives the catalog-wide EngineSettings, slices
	// off the engine-relevant section, translates to the engine-native
	// settings struct, and forwards to the underlying engine.
	UpdateSettings(s EngineSettings)
}

// Lifecycle is the optional per-Source background-refresh capability
// (decision e: per-Source tickers). Adapters that own a ticker
// goroutine implement Lifecycle in addition to Source; Aggregator.Start
// detects the capability via type assertion and kicks off each
// adapter's ticker. Test doubles that don't need a background loop
// implement only Source and are safely skipped.
type Lifecycle interface {
	// Start spawns the adapter's background ticker goroutine. The
	// goroutine MUST exit when ctx is canceled. Calling Start more
	// than once is implementation-defined; production adapters expect
	// a single Start per process.
	Start(ctx context.Context)
}

// Aggregator is the bootstrap-time Catalog surface used by
// cmd/operator/main.go: it is the Catalog read/control port plus
// AddSource (registry pattern — decision d) and Start (per-Source
// ticker fan-out — decision e). Returned by New(). Most consumers
// (HTTP handlers, SettingsReconciler) take the narrower Catalog port
// and don't need the bootstrap methods.
//
// This interface intentionally exceeds the ≤4-method ISP target — it
// is split by *role* (the bootstrap role) from the consumer-facing
// Catalog port, per CLAUDE.md.
type Aggregator interface {
	Catalog
	AddSource(s Source)
	Start(ctx context.Context)
}
