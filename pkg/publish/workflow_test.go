package publish

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func newTestWorkflow(bundles bundle.Repository) (Workflow, *FakeEventRecorder) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	rec := &FakeEventRecorder{}
	wf := New(Deps{
		Bundles:    bundles,
		Blueprints: blueprint.NewFakeRepository(),
		Authz:      AllowAllAuthorizer{},
		Recorder:   rec,
		Logger:     logger,
	})
	return wf, rec
}

func draftBundle(ns, name string) *aifv1.Bundle {
	return &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  ns,
			Name:       name,
			Generation: 3,
		},
		Spec: aifv1.BundleSpec{
			Title:           "Test Bundle",
			TargetBlueprint: "my-stack",
			UseCase:         "rag",
			Components:      []aifv1.ComponentRef{{Name: "llm"}},
		},
		Status: aifv1.BundleStatus{
			Phase: aifv1.BundlePhaseDraft,
		},
	}
}

// changesRequestedBundle returns a Bundle in ChangesRequested phase with both Submission and Review set,
// since ChangesRequested implies a prior submission that was reviewed.
func changesRequestedBundle(ns, name string) *aifv1.Bundle {
	b := draftBundle(ns, name)
	b.Status.Phase = aifv1.BundlePhaseChangesRequested
	b.Status.Submission = &aifv1.SubmissionStatus{
		ProposedVersion:    "1.0.0",
		ChangeDescription:  "initial release",
		SubmittedBy:        "alice",
		SubmittedAt:        metav1.Now(),
		GenerationAtSubmit: b.Generation,
	}
	b.Status.Review = &aifv1.ReviewStatus{
		ReviewerComment: "needs work",
		ReviewedBy:      "reviewer",
		ReviewedAt:      metav1.Now(),
	}
	return b
}

func submittedBundle(ns, name string) *aifv1.Bundle {
	b := draftBundle(ns, name)
	b.Status.Phase = aifv1.BundlePhaseSubmitted
	b.Status.Submission = &aifv1.SubmissionStatus{
		ProposedVersion:    "1.0.0",
		ChangeDescription:  "initial release",
		SubmittedBy:        "alice",
		SubmittedAt:        metav1.Now(),
		GenerationAtSubmit: b.Generation,
	}
	return b
}

func TestSubmit_DraftToSubmitted(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(draftBundle("ns", "my-bundle"))
	wf, rec := newTestWorkflow(repo)

	got, err := wf.Submit(context.Background(), "ns", "my-bundle", SubmitRequest{
		User:              "alice",
		ProposedVersion:   "1.0.0",
		ChangeDescription: "initial release",
	})

	require.NoError(t, err)
	assert.Equal(t, aifv1.BundlePhaseSubmitted, got.Phase)
	require.NotNil(t, got.Submission)
	assert.Equal(t, "1.0.0", got.Submission.ProposedVersion)
	assert.Equal(t, "initial release", got.Submission.ChangeDescription)
	assert.Equal(t, "alice", got.Submission.SubmittedBy)
	assert.Equal(t, int64(3), got.Submission.GenerationAtSubmit)
	assert.Nil(t, got.Review)

	require.Len(t, rec.Events, 1)
	assert.Contains(t, rec.Events[0], "BundleSubmitted:ns/my-bundle:alice:1.0.0")
}

func TestSubmit_ChangesRequestedToSubmitted(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(changesRequestedBundle("ns", "my-bundle"))
	wf, _ := newTestWorkflow(repo)

	got, err := wf.Submit(context.Background(), "ns", "my-bundle", SubmitRequest{
		User:              "alice",
		ProposedVersion:   "1.0.1",
		ChangeDescription: "addressed feedback",
	})

	require.NoError(t, err)
	assert.Equal(t, aifv1.BundlePhaseSubmitted, got.Phase)
	require.NotNil(t, got.Submission)
	assert.Equal(t, "1.0.1", got.Submission.ProposedVersion)
	assert.Nil(t, got.Review, "review should be cleared on re-submit")
}

func TestSubmit_AlreadySubmitted_ReturnsInvalidTransition(t *testing.T) {
	repo := bundle.NewFakeRepository()
	b := draftBundle("ns", "my-bundle")
	b.Status.Phase = aifv1.BundlePhaseSubmitted
	repo.Seed(b)
	wf, rec := newTestWorkflow(repo)

	_, err := wf.Submit(context.Background(), "ns", "my-bundle", SubmitRequest{
		User:            "alice",
		ProposedVersion: "1.0.0",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidTransition))
	assert.Empty(t, rec.Events, "no event should be recorded on failed submit")
}

func TestSubmit_BundleNotFound(t *testing.T) {
	repo := bundle.NewFakeRepository()
	wf, _ := newTestWorkflow(repo)

	_, err := wf.Submit(context.Background(), "ns", "missing", SubmitRequest{
		User:            "alice",
		ProposedVersion: "1.0.0",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBundleNotFound))
}

