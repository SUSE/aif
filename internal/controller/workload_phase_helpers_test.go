package controller

import (
	"errors"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/workload"
)

func TestComputePhaseWithTransitions_IncrementOnDegradedEntry(t *testing.T) {
	w := &aifv1.Workload{
		Status: aifv1.WorkloadStatus{
			Phase:                aifv1.WorkloadPhaseRunning,
			RecoveryFailureCount: 0,
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "failed"},
			},
		},
	}
	got := computePhaseWithTransitions(w, workload.DeployResult{}, nil)
	if got != workload.PhaseDegraded {
		t.Errorf("phase=%q, want Degraded", got)
	}
	if w.Status.RecoveryFailureCount != 1 {
		t.Errorf("RecoveryFailureCount=%d, want 1 (incremented on Degraded entry)", w.Status.RecoveryFailureCount)
	}
}

func TestComputePhaseWithTransitions_NoIncrementOnDegradedStay(t *testing.T) {
	w := &aifv1.Workload{
		Status: aifv1.WorkloadStatus{
			Phase:                aifv1.WorkloadPhaseDegraded,
			RecoveryFailureCount: 1,
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "failed"},
			},
		},
	}
	got := computePhaseWithTransitions(w, workload.DeployResult{}, nil)
	if got != workload.PhaseDegraded {
		t.Errorf("phase=%q, want Degraded", got)
	}
	if w.Status.RecoveryFailureCount != 1 {
		t.Errorf("RecoveryFailureCount=%d, want 1 (no increment on stay)", w.Status.RecoveryFailureCount)
	}
}

func TestComputePhaseWithTransitions_ResetOnRunningEntry(t *testing.T) {
	w := &aifv1.Workload{
		Status: aifv1.WorkloadStatus{
			Phase:                aifv1.WorkloadPhaseDegraded,
			RecoveryFailureCount: 2,
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "deployed"},
			},
		},
	}
	got := computePhaseWithTransitions(w, workload.DeployResult{}, nil)
	if got != workload.PhaseRunning {
		t.Errorf("phase=%q, want Running", got)
	}
	if w.Status.RecoveryFailureCount != 0 {
		t.Errorf("RecoveryFailureCount=%d, want 0 (reset on Running entry)", w.Status.RecoveryFailureCount)
	}
}

func TestComputePhaseWithTransitions_NoResetOnRunningStay(t *testing.T) {
	w := &aifv1.Workload{
		Status: aifv1.WorkloadStatus{
			Phase:                aifv1.WorkloadPhaseRunning,
			RecoveryFailureCount: 0,
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "deployed"},
			},
		},
	}
	// Pre-set count to a non-zero (shouldn't happen in real flow but
	// asserts the "stay" branch doesn't wipe).
	w.Status.RecoveryFailureCount = 5
	got := computePhaseWithTransitions(w, workload.DeployResult{}, nil)
	if got != workload.PhaseRunning {
		t.Errorf("phase=%q, want Running", got)
	}
	if w.Status.RecoveryFailureCount != 5 {
		t.Errorf("RecoveryFailureCount=%d, want 5 (no reset on stay)", w.Status.RecoveryFailureCount)
	}
}

func TestComputePhaseWithTransitions_SpecChangeFromFailedResets(t *testing.T) {
	w := &aifv1.Workload{}
	w.Generation = 2
	w.Status.ObservedGeneration = 1
	w.Status.Phase = aifv1.WorkloadPhaseFailed
	w.Status.RecoveryFailureCount = 3
	w.Status.ComponentReleases = []aifv1.ComponentReleaseStatus{
		{Name: "n", Status: "pending-install"},
	}

	got := computePhaseWithTransitions(w, workload.DeployResult{}, nil)
	if got != workload.PhaseDeploying {
		t.Errorf("phase=%q, want Deploying (spec change clears Failed)", got)
	}
	if w.Status.RecoveryFailureCount != 0 {
		t.Errorf("RecoveryFailureCount=%d, want 0 (reset on spec-change-from-Failed)", w.Status.RecoveryFailureCount)
	}
}

func TestComputePhaseWithTransitions_ThresholdPromotesFailed(t *testing.T) {
	// Counter at threshold-1; this reconcile's increment pushes it to threshold,
	// and the second RecomputePhase call promotes Degraded→Failed.
	threshold := int32(3)
	w := &aifv1.Workload{
		Spec: aifv1.WorkloadSpec{
			Strategy: &aifv1.DeploymentStrategy{
				AutomaticRecovery: &aifv1.AutomaticRecoveryStrategy{
					Enabled:          true,
					FailureThreshold: &threshold,
				},
			},
		},
		Status: aifv1.WorkloadStatus{
			Phase:                aifv1.WorkloadPhaseRunning, // entering Degraded this pass
			RecoveryFailureCount: 2,                          // about to become 3
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "failed"},
			},
		},
	}

	got := computePhaseWithTransitions(w, workload.DeployResult{}, nil)
	if got != workload.PhaseFailed {
		t.Errorf("phase=%q, want Failed (counter hit threshold on this pass)", got)
	}
	if w.Status.RecoveryFailureCount != 3 {
		t.Errorf("RecoveryFailureCount=%d, want 3", w.Status.RecoveryFailureCount)
	}
}

func TestApplyErrorPhaseOverrides_NestedBlueprintForcesFailed(t *testing.T) {
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{Phase: aifv1.WorkloadPhasePending}}
	phase := workload.PhaseDeploying
	applyErrorPhaseOverrides(w, &phase, workload.ErrNestedBlueprintNotSupported)
	if phase != workload.PhaseFailed {
		t.Errorf("phase=%q, want Failed (nested-Blueprint is terminal)", phase)
	}
}

func TestApplyErrorPhaseOverrides_UnclassifiedPreservesPriorPhase(t *testing.T) {
	// Latent-bug fix: RecomputePhase always returns at least Pending, so the
	// spec's "*phase == ''" check never fires. We preserve prior phase
	// whenever prior is non-empty AND the error is unclassified, regardless
	// of what RecomputePhase produced this pass.
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{Phase: aifv1.WorkloadPhaseRunning}}
	phase := workload.PhasePending // what RecomputePhase returned (no components yet)
	applyErrorPhaseOverrides(w, &phase, errors.New("transient cluster bug"))
	if phase != workload.PhaseRunning {
		t.Errorf("phase=%q, want Running (prior preserved on unclassified error)", phase)
	}
}

func TestApplyErrorPhaseOverrides_NoErrorNoChange(t *testing.T) {
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{Phase: aifv1.WorkloadPhaseRunning}}
	phase := workload.PhaseDeploying
	applyErrorPhaseOverrides(w, &phase, nil)
	if phase != workload.PhaseDeploying {
		t.Errorf("phase=%q, want Deploying (no override when err==nil)", phase)
	}
}
