package workload

import "context"

// Deployer reconciles a Workload's resolved component set against the
// cluster. Single concrete production impl in deployer.go; in-memory
// FakeDeployer in fake_deployer.go for controller tests.
//
// Idempotent: re-invocation with the same DeployRequest converges to
// the same cluster state (reconciles Fleet Bundle to desired spec).
//
// Pure orchestrator — never reads K8s directly. All K8s I/O happens
// through injected ports (helm.ValueRenderer, fleet.FleetBundleEngine,
// blueprint.Repository, bundle.Repository, nvidia.Discovery, nvidia.Deployer).
//
// 2 methods (well within ISP target of ≤4).
type Deployer interface {
	// Deploy resolves req.Source into components, renders per-component
	// values via helm.ValueRenderer (layers 1-5), assembles a Fleet
	// BundleDeploymentSpec, and dispatches it via FleetBundleEngine.Apply.
	// Returns the per-component release records plus aggregate Phase
	// translated from Fleet's per-cluster state.
	//
	// Returns (DeployResult, nil) on success. Returns (DeployResult, err)
	// on value-render or Fleet-apply failure; DeployResult still reflects
	// what was attempted so the reconciler can surface useful status.
	Deploy(ctx context.Context, req DeployRequest) (DeployResult, error)

	// Teardown deletes the Fleet Bundle for the workload. Used by the
	// reconciler's finalizer block. Fleet handles per-cluster uninstall
	// and orphan cleanup declaratively. Returns nil if the Bundle is deleted
	// or was already absent.
	Teardown(ctx context.Context, namespace, workloadID string, releases []ComponentRelease) error
}

// Upgrader is the workflow port for the P5-3 upgrade action. It runs the 5
// validation rules from PROJECT_PLAN.md §P5-3, emits a UpgradeStarted event
// via UpgradeEventRecorder, and persists the spec change via Repository.Patch
// (merge-patch, optimistic concurrency).
//
// aifv1-free per the layering rule (this is interface.go).
type Upgrader interface {
	Upgrade(ctx context.Context, namespace, name, toBlueprintVersion, user string) (UpgradeResult, error)
}

// UpgradeEventRecorder emits domain events for the upgrade workflow. The port
// keeps pkg/workload free of k8s.io/client-go/tools/events. The production
// adapter lives in internal/workload/event_recorder.go.
type UpgradeEventRecorder interface {
	UpgradeStarted(ctx context.Context, namespace, name, oldVersion, newVersion string)
}
