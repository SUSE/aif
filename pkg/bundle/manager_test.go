package bundle

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
)

func newTestManager() Manager {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	repo := NewFakeRepository()
	return New(repo, logger)
}

func TestManager_Create_ReturnsErrNotImplemented(t *testing.T) {
	mgr := newTestManager()
	_, err := mgr.Create(context.Background(), Bundle{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestManager_Get_ReturnsErrNotImplemented(t *testing.T) {
	mgr := newTestManager()
	_, err := mgr.Get(context.Background(), "ns", "name")
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestManager_List_ReturnsErrNotImplemented(t *testing.T) {
	mgr := newTestManager()
	_, err := mgr.List(context.Background(), ListOptions{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestManager_Update_ReturnsErrNotImplemented(t *testing.T) {
	mgr := newTestManager()
	_, err := mgr.Update(context.Background(), Bundle{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestManager_Delete_ReturnsErrNotImplemented(t *testing.T) {
	mgr := newTestManager()
	err := mgr.Delete(context.Background(), "ns", "name")
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestManager_ListPendingReview_ReturnsErrNotImplemented(t *testing.T) {
	mgr := newTestManager()
	_, err := mgr.ListPendingReview(context.Background())
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}
