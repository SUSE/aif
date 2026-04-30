package helm

import "log/slog"

// Engine manages Helm chart operations.
type Engine struct {
	logger    *slog.Logger
	chartsDir string
}

// New creates a new Helm engine.
func New(logger *slog.Logger, chartsDir string) *Engine {
	return &Engine{
		logger:    logger,
		chartsDir: chartsDir,
	}
}
