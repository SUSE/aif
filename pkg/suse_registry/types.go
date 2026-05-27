package suse_registry

import "time"

// SUSEChart is one (chart, version) entry surfaced by the provider.
// Fields parallel pkg/nvidia.NIMEntry's role: enough to render an Apps
// catalog row and to construct a Helm chart pull.
type SUSEChart struct {
	// ID is "<chart>:<version>". Chart is the slug — the path segment
	// directly after ai/charts/ minus the version. For charts published
	// to sub-paths (e.g. ai/charts/example/foo:1.0.0), Chart is the
	// joined remainder (e.g. "example/foo") and the slash is preserved.
	ID      string
	Chart   string
	Version string

	// DisplayName / Description / UseCase are populated from
	// ai.suse.com/* annotations when present, with Chart.yaml fallback.
	DisplayName string
	Description string
	UseCase     string

	// ReferenceBlueprint is true when ai.suse.com/role == "reference-blueprint".
	ReferenceBlueprint bool

	// LastUpdatedAt comes from the OCI manifest's org.opencontainers.image.created
	// annotation. Nil when absent.
	LastUpdatedAt *time.Time

	// ChartRef is the canonical OCI reference, e.g.
	// "oci://registry.suse.com/ai/charts/example/foo:1.0.0".
	ChartRef string
}

// EngineSettings mirrors pkg/nvidia.EngineSettings — both engines consume
// the same SUSE Registry credentials.
type EngineSettings struct {
	RegistryEndpoint string
	Username         string
	Token            string
	RefreshInterval  time.Duration
}
