package aiworkload

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestEventWrapperEmitsAndIsNilSafe(t *testing.T) {
	w := &aiplatformv1alpha1.AIWorkload{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "default"}}

	// Nil recorder must not panic.
	(&AIWorkloadReconciler{}).event(w, corev1.EventTypeWarning, "R", "msg %d", 1)

	rec := record.NewFakeRecorder(4)
	(&AIWorkloadReconciler{Recorder: rec}).event(w, corev1.EventTypeNormal, "RolledBack", "to %s", "1.0.0")
	select {
	case got := <-rec.Events:
		if got == "" {
			t.Fatal("expected an event")
		}
	default:
		t.Fatal("expected one event to be recorded")
	}
}
