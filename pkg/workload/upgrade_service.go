package workload

import "context"

// UpgradeService orchestrates the helm upgrade flow that the recovery
// procedure invokes when AutomaticRecovery is enabled. P5-1 declares the
// port so the WorkloadReconciler has a stable seam to depend on; the
// concrete implementation lands in P5-6 (Recovery procedure).
//
// Two methods, well within the ISP target of ≤4.
type UpgradeService interface {
	// Upgrade performs a helm upgrade of the workload's components.
	// Semantics match Deployer.Deploy but invoke `helm upgrade` instead
	// of install — used by the recovery procedure to roll forward to a
	// fixed chart version after a rollback.
	Upgrade(ctx context.Context, req DeployRequest) (DeployResult, error)

	// Rollback reverts each release to its prior revision via
	// `helm rollback`. Used by the recovery procedure when
	// AutomaticRecovery is enabled and the failure threshold is exceeded.
	Rollback(ctx context.Context, namespace string, releases []ComponentRelease) error
}
