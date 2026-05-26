package fleet_test

import (
	"testing"

	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"

	"github.com/SUSE/aif/pkg/fleet"
)

func TestMirrorGitRepoStatus_ReadyOneOfOne(t *testing.T) {
	s := fleetv1.GitRepoStatus{DesiredReadyClusters: 1, ReadyClusters: 1}
	got := fleet.MirrorGitRepoStatusForTest(s, "cluster-x")
	if got.ClusterName != "cluster-x" {
		t.Fatalf("ClusterName mismatch: %s", got.ClusterName)
	}
	if got.FleetState != "Ready" {
		t.Fatalf("FleetState %q, want Ready", got.FleetState)
	}
	if got.ConnectionError {
		t.Fatalf("ConnectionError should be false")
	}
}

func TestMirrorGitRepoStatus_NotReady(t *testing.T) {
	s := fleetv1.GitRepoStatus{DesiredReadyClusters: 1, ReadyClusters: 0}
	got := fleet.MirrorGitRepoStatusForTest(s, "cluster-x")
	if got.FleetState != "" {
		t.Fatalf("FleetState %q, want empty (treated as Deploying upstream)", got.FleetState)
	}
}
