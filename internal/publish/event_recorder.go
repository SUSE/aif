package publish

import (
	"context"
	"log/slog"

	aifv1alpha1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/client-go/tools/events"
)

// EventRecorder adapts the controller-runtime event recorder to the
// publish.EventRecorder port, keeping pkg/publish free of K8s types.
type EventRecorder struct {
	recorder events.EventRecorder
	getter   func(ctx context.Context, ns, name string) (*aifv1alpha1.Bundle, error)
}

func NewEventRecorder(recorder events.EventRecorder, getter func(ctx context.Context, ns, name string) (*aifv1alpha1.Bundle, error)) *EventRecorder {
	return &EventRecorder{recorder: recorder, getter: getter}
}

func (r *EventRecorder) BundleSubmitted(ctx context.Context, namespace, name, user, version string) {
	obj, err := r.getter(ctx, namespace, name)
	if err != nil {
		slog.Default().Warn("failed to get bundle for event recording", "error", err, "namespace", namespace, "name", name)
		return
	}
	r.recorder.Eventf(obj, nil, "Normal", "BundleSubmitted", "Submit", "Bundle submitted by %s with proposed version %s", user, version)
}
