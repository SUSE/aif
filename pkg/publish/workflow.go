package publish

import (
	"context"
	"log/slog"

	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
)

// Deps groups the constructor dependencies.
type Deps struct {
	Bundles    bundle.Repository
	Blueprints blueprint.Repository
	Authz      Authorizer
	Logger     *slog.Logger
}

func New(d Deps) Workflow {
	return &workflowImpl{deps: d}
}

type workflowImpl struct {
	deps Deps
}

func (w *workflowImpl) Submit(_ context.Context, _, _ string, _ SubmitRequest) (bundle.Bundle, error) {
	return bundle.Bundle{}, ErrNotImplemented
}

func (w *workflowImpl) Withdraw(_ context.Context, _, _ string, _ string) (bundle.Bundle, error) {
	return bundle.Bundle{}, ErrNotImplemented
}

func (w *workflowImpl) RequestChanges(_ context.Context, _, _ string, _ ReviewRequest) (bundle.Bundle, error) {
	return bundle.Bundle{}, ErrNotImplemented
}

func (w *workflowImpl) Approve(_ context.Context, _, _ string, _ ApproveRequest) (PublishedBlueprintRef, error) {
	return PublishedBlueprintRef{}, ErrNotImplemented
}
