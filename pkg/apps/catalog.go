package apps

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// errNotImplementedYet is returned by stub Catalog methods until Layer 4
// of the P2-3 plan lands. Tests in Layer 4 will replace these stubs
// with the real implementation; this exists only so main.go keeps
// compiling during Layer 1–3 development.
var errNotImplementedYet = errors.New("apps: catalog implementation pending P2-3 Layer 4")

// catalogImpl is the production Catalog. It owns a slice of registered
// Sources (added via AddSource) and fans out to them on every public
// Catalog method. Caching lives in each Source adapter; catalogImpl
// holds no cache of its own.
type catalogImpl struct {
	logger          *slog.Logger
	refreshInterval time.Duration

	mu      sync.RWMutex
	sources []Source
}

// New returns a Catalog ready to receive AddSource calls. The
// refreshInterval is the default tick cadence handed to each Source
// when no per-Source override is provided via UpdateSettings.
func New(logger *slog.Logger, refreshInterval time.Duration) Catalog {
	return &catalogImpl{
		logger:          logger,
		refreshInterval: refreshInterval,
	}
}

// AddSource registers a Source. Called from cmd/operator/main.go at
// bootstrap. NOT part of the Catalog interface — this is a struct
// method per the registry-pattern decision (P2-3 plan, decision d).
func (c *catalogImpl) AddSource(s Source) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sources = append(c.sources, s)
}

// List — stub. Layer 4 fan-out + dedupe + sort + filter lands here.
func (c *catalogImpl) List(_ context.Context, _ ListOpts) ([]App, error) {
	return nil, errNotImplementedYet
}

// Get — stub. Layer 4 namespace dispatch lands here.
func (c *catalogImpl) Get(_ context.Context, _ string) (App, error) {
	return App{}, errNotImplementedYet
}

// Refresh — stub. Layer 4 fan-out lands here.
func (c *catalogImpl) Refresh(_ context.Context) error {
	return errNotImplementedYet
}

// UpdateSettings — stub. Layer 4 fan-out to adapters lands here.
func (c *catalogImpl) UpdateSettings(_ EngineSettings) {}