func TestSubmit_InvalidVersion(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(draftBundle("ns", "my-bundle"))
	wf, _ := newTestWorkflow(repo)

	cases := []struct {
		name    string
		version string
	}{
		{"empty", ""},
		{"no_patch", "1.0"},
		{"leading_v", "v1.0.0"},
		{"non_numeric", "a.b.c"},
		{"extra_segment", "1.0.0.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := wf.Submit(context.Background(), "ns", "my-bundle", SubmitRequest{
				User:            "alice",
				ProposedVersion: tc.version,
			})
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidVersion), "got: %v", err)
		})
	}
}

func TestSubmit_UserRequired(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(draftBundle("ns", "my-bundle"))
	wf, _ := newTestWorkflow(repo)

	_, err := wf.Submit(context.Background(), "ns", "my-bundle", SubmitRequest{
		User:            "",
		ProposedVersion: "1.0.0",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUserRequired), "got: %v", err)
}

func TestSubmit_ConflictOnUpdate_ReturnsPublishConflict(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(draftBundle("ns", "my-bundle"))
	repo.UpdateStatusErr = apierrors.NewConflict(
		schema.GroupResource{Group: "ai.suse.com", Resource: "bundles"},
		"my-bundle",
		errors.New("resourceVersion changed"),
	)
	wf, rec := newTestWorkflow(repo)

	_, err := wf.Submit(context.Background(), "ns", "my-bundle", SubmitRequest{
		User:            "alice",
		ProposedVersion: "1.0.0",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPublishConflict), "got: %v", err)
	assert.Empty(t, rec.Events, "no event should be recorded on conflict")
}

func TestWithdraw_SubmittedToDraft(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(submittedBundle("ns", "my-bundle"))
	wf, rec := newTestWorkflow(repo)

	got, err := wf.Withdraw(context.Background(), "ns", "my-bundle", "alice")

	require.NoError(t, err)
	assert.Equal(t, aifv1.BundlePhaseDraft, got.Phase)
	assert.Nil(t, got.Submission, "submission should be cleared")
	assert.Nil(t, got.Review, "review should be cleared")

	require.Len(t, rec.Events, 1)
	assert.Contains(t, rec.Events[0], "BundleWithdrawn:ns/my-bundle:alice")
}

func TestWithdraw_ChangesRequestedToDraft(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(changesRequestedBundle("ns", "my-bundle"))
	wf, rec := newTestWorkflow(repo)

	got, err := wf.Withdraw(context.Background(), "ns", "my-bundle", "bob")

	require.NoError(t, err)
	assert.Equal(t, aifv1.BundlePhaseDraft, got.Phase)
	assert.Nil(t, got.Submission)
	assert.Nil(t, got.Review)

	require.Len(t, rec.Events, 1)
	assert.Contains(t, rec.Events[0], "BundleWithdrawn:ns/my-bundle:bob")
}

func TestWithdraw_DraftReturnsInvalidTransition(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(draftBundle("ns", "my-bundle"))
	wf, rec := newTestWorkflow(repo)

	_, err := wf.Withdraw(context.Background(), "ns", "my-bundle", "alice")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidTransition))
	assert.Empty(t, rec.Events, "no event should be recorded on failed withdraw")
}

func TestWithdraw_BundleNotFound(t *testing.T) {
	repo := bundle.NewFakeRepository()
	wf, _ := newTestWorkflow(repo)

	_, err := wf.Withdraw(context.Background(), "ns", "missing", "alice")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBundleNotFound))
}

func TestWithdraw_ConflictOnUpdate(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(submittedBundle("ns", "my-bundle"))
	repo.UpdateStatusErr = apierrors.NewConflict(
		schema.GroupResource{Group: "ai.suse.com", Resource: "bundles"},
		"my-bundle",
		errors.New("resourceVersion changed"),
	)
	wf, rec := newTestWorkflow(repo)

	_, err := wf.Withdraw(context.Background(), "ns", "my-bundle", "alice")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPublishConflict))
	assert.Empty(t, rec.Events, "no event should be recorded on conflict")
}

func TestWithdraw_UserRequired(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(submittedBundle("ns", "my-bundle"))
	wf, _ := newTestWorkflow(repo)

	_, err := wf.Withdraw(context.Background(), "ns", "my-bundle", "")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUserRequired), "got: %v", err)
}

// Keep the other ErrNotImplemented tests for methods not yet implemented.

func TestWorkflow_RequestChanges_ReturnsErrNotImplemented(t *testing.T) {
	wf, _ := newTestWorkflow(bundle.NewFakeRepository())
	_, err := wf.RequestChanges(context.Background(), "ns", "name", ReviewRequest{})
	assert.True(t, errors.Is(err, ErrNotImplemented))
}

func TestWorkflow_Approve_ReturnsErrNotImplemented(t *testing.T) {
	wf, _ := newTestWorkflow(bundle.NewFakeRepository())
	_, err := wf.Approve(context.Background(), "ns", "name", ApproveRequest{})
	assert.True(t, errors.Is(err, ErrNotImplemented))
}
