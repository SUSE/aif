package controller

import (
	"errors"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/workload"
)

// recoveryEnabledSpec returns a Spec with AutomaticRecovery.Enabled=true
// and FailureThreshold=3. Helpers that want to reach Degraded (rule 2
// branch B) MUST set this — with recovery off, a failed component goes
// straight to Failed and the counter never increments.
func recoveryEnabledSpec() aifv1.WorkloadSpec {
	threshold := int32(3)
	return aifv1.WorkloadSpec{
		Strategy: &aifv1.DeploymentStrategy{
			AutomaticRecovery: &aifv1.AutomaticRecoveryStrategy{
				Enabled:          true,
				FailureThreshold: &threshold,
			},
		},
	}
}

func TestComputePhaseWithTransitions_IncrementOnDegradedEntry(t *testing.T) {
	// AutomaticRecovery.Enabled=true so a failed component routes to
	// Degraded (rule 2 branch B), which is the phase whose entry the
	// counter increments on. With recovery off the same input would
	// produce Failed and no counter bump.
	w := &aifv1.Workload{
		Spec: recoveryEnabledSpec(),
		Status: aifv1.WorkloadStatus{
			Phase:                aifv1.WorkloadPhaseRunning,
			RecoveryFailureCount: 0,
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "failed"},
			},
		},
	}
	got := computePhaseWithTransitions(w)
	if got != workload.PhaseDegraded {
		t.Errorf("phase=%q, want Degraded", got)
	}
	if w.Status.RecoveryFailureCount != 1 {
		t.Errorf("RecoveryFailureCount=%d, want 1 (incremented on Degraded entry)", w.Status.RecoveryFailureCount)
	}
}

func TestComputePhaseWithTransitions_NoIncrementOnDegradedStay(t *testing.T) {
	// AutomaticRecovery.Enabled=true so the same failed-component input
	// stays in Degraded across reconciles (rather than collapsing to
	// Failed-immediate under the recovery-off branch).
	w := &aifv1.Workload{
		Spec: recoveryEnabledSpec(),
		Status: aifv1.WorkloadStatus{
			Phase:                aifv1.WorkloadPhaseDegraded,
			RecoveryFailureCount: 1,
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "failed"},
			},
		},
	}
	got := computePhaseWithTransitions(w)
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
	got := computePhaseWithTransitions(w)
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
	got := computePhaseWithTransitions(w)
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

	got := computePhaseWithTransitions(w)
	if got != workload.PhaseDeploying {
		t.Errorf("phase=%q, want Deploying (spec change clears Failed)", got)
	}
	if w.Status.RecoveryFailureCount != 0 {
		t.Errorf("RecoveryFailureCount=%d, want 0 (reset on spec-change-from-Failed)", w.Status.RecoveryFailureCount)
	}
}

func TestComputePhaseWithTransitions_ThresholdPromotesRecoveryInProgress(t *testing.T) {
	// AutomaticRecovery.Enabled=true: counter at threshold-1; this
	// reconcile's increment pushes it to threshold, and the second
	// RecomputePhase call promotes Degraded → RecoveryInProgress (rule 2
	// branch B, not Failed — that's the recovery-OFF path covered by the
	// next test). recoveryEnabledSpec() uses threshold=3, which matches
	// the count→3 expectation below.
	w := &aifv1.Workload{
		Spec: recoveryEnabledSpec(),
		Status: aifv1.WorkloadStatus{
			Phase:                aifv1.WorkloadPhaseRunning, // entering Degraded this pass
			RecoveryFailureCount: 2,                          // about to become 3
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "failed"},
			},
		},
	}

	got := computePhaseWithTransitions(w)
	if got != workload.PhaseRecoveryInProgress {
		t.Errorf("phase=%q, want RecoveryInProgress (counter hit threshold + recovery on)", got)
	}
	if w.Status.RecoveryFailureCount != 3 {
		t.Errorf("RecoveryFailureCount=%d, want 3", w.Status.RecoveryFailureCount)
	}
}

