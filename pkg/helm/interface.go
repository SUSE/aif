package helm

import (
	"context"
	"time"
)

// Engine manages Helm chart operations for AI Factory workloads.
// Implements the interface specified in ARCHITECTURE.md §6.4.
type Engine interface {
	// InstallChartFromRepo installs a chart pulled from an OCI repo. Idempotent: if a
	// release with the same name exists, performs an upgrade instead.
	InstallChartFromRepo(ctx context.Context, req InstallRequest) (ReleaseStatus, error)

	// Uninstall removes a release. Returns nil if the release doesn't exist.
	Uninstall(ctx context.Context, namespace, releaseName string) error

	// Status returns the current Helm release status.
	Status(ctx context.Context, namespace, releaseName string) (ReleaseStatus, error)

	// Rollback rolls back to a specific revision (per §4.4 Recovery procedure).
	Rollback(ctx context.Context, namespace, releaseName string, revision int) error

	// History returns release revision history (newest first).
	History(ctx context.Context, namespace, releaseName string) ([]RevisionInfo, error)

	// UpdateSettings is called by SettingsReconciler.applySettingsToEngines to push
	// the latest registry endpoints + image-rewrite rules. Engine holds no Settings
	// reference; the reconciler pushes scalars on every reconcile (per §4.5 defaults
	// policy + §8.2.1 settings propagation pattern).
	UpdateSettings(s EngineSettings)
}

// InstallRequest specifies parameters for installing a Helm chart from an OCI repository.
type InstallRequest struct {
	Namespace   string
	ReleaseName string
	ChartRef    string         // OCI ref, e.g. "oci://registry.suse.com/ai/charts/nim-llm:1.2.0"
	Values      map[string]any // post-merge, post-image-rewrite values per §6.6
	Wait        bool           // block until release reaches deployed
	Timeout     time.Duration  // default 5min
}

// ReleaseStatus represents the current state of a Helm release.
type ReleaseStatus struct {
	Name     string
	Revision int
	Status   string    // helm.sh/helm/v3/pkg/release status verbatim per §4.4
	Updated  time.Time
}

// RevisionInfo represents a single entry in Helm release history.
type RevisionInfo struct {
	Revision    int
	Updated     time.Time
	Status      string
	Description string
}

// EngineSettings holds configuration pushed from Settings CRD to the Helm engine.
type EngineSettings struct {
	RegistryEndpoints RegistryEndpoints  // mirrored shape of api/v1alpha1.RegistryEndpointsSpec
	ImageRewrite      ImageRewriteConfig // mirrored shape of api/v1alpha1.ImageRewriteSpec
}

// RegistryEndpoints holds registry hostname overrides for air-gap deployments.
type RegistryEndpoints struct {
	// SUSERegistry is the hostname for SUSE Registry (NIM index, vendor charts, NIM container images).
	// Default: "registry.suse.com".
	SUSERegistry string

	// ApplicationCollection is the OCI hostname for SUSE Application Collection chart pulls.
	// Default: "dp.apps.rancher.io".
	ApplicationCollection string

	// ApplicationCollectionAPI is the HTTP API URL for SUSE App Collection metadata.
	// Default: "https://api.apps.rancher.io". Set to "" to disable HTTP discovery.
	ApplicationCollectionAPI string
}

// ImageRewriteConfig holds image rewrite rules for air-gap deployments.
type ImageRewriteConfig struct {
	// Enabled is true to apply rewrite rules during Helm values merge (§6.6 layer 5).
	// Default: false.
	Enabled bool

	// Rules apply in order; first match per field wins. Empty list = no-op.
	Rules []ImageRewriteRule
}

// ImageRewriteRule defines a single image prefix rewrite rule.
type ImageRewriteRule struct {
	// Match is the prefix to match on image.repository / image.registry fields.
	Match string

	// Replace is the substitution prefix.
	Replace string
}
