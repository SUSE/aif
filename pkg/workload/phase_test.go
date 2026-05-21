package workload

import "testing"

func TestRecomputePhase(t *testing.T) {
	cases := []struct {
		name string
		in   PhaseInput
		want Phase
	}{
		{
			name: "no components → Pending",
			in:   PhaseInput{FailureThreshold: 3},
			want: PhasePending,
		},
		{
			name: "one pending-install → Deploying",
			in: PhaseInput{
				Components:       []ComponentRelease{{Status: "pending-install"}},
				FailureThreshold: 3,
			},
			want: PhaseDeploying,
		},
		{
			name: "orphan-uninstall-failed counts as in-flight → Deploying",
			in: PhaseInput{
				Components: []ComponentRelease{
					{Status: "deployed"},
					{Status: ComponentStatusOrphanUninstallFailed},
				},
				FailureThreshold: 3,
			},
			want: PhaseDeploying,
		},
		{
			// ARCHITECTURE.md §4.4 rule 2 branch A: recovery enabled +
			// failureCount < threshold → Degraded. AutomaticRecoveryEnabled
			// MUST be true; with recovery off the same input would surface as
			// Failed-immediate (branch C).
			name: "one failed, recovery=on, count < threshold → Degraded",
			in: PhaseInput{
				Components:               []ComponentRelease{{Status: "deployed"}, {Status: "failed"}},
				AutomaticRecoveryEnabled: true,
				RecoveryFailureCount:     1,
				FailureThreshold:         3,
			},
			want: PhaseDegraded,
		},
		{
			// ARCHITECTURE.md §4.4 rule 2 branch B: recovery enabled +
			// failureCount >= threshold → RecoveryInProgress (NOT Failed).
			// P5-1 owns entry; P5-6 owns the rollback exit.
			name: "failed, recovery=on, count == threshold → RecoveryInProgress",
			in: PhaseInput{
				Components:               []ComponentRelease{{Status: "failed"}},
				AutomaticRecoveryEnabled: true,
				RecoveryFailureCount:     3,
				FailureThreshold:         3,
			},
			want: PhaseRecoveryInProgress,
		},
		{
			// Same branch B, clamp-safety: counter overshoots threshold.
			name: "failed, recovery=on, count > threshold (clamp safety) → RecoveryInProgress",
			in: PhaseInput{
				Components:               []ComponentRelease{{Status: "failed"}},
				AutomaticRecoveryEnabled: true,
				RecoveryFailureCount:     99,
				FailureThreshold:         3,
			},
			want: PhaseRecoveryInProgress,
		},
		{
			// ARCHITECTURE.md §4.4 rule 2 branch C: recovery disabled →
			// Failed immediately, independent of count or threshold. The
			// Degraded → RecoveryInProgress ladder only exists when
			// AutomaticRecovery.Enabled is true.
			name: "failed, recovery=off, count < threshold → Failed (no Degraded intermediate)",
			in: PhaseInput{
				Components:           []ComponentRelease{{Status: "failed"}},
				RecoveryFailureCount: 0,
				FailureThreshold:     3,
				// AutomaticRecoveryEnabled is the zero value (false).
			},
			want: PhaseFailed,
		},
		{
			// Same branch C with a non-trivial count to prove the count
			// branch is bypassed entirely when recovery is off.
			name: "failed, recovery=off, count >= threshold → Failed",
			in: PhaseInput{
				Components:           []ComponentRelease{{Status: "failed"}},
				RecoveryFailureCount: 5,
				FailureThreshold:     3,
			},
			want: PhaseFailed,
		},
		{
			name: "all deployed, ready == desired → Running",
			in: PhaseInput{
				Components:       []ComponentRelease{{Status: "deployed"}, {Status: "deployed"}},
				DesiredReplicas:  3,
				ReadyReplicas:    3,
				FailureThreshold: 3,
			},
			want: PhaseRunning,
		},
		{
			name: "all deployed, ready > desired (HPA scaled up) → Running",
			in: PhaseInput{
				Components:       []ComponentRelease{{Status: "deployed"}},
				DesiredReplicas:  1,
				ReadyReplicas:    5,
				FailureThreshold: 3,
			},
			want: PhaseRunning,
		},
		{
			name: "all deployed, ready == desired == 0 (pre-P5-2 default) → Running",
			in: PhaseInput{
				Components:       []ComponentRelease{{Status: "deployed"}},
				FailureThreshold: 3,
			},
			want: PhaseRunning,
		},
		{
			name: "all deployed, ready < desired → Degraded",
			in: PhaseInput{
				Components:       []ComponentRelease{{Status: "deployed"}},
				DesiredReplicas:  3,
				ReadyReplicas:    1,
				FailureThreshold: 3,
			},
			want: PhaseDegraded,
		},
		{
			name: "prior RecoveryInProgress + components healthy → Running",
			in: PhaseInput{
				Components:       []ComponentRelease{{Status: "deployed"}},
				DesiredReplicas:  1,
				ReadyReplicas:    1,
				FailureThreshold: 3,
				PriorPhase:       PhaseRecoveryInProgress,
			},
			want: PhaseRunning,
		},
		{
			name: "deployed status mixed with unknown helm status → Deploying",
			in: PhaseInput{
				Components: []ComponentRelease{
					{Status: "deployed"},
					{Status: "weird-helm-status"},
				},
				FailureThreshold: 3,
			},
			want: PhaseDeploying,
		},
		{
			// ARCHITECTURE.md §4.4 rule 2 (failed) wins over rule 3 (pending-*).
			// A {failed, pending-install} mix MUST surface as Degraded (or
			// RecoveryInProgress / Failed depending on rule-2 branch), never
			// Deploying. This case locks the spec ordering and would regress
			// to Deploying if the rule order were re-inverted.
			name: "failed + pending-install mixed, recovery=on, count < threshold → Degraded",
			in: PhaseInput{
				Components: []ComponentRelease{
					{Status: "failed"},
					{Status: "pending-install"},
				},
				AutomaticRecoveryEnabled: true,
				RecoveryFailureCount:     1,
				FailureThreshold:         3,
			},
			want: PhaseDegraded,
		},
		{
			// Same ordering check at the RecoveryInProgress boundary
			// (recovery enabled + count >= threshold).
			name: "failed + pending-upgrade mixed, recovery=on, count >= threshold → RecoveryInProgress",
			in: PhaseInput{
				Components: []ComponentRelease{
					{Status: "failed"},
					{Status: "pending-upgrade"},
				},
				AutomaticRecoveryEnabled: true,
				RecoveryFailureCount:     3,
				FailureThreshold:         3,
			},
			want: PhaseRecoveryInProgress,
		},
		{
			// Companion to the {failed, pending} ordering case for the
			// recovery-off branch (rule 2 branch C). Locks BOTH the rule
			// ordering (failed beats pending) AND the three-branch outcome
			// (recovery off → Failed regardless of count or in-flight peer).
			name: "failed + pending-install mixed, recovery=off → Failed",
			in: PhaseInput{
				Components: []ComponentRelease{
					{Status: "failed"},
					{Status: "pending-install"},
				},
				// AutomaticRecoveryEnabled zero-value (false).
				RecoveryFailureCount: 1,
				FailureThreshold:     3,
			},
			want: PhaseFailed,
		},
		{
			// pending-rollback is named in ARCHITECTURE.md §4.4 rule 3 and
			// must be classified as in-flight by its own case arm, not by
			// the "unknown helm status" default.
			name: "pending-rollback only → Deploying",
			in: PhaseInput{
				Components:       []ComponentRelease{{Status: "pending-rollback"}},
				FailureThreshold: 3,
			},
			want: PhaseDeploying,
		},
		{
			// superseded is named in ARCHITECTURE.md §4.4 rule 3 and must
			// be classified as in-flight by its own case arm.
			name: "superseded only → Deploying",
			in: PhaseInput{
				Components:       []ComponentRelease{{Status: "superseded"}},
				FailureThreshold: 3,
			},
			want: PhaseDeploying,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RecomputePhase(tc.in)
			if got != tc.want {
				t.Errorf("RecomputePhase(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
