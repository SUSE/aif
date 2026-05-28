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

	// InstallFromRepoURL installs a chart resolved by name from an HTTP chart
	// repository URL (e.g. a ClusterRepo service endpoint or a raw GitHub URL).
	// Idempotent: if the release exists, performs an upgrade.
	InstallFromRepoURL(ctx context.Context, req InstallFromRepoURLRequest) (ReleaseStatus, error)

	// UpdateSettings is called by SettingsReconciler.applySettingsToEngines to push
	// the latest registry endpoints + image-rewrite rules. Engine holds no Settings
	// reference; the reconciler pushes scalars on every reconcile (per §4.5 defaults
	// policy + §8.2.1 settings propagation pattern).
	UpdateSettings(s EngineSettings)
}

// Overrides carries §6.6 layers 2-4 — the user-author and NIM-generated
// inputs to the merge. The engine internalises layer 1 (chart defaults
// from loader.Load) and layers 5-6 (image rewrite + imagePullSecrets);
// callers MUST NOT pre-merge.
//
// Each field is map[string]any (parsed YAML). Nil maps are treated as
// empty and are safe.
type Overrides struct {
	// Blueprint is layer 2: Blueprint.spec.valueOverrides[componentName]
	// parsed from YAML. Nil for App sources.
	Blueprint map[string]any
	// Workload is layer 3: Workload.spec.valueOverrides[componentName]
	// parsed from YAML. Nil when the user supplied no overrides.
	Workload map[string]any
	// NIMGenerated is layer 4: nvidia.Deployer.GenerateValues output.
	// Nil for non-NIM components.
	NIMGenerated map[string]any
}

// InstallRequest is the consumer-facing input to InstallChartFromRepo.
// The engine merges Overrides with chart defaults, applies image rewrites
// from EngineSettings.ImageRewrite.Rules, and appends imagePullSecrets
// — callers do not need to pre-merge.
type InstallRequest struct {
	Namespace   string
	ReleaseName string
	ChartRef    string        // OCI ref, e.g. "oci://registry.suse.com/ai/charts/nim-llm:1.2.0"
	Overrides   Overrides
	Wait        bool          // block until release reaches deployed
	Timeout     time.Duration // default 5min

	// RequireImageRepository: when true, the engine validates that the
	// post-merge values contain a non-empty `image.repository` and returns
	// ErrMissingImageRepository otherwise. AI-workload deployers (pkg/workload)
	// set this; non-image charts like UIPlugin extensions leave it false.
	RequireImageRepository bool
}

// InstallFromRepoURLRequest is the input for InstallFromRepoURL.
// Unlike InstallRequest (OCI-based), this resolves a chart by name from an
// HTTP Helm repository URL — used for installing UI extension charts from
// ClusterRepo endpoints.
type InstallFromRepoURLRequest struct {
	Namespace   string
	ReleaseName string
	ChartName   string        // chart name as listed in the repo's index.yaml
	RepoURL     string        // HTTP URL of the Helm chart repository
	Version     string        // chart version constraint
	Wait        bool
	Timeout     time.Duration
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

// ValueRenderer renders the post-merge Helm values for one chart
// without installing. Used by pkg/workload's Fleet path: the deployer
// composes Render() per component, then hands assembled values to
// fleet.FleetBundleEngine.Apply.
//
// Single method (well within ISP target). The same concrete *engine
// type satisfies both helm.Engine and helm.ValueRenderer — wire-time
// composition in cmd/operator/main.go.
type ValueRenderer interface {
	// Render pulls the chart, loads it, applies §6.6 layers 1-5 (chart
	// defaults, Blueprint overrides, Workload overrides, NIM-generated,
	// image rewrite) and returns the merged values. Layer 6 (pull-secret
	// injection) is NOT applied here — Fleet ships the pull-secret as a
	// separate Secret resource via spec.resources[], so injecting
	// imagePullSecrets into chart values would be redundant.
	Render(ctx context.Context, repo, chart, version string, ov Overrides) (map[string]any, error)
}
