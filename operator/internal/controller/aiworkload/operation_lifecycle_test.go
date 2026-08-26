package aiworkload

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestOperationOutcome(t *testing.T) {
	base := wlWithBlueprint("2.0.0")
	inProgress := aiplatformv1alpha1.AIWorkloadOperation{
		Type: aiplatformv1alpha1.OperationTypeUpgrade, TargetVersion: "2.0.0",
		RequestedAt: metav1.Now(), IntentDigest: intentDigest(base.Spec), State: aiplatformv1alpha1.OperationStateInProgress,
	}

	// Direct-edit supersession: intent moved.
	edited := wlWithBlueprint("3.0.0")
	if got := operationOutcome(edited, inProgress, false, false, false, defaultOperationDeadline); got.State != aiplatformv1alpha1.OperationStateSuperseded {
		t.Fatalf("intent mismatch should supersede, got %s", got.State)
	}
	// Success: certified at target.
	if got := operationOutcome(base, inProgress, true, false, false, defaultOperationDeadline); got.State != aiplatformv1alpha1.OperationStateSucceeded {
		t.Fatalf("certified-at-target should succeed, got %s", got.State)
	}
	// Failure: a matrix cell failed.
	if got := operationOutcome(base, inProgress, false, true, false, defaultOperationDeadline); got.State != aiplatformv1alpha1.OperationStateFailed {
		t.Fatalf("failed cell should fail, got %s", got.State)
	}
	// Timeout.
	timedOut := inProgress
	timedOut.RequestedAt = metav1.NewTime(time.Now().Add(-2 * defaultOperationDeadline))
	if got := operationOutcome(base, timedOut, false, false, false, defaultOperationDeadline); got.State != aiplatformv1alpha1.OperationStateFailed || got.Reason != "TimedOut" {
		t.Fatalf("expired should be Failed/TimedOut, got %s/%s", got.State, got.Reason)
	}
	// Still in progress.
	if got := operationOutcome(base, inProgress, false, false, false, defaultOperationDeadline); got.State != aiplatformv1alpha1.OperationStateInProgress {
		t.Fatalf("no signal yet should stay InProgress, got %s", got.State)
	}
	// Rollback drift.
	rb := inProgress
	rb.Type = aiplatformv1alpha1.OperationTypeRollback
	if got := operationOutcome(base, rb, false, false, true, defaultOperationDeadline); got.State != aiplatformv1alpha1.OperationStateFailed || got.Reason != "BlueprintDrift" {
		t.Fatalf("rollback drift should be Failed/BlueprintDrift, got %s/%s", got.State, got.Reason)
	}
}
