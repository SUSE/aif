package git

import "log/slog"

// FleetEngine manages Fleet-based GitOps operations.
type FleetEngine struct {
	logger *slog.Logger
	gitDir string
}

// NewFleetEngine creates a new Fleet GitOps engine.
func NewFleetEngine(logger *slog.Logger, gitDir string) *FleetEngine {
	return &FleetEngine{
		logger: logger,
		gitDir: gitDir,
	}
}
