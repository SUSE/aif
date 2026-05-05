package helm

import (
	"log/slog"

	"k8s.io/client-go/rest"
)

// engine is the concrete implementation of the Engine interface.
type engine struct {
	logger   *slog.Logger
	config   *rest.Config
	settings EngineSettings
}

// New creates a new Helm engine.
func New(logger *slog.Logger, config *rest.Config) Engine {
	return &engine{
		logger: logger,
		config: config,
	}
}
