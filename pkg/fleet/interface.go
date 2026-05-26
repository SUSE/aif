package fleet

import "context"

// FleetBundleEngine owns the lifecycle of ONE fleet.cattle.io/v1alpha1
// Bundle CR per Workload. Idempotent: Apply with the same spec converges
// to the same Bundle (server-side-apply with stable field manager).
//
// 3 methods (within ISP ≤4 target). Implemented by *bundleEngine in
// bundle_engine.go; test double in fake_bundle_engine.go.
type FleetBundleEngine interface {
	// Apply creates or updates the Fleet Bundle for spec.WorkloadID.
	// Returns the observed Bundle status (read back after SSA) so the
	// caller can mirror it into Workload.status without a second
	// round-trip.
	Apply(ctx context.Context, spec BundleDeploymentSpec) (BundleObservedStatus, error)

	// Teardown deletes the Fleet Bundle for the given workload. Returns
	// nil if the Bundle is already absent. Called from the controller's
	// finalizer block.
	Teardown(ctx context.Context, namespace, workloadID string) error

	// UpdateSettings is called by SettingsReconciler via engine_bus.
	// Currently no-op (FleetSettings is empty); kept for symmetry.
	UpdateSettings(s FleetSettings)
}

// FleetGitRepoEngine owns the lifecycle of per-cluster fleet.cattle.io/v1alpha1
// GitRepo CRs for ONE Workload (one GitRepo per target cluster — see
// ARCHITECTURE.md §6.7). Idempotent: Apply with the same spec converges to the
// same GitRepo set (server-side-apply with stable field manager).
//
// 3 methods (within ISP ≤4 target). Implemented by *gitRepoEngine in
// gitrepo_engine.go; test double in fake_gitrepo_engine.go.
type FleetGitRepoEngine interface {
	// Apply creates or updates the per-cluster GitRepo CRs for
	// spec.WorkloadID, one CR per entry in spec.TargetClusters. Returns
	// the observed aggregate status (read back after SSA) so the caller
	// can mirror it into Workload.status without a second round-trip.
	Apply(ctx context.Context, spec GitRepoDeploymentSpec) (GitRepoObservedStatus, error)

	// Teardown deletes every GitRepo CR labelled with the workloadID in
	// the given namespace. Returns nil if no GitRepo CRs are present.
	// Called from the controller's finalizer block; the controller does
	// not know the per-cluster CR set at delete time, so the engine
	// resolves via label selector (ai.suse.com/workload=<workloadID>).
	Teardown(ctx context.Context, namespace, workloadID string) error

	// UpdateSettings is called by SettingsReconciler via engine_bus.
	// Forwards spec.GitRepoURL/GitBranch/GitAuth into pkg/git.EngineSettings
	// on the embedded git.Engine.
	UpdateSettings(s FleetSettings)
}
