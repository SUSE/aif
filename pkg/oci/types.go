package oci

import "time"

// EngineSettings is the credential + endpoint slice supplied via
// UpdateSettings. Mirrors pkg/nvidia.EngineSettings's shape; both
// engines source the same SUSE Registry credentials but each owns
// its own value (no shared mutable state).
type EngineSettings struct {
	// Endpoint: bare hostname (https:// assumed) or full URL.
	// Empty clears the client and subsequent calls return ErrNotConfigured.
	Endpoint string

	Username string
	Token    string

	// RefreshInterval is consumed by domain packages, not by the
	// walker itself. Kept on EngineSettings so per-package pushers
	// can carry the value without an extra field.
	RefreshInterval time.Duration
}

// ChartCoordinate names one (repository, tag) pair returned by the
// catalog walk. The package does not interpret tags as semver.
type ChartCoordinate struct {
	Repository string // e.g. "ai/charts/example"
	Tag        string // e.g. "1.2.3"
}
