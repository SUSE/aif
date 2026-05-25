// Package workload owns Workload domain types and the Deployer port.
//
// Per CLAUDE.md's layering rule, this file MUST be free of api/v1alpha1
// imports — translation between aifv1.Workload and DeployRequest lives in
// conversions.go (the canonical home for CR↔domain translation).
package workload

import (
	"github.com/SUSE/aif/pkg/fleet"
)

// SourceKind enumerates the provenance of a Workload's spec.source.
// Mirrors aifv1.WorkloadSourceKind so interface.go can stay aifv1-free.
type SourceKind string

const (
	SourceKindApp        SourceKind = "App"
	SourceKindBlueprint  SourceKind = "Blueprint"
	SourceKindBundleTest SourceKind = "BundleTest"
)

// Phase is the workload-domain phase. Mirrors aifv1.WorkloadPhase across
// all six states (Pending/Deploying/Running/Degraded/Failed/RecoveryInProgress).
// Computed by RecomputePhase in phase.go from a PhaseInput projection;
// the controller is the single source of truth for status.phase.
type Phase string

const (
	PhasePending            Phase = "Pending"
	PhaseDeploying          Phase = "Deploying"
	PhaseRunning            Phase = "Running"
	PhaseDegraded           Phase = "Degraded" // P5-1
	PhaseFailed             Phase = "Failed"
	PhaseRecoveryInProgress Phase = "RecoveryInProgress" // P5-1
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

	// DeployStrategy mirrors aifv1.Workload.spec.deployStrategy. Empty
	// defaults to "helm" (matches CRD default). The deployer's dispatch
	// reads this to choose the Fleet path.
	DeployStrategy string

	// TargetClusters mirrors aifv1.Workload.spec.targetClusters. Used as
	// the per-cluster fan-out list for the Fleet Bundle's spec.targets.
	TargetClusters []string

	// PullSecretData is the raw .dockerconfigjson from suse-registry-creds
	// in the operator namespace. The Workload reconciler fetches it; the
	// deployer embeds it into the Fleet Bundle so downstream clusters can
	// pull images.
	PullSecretData []byte

	// Owner captures the Workload CR identity for the Fleet Bundle's
	// OwnerReferences (cascade-delete). Filled in by conversions.OwnerRefFromCR.
	Owner fleet.OwnerRef
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
// this into Workload.status via conversions.ApplyDeployResult, and
// independently computes Phase via RecomputePhase(PhaseInputFromCR(w)).
//
// Phase is intentionally NOT a field here: P5-1 moved phase ownership to
// the controller so a single function (RecomputePhase) is the source of
// truth, fed by both the deploy path and (in P5-2) the pod-readiness
// informer + ProgressDeadlineExceeded watch.
type DeployResult struct {
	// Components is the per-component outcome list (in source order, with
	// surviving orphans appended on cleanup failure).
	Components []ComponentRelease

	// ObservedBundleGeneration is the Bundle.metadata.generation observed
	// at deploy time when source.Kind == BundleTest. Zero otherwise.
	ObservedBundleGeneration int64

	// PerCluster carries the per-target-cluster observed status, mirrored
	// from the Fleet Bundle's per-cluster summary. Empty for sources that
	// haven't been deployed yet (or for the in-cluster Helm path).
	PerCluster []ClusterDeploymentStatusDomain
}

// PhaseInput is the domain projection consumed by RecomputePhase.
// Built by conversions.PhaseInputFromCR; keeps phase.go aifv1-free.
type PhaseInput struct {
	// Components is the per-component release outcome list.
	Components []ComponentRelease

	// DesiredReplicas mirrors workload.spec.replicas (defaulted to 1).
	DesiredReplicas int32

	// ReadyReplicas mirrors workload.status.readyReplicas. P5-2 populates
	// this via the pod informer; P5-1 always sees 0 from envtest, so rule 4
	// "ready >= desired" is effectively "0 >= 0 → Running" until P5-2 lands.
	ReadyReplicas int32

	// AutomaticRecoveryEnabled mirrors
	// spec.strategy.automaticRecovery.enabled (false when the nested struct
	// is nil — matches the kubebuilder default). It keys the three branches
	// of ARCHITECTURE.md §4.4 rule 2:
	//
	//   - enabled + failureCount <  threshold → Degraded
	//   - enabled + failureCount >= threshold → RecoveryInProgress
	//   - disabled                            → Failed (immediate)
	//
	// Placed next to FailureThreshold so the recovery inputs cluster.
	AutomaticRecoveryEnabled bool

	// RecoveryFailureCount mirrors workload.status.recoveryFailureCount.
	RecoveryFailureCount int32

	// FailureThreshold is the defaulted threshold (DefaultFailureThreshold
	// if spec.strategy.automaticRecovery.failureThreshold is nil/zero).
	FailureThreshold int32

	// PriorPhase is workload.status.phase before this reconcile pass.
	// Used by rule 6 (preserve prior phase when no rule matches) and by
	// the RecoveryInProgress exit path.
	PriorPhase Phase

	// PerClusterPhases is the Fleet-path per-target-cluster phase list,
	// projected from workload.status.perCluster (or a fresh DeployResult
	// after a successful Apply). When non-empty, Rule 0 of RecomputePhase
	// fires and aggregates these via AggregateClusterPhases — Fleet is
	// the authoritative state source for the helm/Fleet deployStrategy.
	// Empty for the in-cluster Helm path (P5-2 in-cluster informer) and
	// for sources that haven't been deployed yet.
	PerClusterPhases []ClusterPhase
}

// UpgradeResult is the success-return value of Upgrader.Upgrade. It carries
// enough context for the HTTP handler to construct a response body and an
// audit log line without needing to re-read the Workload CR.
type UpgradeResult struct {
	Namespace     string
	Name          string
	BlueprintName string
	OldVersion    string
	NewVersion    string
}

// UpgradeWorkloadView is the projection of a Workload CR that Upgrader
// inspects: just enough to run the AC validations and to round-trip the
// resourceVersion the adapter uses for the optimistic-lock patch.
// Adapter-owned (internal/workload/upgrade_store.go) — the Upgrader treats
// it as read-only.
//
// Blueprint is nil when SourceKind != Blueprint OR when the source pointer
// is unset; the upgrader checks both invariants before reading it.
type UpgradeWorkloadView struct {
	Namespace       string
	Name            string
	ResourceVersion string
	SourceKind      SourceKind
	Blueprint       *BlueprintRef
}

// UpgradeBlueprintView is the projection of a target Blueprint CR that
// Upgrader inspects. Lineage mirrors aifv1.Blueprint.Spec.BlueprintName;
// Withdrawn is derived from Status.Phase so the upgrader doesn't need to
// know the aifv1 phase enum.
type UpgradeBlueprintView struct {
	Name      string
	Lineage   string
	Withdrawn bool
}

// ClusterDeploymentStatusDomain is the domain-level twin of
// aifv1.ClusterDeploymentStatus (translation in conversions.go). Carried
// in DeployResult.PerCluster (Fleet path) and consumed by RecomputePhase
// via PhaseInput.PerClusterPhases when the per-cluster aggregate is the
// authoritative phase source.
type ClusterDeploymentStatusDomain struct {
	ClusterName string
	Phase       ClusterPhase
	FleetState  string
}
