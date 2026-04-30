package workload

import "log/slog"

// Manager handles workload lifecycle.
type Manager struct {
	logger *slog.Logger
}

// New creates a new workload manager.
func New(logger *slog.Logger) *Manager {
	return &Manager{
		logger: logger,
	}
}
