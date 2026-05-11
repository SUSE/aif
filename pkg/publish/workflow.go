package publish

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func isValidVersion(v string) bool {
	return semverPattern.MatchString(v)
}

// Deps groups the constructor dependencies.
type Deps struct {
	Bundles    bundle.Repository
	Blueprints blueprint.Repository
	Authz      Authorizer
	Recorder   EventRecorder
	Logger     *slog.Logger
}

func New(d Deps) Workflow {
	return &workflowImpl{deps: d}
}

type workflowImpl struct {
	deps Deps
}

func (w *workflowImpl) Submit(ctx context.Context, namespace, name string, req SubmitRequest) (bundle.Bundle, error) {
	if req.User == "" {
		return bundle.Bundle{}, fmt.Errorf("submit: %w", ErrUserRequired)
	}
	if !isValidVersion(req.ProposedVersion) {
		return bundle.Bundle{}, fmt.Errorf("proposedVersion %q: %w", req.ProposedVersion, ErrInvalidVersion)
	}

	cr, err := w.deps.Bundles.Get(ctx, namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return bundle.Bundle{}, fmt.Errorf("bundle %s/%s: %w", namespace, name, ErrBundleNotFound)
		}
		return bundle.Bundle{}, fmt.Errorf("get bundle: %w", err)
	}

	if cr.Status.Phase != aifv1.BundlePhaseDraft && cr.Status.Phase != aifv1.BundlePhaseChangesRequested {
		return bundle.Bundle{}, fmt.Errorf(
			"bundle %s/%s is in phase %q, must be Draft or ChangesRequested: %w",
			namespace, name, cr.Status.Phase, ErrInvalidTransition,
		)
	}

	cr.Status.Phase = aifv1.BundlePhaseSubmitted
	cr.Status.Submission = &aifv1.SubmissionStatus{
		ProposedVersion:    req.ProposedVersion,
		ChangeDescription:  req.ChangeDescription,
		SubmittedBy:        req.User,
		SubmittedAt:        metav1.Now(),
		GenerationAtSubmit: cr.Generation,
	}
	cr.Status.Review = nil

	if err := w.deps.Bundles.UpdateStatus(ctx, cr); err != nil {
		if apierrors.IsConflict(err) {
			return bundle.Bundle{}, fmt.Errorf("concurrent update on %s/%s: %w", namespace, name, ErrPublishConflict)
		}
		return bundle.Bundle{}, fmt.Errorf("update bundle status: %w", err)
	}

	if w.deps.Recorder != nil {
		w.deps.Recorder.BundleSubmitted(ctx, namespace, name, req.User, req.ProposedVersion)
	}

	return bundle.BundleFromCR(cr), nil
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
