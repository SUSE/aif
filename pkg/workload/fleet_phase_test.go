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

func TestAggregateClusterPhases(t *testing.T) {
	cases := []struct {
		name  string
		in    []ClusterPhase
		want  Phase
	}{
		{"empty", []ClusterPhase{}, PhasePending},
		{"all running", []ClusterPhase{ClusterRunning, ClusterRunning}, PhaseRunning},
		{"any failed", []ClusterPhase{ClusterRunning, ClusterFailed}, PhaseFailed},
		{"any deploying no failed", []ClusterPhase{ClusterRunning, ClusterDeploying}, PhaseDeploying},
		{"all deploying", []ClusterPhase{ClusterDeploying, ClusterDeploying}, PhaseDeploying},
		{"all pending", []ClusterPhase{ClusterPending, ClusterPending}, PhasePending},
		{"pending mixed with deploying yields deploying", []ClusterPhase{ClusterPending, ClusterDeploying}, PhaseDeploying},
		{"single running", []ClusterPhase{ClusterRunning}, PhaseRunning},
		{"single pending", []ClusterPhase{ClusterPending}, PhasePending},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AggregateClusterPhases(c.in); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}
