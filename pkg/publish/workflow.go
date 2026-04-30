package publish

import "log/slog"

// Workflow handles publishing blueprints and workloads.
type Workflow struct {
	logger *slog.Logger
}

// New creates a new publish workflow.
func New(logger *slog.Logger) *Workflow {
	return &Workflow{
		logger: logger,
	}
}
