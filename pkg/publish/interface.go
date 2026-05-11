package publish

import (
	"context"

	"github.com/SUSE/aif/pkg/bundle"
)

// Workflow orchestrates the Bundle publish-by-approval lifecycle.
type Workflow interface {
	// Submit moves a Bundle from Draft to Submitted.
	Submit(ctx context.Context, namespace, name string, req SubmitRequest) (bundle.Bundle, error)
	// Withdraw moves a Submitted Bundle back to Draft.
	Withdraw(ctx context.Context, namespace, name string, user string) (bundle.Bundle, error)
	// RequestChanges moves a Submitted Bundle to ChangesRequested. Requires publisher role.
	RequestChanges(ctx context.Context, namespace, name string, req ReviewRequest) (bundle.Bundle, error)
	// Approve mints an immutable Blueprint version from the Bundle. Requires publisher role.
	// Atomic with respect to publish-already-exists detection (§6.5.1).
	Approve(ctx context.Context, namespace, name string, req ApproveRequest) (PublishedBlueprintRef, error)
}

type SubmitRequest struct {
	User              string
	ProposedVersion   string
	ChangeDescription string
}

type ApproveRequest struct {
	User string
}

type ReviewRequest struct {
	User    string
	Comment string
}

// Authorizer checks publish-action permissions. verb/resource follow K8s SAR conventions.
type Authorizer interface {
	Allowed(ctx context.Context, user, verb, resource string) (bool, error)
}

// EventRecorder emits domain events for publish-workflow actions. The port is
// domain-specific (not a generic K8s recorder) so pkg/publish stays K8s-agnostic.
// K8s-backed adapter in internal/publish/event_recorder.go.
type EventRecorder interface {
	BundleSubmitted(ctx context.Context, namespace, name, user, version string)
	BundleWithdrawn(ctx context.Context, namespace, name, user string)
}
