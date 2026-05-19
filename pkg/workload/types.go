// Package workload owns Workload domain types and the Deployer port.
//
// Per CLAUDE.md's layering rule, this file MUST be free of api/v1alpha1
// imports — translation between aifv1.Workload and DeployRequest lives in
// conversions.go (the canonical home for CR↔domain translation).
package workload

// SourceKind enumerates the provenance of a Workload's spec.source.
// Mirrors aifv1.WorkloadSourceKind so interface.go can stay aifv1-free.
type SourceKind string

const (
	SourceKindApp        SourceKind = "App"
	SourceKindBlueprint  SourceKind = "Blueprint"
	SourceKindBundleTest SourceKind = "BundleTest"
)

// Phase is the deployer-domain phase. Mirrors a subset of
// aifv1.WorkloadPhase reachable in P4-2 (Pending/Deploying/Running/Failed).
// Degraded and RecoveryInProgress are deferred to P5-1/P5-2/P5-6.
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseDeploying Phase = "Deploying"
	PhaseRunning   Phase = "Running"
	PhaseFailed    Phase = "Failed"
)

// ComponentStatus values that may appear in ComponentRelease.Status beyond
// the verbatim helm release statuses. Helm releases use lower-case kebab
// ("deployed", "failed", "pending-install", "pending-upgrade", "uninstalling");
// the deployer adds the following marker statuses.
const (
	// ComponentStatusOrphanUninstallFailed marks an orphan that the
	// deployer attempted to uninstall but failed; phase aggregation
	// treats it as in-flight (Deploying) until cleanup succeeds.
	ComponentStatusOrphanUninstallFailed = "orphan-uninstall-failed"
)

// AppRef points at a Helm chart in an OCI repository.
// Mirrors aifv1.AppRef; translation in conversions.go.
type AppRef struct {
	Repo    string
	Chart   string
	Version string
}

// BlueprintRef points at a published Blueprint version.
type BlueprintRef struct {
	Name    string
	Version string
}

// BundleTestRef points at a Bundle being tested at a specific generation
// snapshot. The generation is recorded at test-deploy time.
type BundleTestRef struct {
	Namespace  string
	Name       string
	Generation int64
}

// SourceRef is the discriminated union over App/Blueprint/BundleTest.
// Exactly one of App/Blueprint/BundleTest is non-nil per Kind.
type SourceRef struct {
	Kind       SourceKind
	App        *AppRef
	Blueprint  *BlueprintRef
	BundleTest *BundleTestRef
}

// DeployRequest is the input to Deployer.Deploy. Carries everything the
// deployer needs from a Workload CR; framework-agnostic.
type DeployRequest struct {
	// Namespace is the workload's metadata.namespace — also the install
	// target namespace.
	Namespace string

	// ID is the workload's metadata.name (the workloadID used for release
	// naming as `{ID}-{componentName}`).
	ID string

	// SpecName is the user-supplied spec.name field. Used as the synthesized
	// componentName when source.Kind == App.
	SpecName string

	// Replicas mirrors workload.spec.replicas; defaults to 1 when the CR
	// pointer is nil (defaulting happens in conversions.go).
	Replicas int32

	// Source is the provenance discriminator + typed ref.
	Source SourceRef

	// Overrides is the per-component valueOverrides map keyed by
	// componentName; values are YAML strings (parsed by the deployer
	// before merge).
	Overrides map[string]string

	// Previous is the prior status.componentReleases — drift-detection input.
	Previous []ComponentRelease
}

// ComponentRelease records the outcome of one component's helm release.
type ComponentRelease struct {
	// Name is the componentName (= SpecName for App-source, =
	// Blueprint.spec.components[i].name otherwise).
	Name string

	// ReleaseName is `{workloadID}-{Name}` after DNS-1123 sanitisation
	// and 53-char truncation (Helm release-name limit).
	ReleaseName string

	// ChartRef is the OCI ref recorded for diagnostics.
	ChartRef string

	// Status is the helm release status verbatim ("deployed", "failed",
	// "pending-install", "pending-upgrade", "uninstalling") or the deployer
	// marker ComponentStatusOrphanUninstallFailed for drift-cleanup failures.
	Status string

	// Revision is the helm revision counter.
	Revision int32
}

// DeployResult is what Deployer.Deploy returns. The reconciler translates
// this into Workload.status via conversions.ApplyDeployResult.
type DeployResult struct {
	// Components is the per-component outcome list (in source order, with
	// surviving orphans appended on cleanup failure).
	Components []ComponentRelease

	// ObservedBundleGeneration is the Bundle.metadata.generation observed
	// at deploy time when source.Kind == BundleTest. Zero otherwise.
	ObservedBundleGeneration int64

	// Phase is the aggregate phase computed from Components statuses.
	Phase Phase
}
