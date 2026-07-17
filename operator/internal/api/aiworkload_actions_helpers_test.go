package api

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestNewNonceUnique(t *testing.T) {
	a, b := newNonce(), newNonce()
	if a == "" || b == "" || a == b {
		t.Fatalf("nonces must be non-empty and unique: %q %q", a, b)
	}
}

func TestOperationInProgress(t *testing.T) {
	idle := &aiplatformv1alpha1.AIWorkload{}
	if operationInProgress(idle) {
		t.Fatal("no operation → not in progress")
	}
	viaStatus := &aiplatformv1alpha1.AIWorkload{Status: aiplatformv1alpha1.AIWorkloadStatus{
		ActiveOperation: &aiplatformv1alpha1.AIWorkloadOperation{State: aiplatformv1alpha1.OperationStateInProgress},
	}}
	if !operationInProgress(viaStatus) {
		t.Fatal("InProgress activeOperation → in progress")
	}
	terminal := &aiplatformv1alpha1.AIWorkload{Status: aiplatformv1alpha1.AIWorkloadStatus{
		ActiveOperation: &aiplatformv1alpha1.AIWorkloadOperation{State: aiplatformv1alpha1.OperationStateFailed},
	}}
	if operationInProgress(terminal) {
		t.Fatal("terminal activeOperation → not in progress")
	}
	viaJournal := &aiplatformv1alpha1.AIWorkload{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{operationAnnotation: `{"state":"InProgress"}`},
	}}
	if !operationInProgress(viaJournal) {
		t.Fatal("InProgress journal → in progress")
	}
}
