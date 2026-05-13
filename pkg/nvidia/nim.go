package nvidia

import (
	"context"
	"log/slog"
	"sync"
)

// deployerImpl is the production Deployer. P4-4 implements GenerateValues
// per ARCHITECTURE.md §4.4 sizing formulas; UpdateSettings receives the
// per-cluster registry endpoint pushed by SettingsReconciler (P5-7).
//
// mu guards settings per §8.2.1 sole-writer pattern (mirrors helm.engine).
// UpdateSettings is the SOLE writer; GenerateValues calls snapshot() once
// at entry and uses the returned struct for the rest of the call.
type deployerImpl struct {
	logger *slog.Logger

	mu       sync.RWMutex
	settings EngineSettings
}

// NewDeployer returns a Deployer bound to the given logger. Initial settings
// are zero-valued; the deployer will use in-code defaults until UpdateSettings
// is called by SettingsReconciler.
func NewDeployer(logger *slog.Logger) Deployer {
	return &deployerImpl{logger: logger}
}

// UpdateSettings replaces the current settings snapshot. Sole writer.
func (d *deployerImpl) UpdateSettings(s EngineSettings) {
	d.mu.Lock()
	d.settings = s
	d.mu.Unlock()
}

// snapshot returns the current settings under a read lock. Callers MUST
// invoke this once at method entry and use the returned struct for the
// remainder of the call; never hold the lock across logic.
func (d *deployerImpl) snapshot() EngineSettings {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.settings
}

// GenerateValues is implemented in Task 5. Currently returns
// ErrNotImplemented so the Deployer interface compiles before the
// formula logic lands.
func (d *deployerImpl) GenerateValues(_ context.Context, _ GenerateRequest) (map[string]any, error) {
	return nil, ErrNotImplemented
}
