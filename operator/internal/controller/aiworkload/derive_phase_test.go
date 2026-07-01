package aiworkload

import (
	"testing"
	"time"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestDerivePhase(t *testing.T) {
	R := aiplatformv1alpha1.AIWorkloadClusterPhaseRunning
	F := aiplatformv1alpha1.AIWorkloadClusterPhaseFailed
	P := aiplatformv1alpha1.AIWorkloadClusterPhasePending

	mkStatuses := func(phases ...aiplatformv1alpha1.AIWorkloadClusterPhase) []aiplatformv1alpha1.AIWorkloadClusterStatus {
		out := make([]aiplatformv1alpha1.AIWorkloadClusterStatus, len(phases))
		for i, p := range phases {
			out[i] = aiplatformv1alpha1.AIWorkloadClusterStatus{ClusterID: "c", Phase: p}
		}
		return out
	}

	tests := []struct {
		name  string
		input []aiplatformv1alpha1.AIWorkloadClusterStatus
		want  aiplatformv1alpha1.AIWorkloadPhase
	}{
		{"empty → Pending", nil, aiplatformv1alpha1.AIWorkloadPhasePending},
		{"all Pending → Pending", mkStatuses(P, P, P), aiplatformv1alpha1.AIWorkloadPhasePending},
		{"all Running → Running", mkStatuses(R, R, R), aiplatformv1alpha1.AIWorkloadPhaseRunning},
		{"single Running → Running", mkStatuses(R), aiplatformv1alpha1.AIWorkloadPhaseRunning},
		{"all Failed → Failed", mkStatuses(F, F), aiplatformv1alpha1.AIWorkloadPhaseFailed},
		{"single Failed → Failed", mkStatuses(F), aiplatformv1alpha1.AIWorkloadPhaseFailed},
		// Degraded: running+pending, no failures (still deploying to some clusters)
		{"Running+Pending no failures → Degraded", mkStatuses(R, P), aiplatformv1alpha1.AIWorkloadPhaseDegraded},
		{"Running+Pending+Pending → Degraded", mkStatuses(R, P, P), aiplatformv1alpha1.AIWorkloadPhaseDegraded},
		// Degraded: running+failed (genuinely degraded)
		{"Running+Failed → Degraded", mkStatuses(R, F), aiplatformv1alpha1.AIWorkloadPhaseDegraded},
		// Degraded: mixed running+pending+failed
		{"Running+Pending+Failed → Degraded", mkStatuses(R, P, F), aiplatformv1alpha1.AIWorkloadPhaseDegraded},
		// Degraded: pending+failed, no running
		{"Pending+Failed no running → Degraded", mkStatuses(P, F), aiplatformv1alpha1.AIWorkloadPhaseDegraded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derivePhase(tt.input)
			if got != tt.want {
				t.Errorf("derivePhase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGuardPhaseTransition(t *testing.T) {
	recent := time.Now()
	stale := time.Now().Add(-10 * time.Minute)

	tests := []struct {
		name      string
		derived   aiplatformv1alpha1.AIWorkloadPhase
		current   aiplatformv1alpha1.AIWorkloadPhase
		createdAt time.Time
		want      aiplatformv1alpha1.AIWorkloadPhase
	}{
		// Failed suppressed during grace period when workload has never been Running.
		{"Failed from empty (recent) → Pending", aiplatformv1alpha1.AIWorkloadPhaseFailed, "", recent, aiplatformv1alpha1.AIWorkloadPhasePending},
		{"Failed from Unknown (recent) → Pending", aiplatformv1alpha1.AIWorkloadPhaseFailed, aiplatformv1alpha1.AIWorkloadPhaseUnknown, recent, aiplatformv1alpha1.AIWorkloadPhasePending},
		{"Failed from Pending (recent) → Pending", aiplatformv1alpha1.AIWorkloadPhaseFailed, aiplatformv1alpha1.AIWorkloadPhasePending, recent, aiplatformv1alpha1.AIWorkloadPhasePending},

		// Grace period expired — stop suppressing, let Failed through.
		{"Failed from empty (stale) → Failed", aiplatformv1alpha1.AIWorkloadPhaseFailed, "", stale, aiplatformv1alpha1.AIWorkloadPhaseFailed},
		{"Failed from Pending (stale) → Failed", aiplatformv1alpha1.AIWorkloadPhaseFailed, aiplatformv1alpha1.AIWorkloadPhasePending, stale, aiplatformv1alpha1.AIWorkloadPhaseFailed},

		// Failed allowed when workload was previously healthy (regardless of age).
		{"Failed from Running → Failed", aiplatformv1alpha1.AIWorkloadPhaseFailed, aiplatformv1alpha1.AIWorkloadPhaseRunning, recent, aiplatformv1alpha1.AIWorkloadPhaseFailed},
		{"Failed from Degraded → Failed", aiplatformv1alpha1.AIWorkloadPhaseFailed, aiplatformv1alpha1.AIWorkloadPhaseDegraded, recent, aiplatformv1alpha1.AIWorkloadPhaseFailed},

		// Failed→Failed stays Failed (no oscillation).
		{"Failed from Failed → Failed", aiplatformv1alpha1.AIWorkloadPhaseFailed, aiplatformv1alpha1.AIWorkloadPhaseFailed, recent, aiplatformv1alpha1.AIWorkloadPhaseFailed},

		// Non-Failed phases are never suppressed.
		{"Running passthrough", aiplatformv1alpha1.AIWorkloadPhaseRunning, aiplatformv1alpha1.AIWorkloadPhasePending, recent, aiplatformv1alpha1.AIWorkloadPhaseRunning},
		{"Pending passthrough", aiplatformv1alpha1.AIWorkloadPhasePending, aiplatformv1alpha1.AIWorkloadPhaseUnknown, recent, aiplatformv1alpha1.AIWorkloadPhasePending},
		{"Degraded passthrough", aiplatformv1alpha1.AIWorkloadPhaseDegraded, aiplatformv1alpha1.AIWorkloadPhasePending, recent, aiplatformv1alpha1.AIWorkloadPhaseDegraded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := guardPhaseTransition(tt.derived, tt.current, tt.createdAt)
			if got != tt.want {
				t.Errorf("guardPhaseTransition(%q, %q, age=%v) = %q, want %q",
					tt.derived, tt.current, time.Since(tt.createdAt).Round(time.Second), got, tt.want)
			}
		})
	}
}
