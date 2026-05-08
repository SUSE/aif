package publish

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
)

func newTestWorkflow() Workflow {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return New(Deps{
		Bundles:    bundle.NewFakeRepository(),
		Blueprints: blueprint.NewFakeRepository(),
		Authz:      AllowAllAuthorizer{},
		Logger:     logger,
	})
}

func TestWorkflow_Submit_ReturnsErrNotImplemented(t *testing.T) {
	wf := newTestWorkflow()
	_, err := wf.Submit(context.Background(), "ns", "name", SubmitRequest{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestWorkflow_Withdraw_ReturnsErrNotImplemented(t *testing.T) {
	wf := newTestWorkflow()
	_, err := wf.Withdraw(context.Background(), "ns", "name", "user")
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestWorkflow_RequestChanges_ReturnsErrNotImplemented(t *testing.T) {
	wf := newTestWorkflow()
	_, err := wf.RequestChanges(context.Background(), "ns", "name", ReviewRequest{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestWorkflow_Approve_ReturnsErrNotImplemented(t *testing.T) {
	wf := newTestWorkflow()
	_, err := wf.Approve(context.Background(), "ns", "name", ApproveRequest{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}
