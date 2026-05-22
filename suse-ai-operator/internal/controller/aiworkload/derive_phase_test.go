package aiworkload

import (
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/suse-ai-operator/api/v1alpha1"
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
