package controller

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

// fakeRecorder implements record.EventRecorder for testing
type fakeRecorder struct {
	events []string
}

func (f *fakeRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	f.events = append(f.events, eventtype+":"+reason+":"+message)
}

func (f *fakeRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	// Not used in tests
}

func (f *fakeRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	// Not used in tests
}

var _ record.EventRecorder = &fakeRecorder{}

// findCondition finds a condition by type in a condition list
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// containsEventReason checks if an event string contains the given reason
func containsEventReason(event, reason string) bool {
	parts := strings.Split(event, ":")
	if len(parts) >= 2 {
		return parts[1] == reason
	}
	return false
}
