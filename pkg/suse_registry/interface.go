package suse_registry

import "context"

// Provider enumerates SUSE-published charts under ai/charts/ (excluding
// nvidia/) and exposes a per-version Get for the REST handler. 4 methods
// (within ISP target).
type Provider interface {
	// List returns the cached chart catalog sorted by ID. Cache-only;
	// never blocks on the registry. Empty until Refresh has succeeded
	// at least once.
	List(ctx context.Context) ([]SUSEChart, error)

	// Get returns a single SUSEChart by (name, version). Returns
	// ErrNotFound when absent.
	Get(ctx context.Context, name, version string) (SUSEChart, error)

	// Refresh re-walks the registry and atomically replaces the cache.
	// Returns ErrNotConfigured if UpdateSettings has not yet supplied
	// a non-empty RegistryEndpoint.
	Refresh(ctx context.Context) error

	// UpdateSettings installs credentials + endpoint and rebuilds the
	// underlying OCI walker. Synchronous; never reads K8s Secrets directly.
	UpdateSettings(s EngineSettings)
}
