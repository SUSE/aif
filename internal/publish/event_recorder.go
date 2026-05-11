package publish

import (
	"context"

	aifv1alpha1 "github.com/SUSE/aif/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
)

// EventRecorder adapts the controller-runtime event recorder to the
// publish.EventRecorder port, keeping pkg/publish free of K8s types.
type EventRecorder struct {
	recorder events.EventRecorder
}

func NewEventRecorder(recorder events.EventRecorder) *EventRecorder {
	return &EventRecorder{recorder: recorder}
}

func (r *EventRecorder) BundleSubmitted(ctx context.Context, namespace, name, user, version string) {
	obj := &aifv1alpha1.Bundle{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
	}
	obj.SetGroupVersionKind(aifv1alpha1.GroupVersion.WithKind("Bundle"))
	r.recorder.Eventf(obj, nil, "Normal", "BundleSubmitted", "Submit", "Bundle submitted by %s with proposed version %s", user, version)
}

func (r *EventRecorder) BundleWithdrawn(ctx context.Context, namespace, name, user string) {
	obj := &aifv1alpha1.Bundle{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
	}
	obj.SetGroupVersionKind(aifv1alpha1.GroupVersion.WithKind("Bundle"))
	r.recorder.Eventf(obj, nil, "Normal", "BundleWithdrawn", "Withdraw", "Bundle withdrawn by %s", user)
}
