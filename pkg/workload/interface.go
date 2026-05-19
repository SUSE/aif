package workload

import "context"

// Deployer reconciles a Workload's resolved component set against the
// cluster. Single concrete production impl in deployer.go; in-memory
// FakeDeployer in fake_deployer.go for controller tests.
//
// Idempotent: re-invocation with the same DeployRequest converges to
// the same cluster state (re-installs, upgrades unchanged releases,
// uninstalls orphans).
//
// Pure orchestrator — never reads K8s directly. All K8s I/O happens
// through injected ports (helm.Engine, blueprint.Repository,
// bundle.Repository, nvidia.Discovery, nvidia.Deployer).
//
// 2 methods (well within ISP target of ≤4).
type Deployer interface {
	// Deploy resolves req.Source to a list of components, drifts orphans
	// against req.Previous, helm-installs each desired component, and
	// returns the per-component outcome plus aggregate phase.
	//
	// Returns (DeployResult, nil) on success. Returns (DeployResult, err)
	// on partial or full failure where DeployResult still reflects what
	// was attempted (so the reconciler can surface useful status). The
	// error is wrapped via errors.Join so the deployer-level sentinel
	// AND the underlying cause are reachable via errors.Is.
	Deploy(ctx context.Context, req DeployRequest) (DeployResult, error)

	// Teardown uninstalls all releases recorded in releases. Used by the
	// reconciler's finalizer block. Returns nil only if all releases
	// are torn down (or were already absent — helm.Engine.Uninstall
	// returns nil for missing releases per its contract).
	Teardown(ctx context.Context, namespace string, releases []ComponentRelease) error
}
