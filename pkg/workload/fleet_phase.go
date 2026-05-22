// Package workload — Fleet-state→phase translation helpers.
//
// These helpers are framework-agnostic (no aifv1 imports) so they can be
// reused by the controller, the deployer, and unit tests without dragging
// in K8s API types.
package workload

// ClusterPhase is the per-target-cluster phase derived from a Fleet
// BundleDeployment.status.display.state. Aggregated to workload-level
// Phase by AggregateClusterPhases.
type ClusterPhase string

const (
	ClusterPending   ClusterPhase = "Pending"
	ClusterDeploying ClusterPhase = "Deploying"
	ClusterRunning   ClusterPhase = "Running"
	ClusterFailed    ClusterPhase = "Failed"
)

// MapFleetStateToPhase translates a Fleet BundleDeployment state
// (status.display.state, verbatim) into a workload ClusterPhase.
//
// Validated against SUSE AI Lifecycle Manager
// (aiworkload_controller.go:248-258). The Modified→Running mapping is
// load-bearing: when Fleet manages a Helm chart that creates a Job,
// the cluster eventually garbage-collects the completed Job, and Fleet
// reports the BundleDeployment as Modified (drift detected). That drift
// is healthy steady state, NOT a failure — flipping it to Failed/Degraded
// would flap every workload that ships a Job.
//
// Connection/auth errors are not surfaced here; the adapter
// (pkg/fleet/status.go) detects them via typed condition reasons and
// returns ClusterFailed via the caller, not via this string mapping.
func MapFleetStateToPhase(state string) ClusterPhase {
	switch state {
	case "Ready", "Modified":
		return ClusterRunning
	case "ErrApplied":
		return ClusterFailed
	default:
		return ClusterDeploying
	}
}

// AggregateClusterPhases collapses per-cluster phases into a single
// workload Phase.
//
//   empty                                 → Pending  (no Bundle observed yet)
//   any Failed                            → Failed   (terminal — surfaces fastest)
//   all Running                           → Running
//   all Pending                           → Pending
//   otherwise (mixed states, no Failed)   → Deploying
func AggregateClusterPhases(phases []ClusterPhase) Phase {
	if len(phases) == 0 {
		return PhasePending
	}
	var anyFailed, anyRunning, anyDeploying, allPending bool
	allPending = true
	for _, p := range phases {
		if p != ClusterPending {
			allPending = false
		}
		switch p {
		case ClusterFailed:
			anyFailed = true
		case ClusterRunning:
			anyRunning = true
		case ClusterDeploying:
			anyDeploying = true
		}
	}
	switch {
	case anyFailed:
		return PhaseFailed
	case allPending:
		return PhasePending
	case anyRunning && !anyDeploying:
		return PhaseRunning
	default:
		return PhaseDeploying
	}
}
