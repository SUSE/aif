package apps

import (
	"log/slog"
	"time"
)

// Catalog manages the AI applications catalog.
type Catalog struct {
	logger        *slog.Logger
	refreshPeriod time.Duration
}

// New creates a new applications catalog.
func New(logger *slog.Logger, refreshPeriod time.Duration) *Catalog {
	return &Catalog{
		logger:        logger,
		refreshPeriod: refreshPeriod,
	}
}
