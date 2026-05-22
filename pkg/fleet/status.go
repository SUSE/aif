package fleet

import (
	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/wrangler/v3/pkg/genericcondition"
	corev1 "k8s.io/api/core/v1"
)

// mirrorStatus walks bs and emits one ClusterDeploymentObserved per
// entry in targets, in the same order. If bs has no per-cluster
// information for a target (Fleet hasn't created a BundleDeployment
// yet), the entry's FleetState is empty — caller's
// workload.MapFleetStateToPhase interprets that as Deploying.
//
// Connection/auth detection is sentinel-driven via fleet condition
// reasons (never string-matched). The safe baseline implemented here is:
// emit one entry per target, set FleetState="" when no info, set
// ConnectionError based on top-level Ready=False condition reason.
// Richer per-cluster mapping (via BundleStatus.Summary / PartitionStatus)
// lands when live tests reveal the exact shape we get from a real
// manager.
func mirrorStatus(bs fleetv1.BundleStatus, targets []string) BundleObservedStatus {
	connErr := connectionErrorFromConditions(bs.Conditions)

	out := BundleObservedStatus{
		PerCluster: make([]ClusterDeploymentObserved, 0, len(targets)),
	}
	for _, c := range targets {
		out.PerCluster = append(out.PerCluster, ClusterDeploymentObserved{
			ClusterName:     c,
			FleetState:      "",
			ConnectionError: connErr,
		})
	}
	return out
}

// connectionErrorFromConditions returns true when a Ready=False
// condition carries a Reason indicating downstream connectivity loss.
// Reasons come from Fleet's typed enum — no strings.Contains on
// human-readable messages.
func connectionErrorFromConditions(conds []genericcondition.GenericCondition) bool {
	for _, c := range conds {
		if c.Type != "Ready" || c.Status != corev1.ConditionFalse {
			continue
		}
		switch c.Reason {
		case "Cluster", "ClusterNotReady", "ClusterDisconnected":
			return true
		}
	}
	return false
}
