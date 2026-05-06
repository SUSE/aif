package publish

import (
	"context"
	"errors"
	"log/slog"

	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
)

// Workflow orchestrates the Bundle publish-by-approval lifecycle. It depends
// only on Repository ports and an Authorizer — never on controller-runtime's
// client.Client directly. This keeps the workflow unit-testable without
// envtest and is a hard rule (see memory feedback_oop_directives.md).
//
// Method bodies arrive in plan task P3-1; the interface is locked here so
// REST handlers (P1-10, P3-2..P3-6) can compile against it now.
type Workflow interface {
	// Submit moves a Bundle from Draft to Submitted with the given proposed
	// version and change description. Caller must already hold the bundle
	// author identity in req.User.
	Submit(ctx context.Context, namespace, name string, req SubmitRequest) error

	// Withdraw moves a Submitted Bundle back to Draft on the author's request.
	// No publisher notification.
	Withdraw(ctx context.Context, namespace, name string, user string) error

	// RequestChanges moves a Submitted Bundle to ChangesRequested with the
	// reviewer's comment. Requires the publisher role (verified via Authorizer).
	RequestChanges(ctx context.Context, namespace, name string, req ReviewRequest) error

	// Approve mints a new immutable Blueprint version from the Bundle's current
	// content and resets the Bundle to Draft. Atomic with respect to publish-
	// already-exists detection (see ARCHITECTURE.md §6.5.1). Requires the
	// publisher role (verified via Authorizer).
	//
	// The minted Blueprint can be retrieved via Deps.Blueprints if the caller
	// needs it; the return is intentionally error-only to keep the port free
	// of api/v1alpha1 imports (it will return a domain blueprint.Blueprint
	// once plan task B1 lands).
	Approve(ctx context.Context, namespace, name string, req ApproveRequest) error
}

// SubmitRequest is the input to Workflow.Submit.
type SubmitRequest struct {
	User              string
	ProposedVersion   string
	ChangeDescription string
}

// ApproveRequest is the input to Workflow.Approve.
type ApproveRequest struct {
	User string
}

// ReviewRequest is the input to Workflow.RequestChanges.
type ReviewRequest struct {
	User    string
	Comment string
}

// Authorizer answers "may this user perform this action on this resource?".
// Implemented by a SubjectAccessReview-backed adapter in pkg/authz when that
// package lands; meanwhile a hand-rolled fake satisfies tests.
//
// verb and resource follow K8s SAR conventions (e.g. verb="approve",
// resource="bundles").
type Authorizer interface {
	Allowed(ctx context.Context, user, verb, resource string) (bool, error)
}

// Deps groups the constructor dependencies so that adding a new port doesn't
// churn every test. New entries here ARE allowed; renames are not.
type Deps struct {
	Bundles    bundle.Repository
	Blueprints blueprint.Repository
	Authz      Authorizer
	Logger     *slog.Logger
}

// ErrNotImplemented is returned by every stub method until plan task P3-1
// implements them. Sentinel so callers can errors.Is against it during the
// transition window.
var ErrNotImplemented = errors.New("publish.Workflow: method not implemented yet (lands in P3-1)")

// AllowAllAuthorizer is a no-op Authorizer that approves every request. It is
// the temporary default used by 'make run' and by tests until pkg/authz lands
// a real SubjectAccessReview-backed implementation. Production deployments
// MUST replace this with a SAR-backed authorizer.
type AllowAllAuthorizer struct{}

// Allowed returns (true, nil) unconditionally.
func (AllowAllAuthorizer) Allowed(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}
