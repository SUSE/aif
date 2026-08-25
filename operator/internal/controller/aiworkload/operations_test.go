package aiworkload

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func wlWithBlueprint(version string) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "ai", DeployStrategy: aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
			TargetClusters: []string{"local"},
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: version},
			},
		},
	}
}

func TestProjectOperationReconstructsFromJournal(t *testing.T) {
	w := wlWithBlueprint("1.0.0")
	op := aiplatformv1alpha1.AIWorkloadOperation{Type: aiplatformv1alpha1.OperationTypeUpgrade, Nonce: "n", State: aiplatformv1alpha1.OperationStateInProgress, IntentDigest: intentDigest(w.Spec)}
	s, _ := encodeOperation(op)
	w.Annotations = map[string]string{operationAnnotation: s}
	w.Status.ActiveOperation = nil

	(&AIWorkloadReconciler{}).projectOperation(w)
	if w.Status.ActiveOperation == nil || w.Status.ActiveOperation.Nonce != "n" {
		t.Fatalf("expected activeOperation reconstructed from journal, got %+v", w.Status.ActiveOperation)
	}
}

func TestProjectOperationClearsStaleTerminal(t *testing.T) {
	w := wlWithBlueprint("2.0.0") // current intent is v2
	// A terminal op from a DIFFERENT (v1) intent, no journal present.
	w.Status.ActiveOperation = &aiplatformv1alpha1.AIWorkloadOperation{
		Type: aiplatformv1alpha1.OperationTypeUpgrade, State: aiplatformv1alpha1.OperationStateFailed,
		IntentDigest: intentDigest(wlWithBlueprint("1.0.0").Spec),
	}
	(&AIWorkloadReconciler{}).projectOperation(w)
	if w.Status.ActiveOperation != nil {
		t.Fatalf("stale terminal op (intent mismatch, no journal) should be cleared, got %+v", w.Status.ActiveOperation)
	}
}

func TestProjectOperationKeepsMatchingTerminal(t *testing.T) {
	w := wlWithBlueprint("1.0.0")
	w.Status.ActiveOperation = &aiplatformv1alpha1.AIWorkloadOperation{
		Type: aiplatformv1alpha1.OperationTypeUpgrade, State: aiplatformv1alpha1.OperationStateFailed,
		IntentDigest: intentDigest(w.Spec),
	}
	(&AIWorkloadReconciler{}).projectOperation(w)
	if w.Status.ActiveOperation == nil {
		t.Fatalf("terminal op matching current intent must be retained")
	}
}
