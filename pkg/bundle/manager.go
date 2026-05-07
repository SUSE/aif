package bundle

import (
	"context"
	"errors"
	"log/slog"
)

var ErrNotImplemented = errors.New("bundle.Manager: method not implemented")

type manager struct {
	repo   Repository
	logger *slog.Logger
}

func New(repo Repository, logger *slog.Logger) Manager {
	return &manager{repo: repo, logger: logger}
}

func (m *manager) Create(_ context.Context, _ Bundle) (Bundle, error) {
	return Bundle{}, ErrNotImplemented
}

func (m *manager) Get(_ context.Context, _, _ string) (Bundle, error) {
	return Bundle{}, ErrNotImplemented
}

func (m *manager) List(_ context.Context, _ ListOptions) ([]Bundle, error) {
	return nil, ErrNotImplemented
}

func (m *manager) Update(_ context.Context, _ Bundle) (Bundle, error) {
	return Bundle{}, ErrNotImplemented
}

func (m *manager) Delete(_ context.Context, _, _ string) error {
	return ErrNotImplemented
}

func (m *manager) ListPendingReview(_ context.Context) ([]Bundle, error) {
	return nil, ErrNotImplemented
}
