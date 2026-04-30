package bundle

import "log/slog"

// Manager handles AI bundle lifecycle.
type Manager struct {
	logger *slog.Logger
}

// New creates a new bundle manager.
func New(logger *slog.Logger) *Manager {
	return &Manager{
		logger: logger,
	}
}
