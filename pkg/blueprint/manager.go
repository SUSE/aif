package blueprint

import "log/slog"

// Manager handles blueprint lifecycle.
type Manager struct {
	logger *slog.Logger
}

// New creates a new blueprint manager.
func New(logger *slog.Logger) *Manager {
	return &Manager{
		logger: logger,
	}
}
