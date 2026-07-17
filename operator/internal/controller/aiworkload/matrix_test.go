package aiworkload

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func bd(deploymentID, applied, state string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{}}
	o.SetGroupVersionKind(bundleDeploymentGVK)
	_ = unstructured.SetNestedField(o.Object, deploymentID, "spec", "deploymentID")
	_ = unstructured.SetNestedField(o.Object, applied, "status", "appliedDeploymentID")
	_ = unstructured.SetNestedField(o.Object, state, "status", "display", "state")
	return o
}

func TestMatrixCellPhase(t *testing.T) {
	R := aiplatformv1alpha1.AIWorkloadClusterPhaseRunning
	F := aiplatformv1alpha1.AIWorkloadClusterPhaseFailed
	P := aiplatformv1alpha1.AIWorkloadClusterPhasePending

	// Render-gated: not current → Pending regardless of ready state.
	if got := matrixCellPhase(bd("s1", "s1", "Ready"), false); got != P {
		t.Fatalf("bundle not render-current must be Pending, got %s", got)
	}
	// Stale ErrApplied (applied != desired) → Pending, NOT Failed.
	if got := matrixCellPhase(bd("s2", "s1", "ErrApplied"), true); got != P {
		t.Fatalf("stale deploymentID must be Pending, got %s", got)
	}
	// Current + Ready / Modified → Running.
	if got := matrixCellPhase(bd("s1", "s1", "Ready"), true); got != R {
		t.Fatalf("Ready+current must be Running, got %s", got)
	}
	if got := matrixCellPhase(bd("s1", "s1", "Modified"), true); got != R {
		t.Fatalf("Modified+current must be Running, got %s", got)
	}
	// Current + ErrApplied → Failed.
	if got := matrixCellPhase(bd("s1", "s1", "ErrApplied"), true); got != F {
		t.Fatalf("ErrApplied+current must be Failed, got %s", got)
	}
	// Current + transient → Pending.
	if got := matrixCellPhase(bd("s1", "s1", "WaitApplied"), true); got != P {
		t.Fatalf("transient must be Pending, got %s", got)
	}
}
