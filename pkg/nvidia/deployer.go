package nvidia

import "log/slog"

// Deployer handles NVIDIA operator and GPU operator deployment.
type Deployer struct {
	logger *slog.Logger
}

// NewDeployer creates a new NVIDIA deployer service.
func NewDeployer(logger *slog.Logger) *Deployer {
	return &Deployer{
		logger: logger,
	}
}
