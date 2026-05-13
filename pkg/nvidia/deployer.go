package nvidia

import (
	"context"
	"log/slog"
)

// deployerImpl is the production Deployer. P4-4 will implement the NIM
// sizing formulas from ARCHITECTURE.md §4.4; until then GenerateValues
// returns ErrNotImplemented.
type deployerImpl struct {
	logger *slog.Logger
}

// NewDeployer returns a Deployer bound to the given logger.
func NewDeployer(logger *slog.Logger) Deployer {
	return &deployerImpl{logger: logger}
}

func (d *deployerImpl) GenerateValues(_ context.Context, _ GenerateRequest) (map[string]any, error) {
	return nil, ErrNotImplemented
}

// UpdateSettings is a no-op until Task 3 fleshes out the struct with a
// settings field; placeholder to satisfy the Deployer interface during
// the staged rollout.
func (d *deployerImpl) UpdateSettings(_ EngineSettings) {}
