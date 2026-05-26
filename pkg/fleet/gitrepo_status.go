package fleet

import (
	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
)

// mirrorGitRepoStatus emits one ClusterDeploymentObserved for the
// supplied cluster from a single GitRepo CR's status. The conservative
// baseline matches mirrorStatus (Bundle path):
//
//   - FleetState="Ready" when DesiredReadyClusters==1 && ReadyClusters==1
//     (the GitRepo CR targets one cluster, so DesiredReadyClusters is
//     always 1 in our shape).
//   - FleetState="" otherwise — caller's MapFleetStateToPhase reads as
//     Deploying.
//   - ConnectionError sourced from connectionErrorFromConditions, the
//     same Bundle-path helper (reason vocabulary unverified; live tests
//     will reveal real Fleet GitRepo reasons).
//
// Per-CR status mining (richer Display.* / Resources[] reads) lands
// when live tests reveal the shape we get from a real Fleet manager.
func mirrorGitRepoStatus(s fleetv1.GitRepoStatus, cluster string) ClusterDeploymentObserved {
	state := ""
	if s.DesiredReadyClusters == 1 && s.ReadyClusters == 1 {
		state = "Ready"
	}
	return ClusterDeploymentObserved{
		ClusterName:     cluster,
		FleetState:      state,
		ConnectionError: connectionErrorFromConditions(s.Conditions),
	}
}
