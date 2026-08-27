package aiworkload

import (
	"strconv"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestRetryEpochValue(t *testing.T) {
	w := &aiplatformv1alpha1.AIWorkload{}
	if got := (&AIWorkloadReconciler{}).retryEpochValue(w); got != 0 {
		t.Fatalf("default epoch should be 0, got %d", got)
	}
	w.Annotations = map[string]string{retryEpochAnnotation: strconv.FormatInt(5, 10)}
	if got := (&AIWorkloadReconciler{}).retryEpochValue(w); got != 5 {
		t.Fatalf("want epoch 5, got %d", got)
	}
	w.Annotations[retryEpochAnnotation] = "not-a-number"
	if got := (&AIWorkloadReconciler{}).retryEpochValue(w); got != 0 {
		t.Fatalf("malformed epoch should fall back to 0, got %d", got)
	}
	_ = metav1.Now
}
