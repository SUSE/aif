package nvidia

import (
	"context"
	"log/slog"
	"sync"
)

// discoveryImpl is the production Discovery. P2-1 will implement Refresh
// against the SUSE Registry chart index; until then Index returns the
// in-memory cache (initially empty) and Refresh reports ErrNotImplemented.
type discoveryImpl struct {
	logger *slog.Logger

	mu       sync.RWMutex
	cache    []NIMEntry
	settings EngineSettings
}

// NewDiscovery returns a Discovery bound to the given logger.
func NewDiscovery(logger *slog.Logger) Discovery {
	return &discoveryImpl{logger: logger}
}

func (d *discoveryImpl) Index(_ context.Context) ([]NIMEntry, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]NIMEntry, len(d.cache))
	copy(out, d.cache)
	return out, nil
}

func (d *discoveryImpl) Refresh(_ context.Context) error {
	return ErrNotImplemented
}

func (d *discoveryImpl) UpdateSettings(s EngineSettings) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.settings = s
}
