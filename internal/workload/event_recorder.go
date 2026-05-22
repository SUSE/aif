// Package workload contains the K8s-typed adapters for pkg/workload ports
// that cannot live in pkg/workload itself (which must stay framework-agnostic
// for its non-repository.go / non-conversions.go files per CLAUDE.md).
//
// Today this package holds only the UpgradeEventRecorder adapter — built for
// P5-3. Future controllers/handlers that need to emit Workload-domain events
// against k8s.io/client-go/tools/events would add their adapters here.
package workload

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
)

// EventRecorder adapts the controller-runtime event recorder to the
// workload.UpgradeEventRecorder port, keeping pkg/workload free of K8s
// machinery. Mirrors internal/publish/event_recorder.go.
type EventRecorder struct {
	recorder events.EventRecorder
}

// NewEventRecorder returns an EventRecorder backed by the provided
// k8s.io/client-go/tools/events.EventRecorder.
func NewEventRecorder(recorder events.EventRecorder) *EventRecorder {
	return &EventRecorder{recorder: recorder}
}

// UpgradeStarted emits a Normal event with reason=UpgradeStarted on the
// Workload CR identified by (namespace, name). Per AC line 2004 the message
// is "Upgrading from {oldVersion} to {newVersion}". Called BEFORE the spec
// patch so the audit trail records the intent even if the patch races.
func (r *EventRecorder) UpgradeStarted(_ context.Context, namespace, name, oldVersion, newVersion string) {
	obj := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
	}
	obj.SetGroupVersionKind(aifv1.GroupVersion.WithKind("Workload"))
	r.recorder.Eventf(obj, nil, "Normal", conditions.ReasonUpgradeStarted, "Upgrade",
		"Upgrading from %s to %s", oldVersion, newVersion)
}
