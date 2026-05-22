package workload

import "testing"

func TestMapFleetStateToPhase(t *testing.T) {
	cases := []struct {
		state string
		want  ClusterPhase
	}{
		{"Ready", ClusterRunning},
		{"Modified", ClusterRunning}, // critical: GC'd-Job drift is healthy
		{"ErrApplied", ClusterFailed},
		{"Pending", ClusterDeploying},
		{"WaitApplied", ClusterDeploying},
		{"OutOfSync", ClusterDeploying},
		{"WaitCheckIn", ClusterDeploying},
		{"", ClusterDeploying},
		{"SomeUnknownFutureState", ClusterDeploying},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			if got := MapFleetStateToPhase(c.state); got != c.want {
				t.Fatalf("MapFleetStateToPhase(%q) = %v, want %v", c.state, got, c.want)
			}
		})
	}
}