// TestComputePhaseWithTransitions_FirstReconcileFailedComponentDegrades
// covers the bootstrap path: a fresh CR with no prior Status.Phase set and
// AutomaticRecovery enabled, fed a single failed component. RecomputePhase
// produces Degraded, the increment branch fires (priorPhase=="" != Degraded),
// and the counter lands at 1 in a single pass. Without this case, the
// first-reconcile path is only exercised end-to-end via the envtest suite.
func TestComputePhaseWithTransitions_FirstReconcileFailedComponentDegrades(t *testing.T) {
	w := &aifv1.Workload{
		Spec: recoveryEnabledSpec(),
		Status: aifv1.WorkloadStatus{
			Phase:                "", // fresh CR — no phase yet
			RecoveryFailureCount: 0,
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "failed"},
			},
		},
	}

	got := computePhaseWithTransitions(w)
	if got != workload.PhaseDegraded {
		t.Errorf("phase=%q, want Degraded (first reconcile + failed component + recovery on)", got)
	}
	if w.Status.RecoveryFailureCount != 1 {
		t.Errorf("RecoveryFailureCount=%d, want 1 (incremented on Degraded entry from empty prior phase)", w.Status.RecoveryFailureCount)
	}
}

func TestComputePhaseWithTransitions_RecoveryDisabledFailsImmediate(t *testing.T) {
	// AutomaticRecovery.Enabled=false (zero-value / nil strategy): the
	// failed component routes to PhaseFailed immediately (rule 2 branch C).
	// Since the candidate is NEVER Degraded on this path, the counter
	// increment branch in computePhaseWithTransitions does not fire — the
	// counter stays at its prior value.
	w := &aifv1.Workload{
		// No Spec.Strategy → AutomaticRecoveryEnabled defaults to false.
		Status: aifv1.WorkloadStatus{
			Phase:                aifv1.WorkloadPhaseRunning,
			RecoveryFailureCount: 0,
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "n", Status: "failed"},
			},
		},
	}

	got := computePhaseWithTransitions(w)
	if got != workload.PhaseFailed {
		t.Errorf("phase=%q, want Failed (recovery off → no Degraded intermediate)", got)
	}
	if w.Status.RecoveryFailureCount != 0 {
		t.Errorf("RecoveryFailureCount=%d, want 0 (counter does not bump when candidate is not Degraded)", w.Status.RecoveryFailureCount)
	}
}

func TestApplyErrorPhaseOverrides_NestedBlueprintForcesFailed(t *testing.T) {
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{Phase: aifv1.WorkloadPhasePending}}
	phase := workload.PhaseDeploying
	applyErrorPhaseOverrides(workload.Phase(w.Status.Phase), &phase, workload.ErrNestedBlueprintNotSupported)
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
	applyErrorPhaseOverrides(workload.Phase(w.Status.Phase), &phase, errors.New("transient cluster bug"))
	if phase != workload.PhaseRunning {
		t.Errorf("phase=%q, want Running (prior preserved on unclassified error)", phase)
	}
}

func TestApplyErrorPhaseOverrides_NoErrorNoChange(t *testing.T) {
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{Phase: aifv1.WorkloadPhaseRunning}}
	phase := workload.PhaseDeploying
	applyErrorPhaseOverrides(workload.Phase(w.Status.Phase), &phase, nil)
	if phase != workload.PhaseDeploying {
		t.Errorf("phase=%q, want Deploying (no override when err==nil)", phase)
	}
}

// TestApplyErrorPhaseOverrides_PriorPhaseParamWins locks the contract that
// applyErrorPhaseOverrides preserves the phase passed via the priorPhase
// parameter, not whatever happens to be on w.Status.Phase at call time. The
// reconcile body captures priorPhase BEFORE writing the new phase, so passing
// it explicitly makes the override visibly correct rather than coincidentally
// correct.
func TestApplyErrorPhaseOverrides_PriorPhaseParamWins(t *testing.T) {
	// w.Status.Phase is Running, but the caller passed Degraded as
	// priorPhase. The override MUST use the parameter, not the struct field.
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{Phase: aifv1.WorkloadPhaseRunning}}
	_ = w // intentionally unused — proves the function does not read the field
	phase := workload.PhasePending
	applyErrorPhaseOverrides(workload.PhaseDegraded, &phase, errors.New("transient cluster bug"))
	if phase != workload.PhaseDegraded {
		t.Errorf("phase=%q, want Degraded (priorPhase param is the source of truth)", phase)
	}
}
