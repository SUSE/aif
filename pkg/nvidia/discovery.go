package nvidia

import "log/slog"

// Discovery handles NVIDIA GPU discovery and validation.
type Discovery struct {
	logger *slog.Logger
}

// NewDiscovery creates a new NVIDIA discovery service.
func NewDiscovery(logger *slog.Logger) *Discovery {
	return &Discovery{
		logger: logger,
	}
}
