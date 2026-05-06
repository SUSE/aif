package nvidia

import (
	"context"
	"errors"
)

// Discovery enumerates SUSE-mirrored NIM models from the SUSE Registry chart
// index at oci://registry.suse.com/ai/charts/nvidia/. The actual fetch happens
// out-of-band on a refresh interval; Index/Get read the cached result.
//
// 4 methods (the ISP target, not over). Spec: ARCHITECTURE.md §6.2.
type Discovery interface {
	// Index returns the cached NIM catalog sorted by ID. Returns whatever
	// was loaded by the last successful Refresh; never blocks on the
	// upstream registry.
	Index(ctx context.Context) ([]NIMEntry, error)

	// Get returns a single cached NIMEntry by its canonical ID
	// ("<chart>:<version>"). Returns ErrNIMNotFound if the ID is absent.
	// Used by the per-model REST handler GET /api/v1/nvidia/nims/{id} (P2-6).
	Get(ctx context.Context, id string) (NIMEntry, error)

	// Refresh forces an immediate sync against the SUSE Registry chart index.
	// Used by Settings save (P5-4) and the manual refresh button (P2-3).
	Refresh(ctx context.Context) error

	// UpdateSettings receives a credential/endpoint push from
	// SettingsReconciler. Synchronous; never reads Secrets directly.
	UpdateSettings(s EngineSettings)
}

// Deployer produces the Helm values block for a NIM deployment. It is
// deliberately a separate port from Discovery — they have nothing in common.
// Spec: ARCHITECTURE.md §6.2 + §4.4 NIM Sizing Formulas (lands in P4-4).
type Deployer interface {
	// GenerateValues produces the values map handed to the Helm engine.
	GenerateValues(ctx context.Context, req GenerateRequest) (map[string]any, error)
}

// ErrNotImplemented is returned by stub method bodies until plan tasks P2-1
// (Discovery) and P4-4 (Deployer) implement them.
var ErrNotImplemented = errors.New("nvidia: method not implemented yet")
