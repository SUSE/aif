// pkg/helm/helm.go
package helm

import (
	"log/slog"
	"os"
	"sync"

	"helm.sh/helm/v3/pkg/action"
	"k8s.io/client-go/rest"
)

// engine is the concrete implementation of the Engine interface. Holds an
// injected helmRunner (production = realRunner; tests inject a fake), a
// writable chart download directory, and a settings cache guarded by an
// RWMutex per §8.2.1. cfgFactory is an injection point so unit tests can
// bypass the real action.Configuration init (which would touch a live
// apiserver).
type engine struct {
	logger     *slog.Logger
	config     *rest.Config
	runner     helmRunner
	chartDir   string
	cfgFactory func(namespace string) (*action.Configuration, error)

	mu       sync.RWMutex
	settings EngineSettings
}

// Option configures the engine at construction time.
type Option func(*engine)

// WithRunner injects a custom helmRunner. Production callers omit this
// (realRunner is the default). Tests use this to inject a fake.
func WithRunner(r helmRunner) Option { return func(e *engine) { e.runner = r } }

// WithChartDir overrides the directory pulled .tgz charts are written to.
// Default: os.TempDir(). Production deployment should set this to a
// writable mount (e.g. /data/charts) because the operator container's root
// FS is read-only.
func WithChartDir(dir string) Option { return func(e *engine) { e.chartDir = dir } }

// WithActionConfigFactory overrides the per-namespace action.Configuration
// factory. Tests use this to bypass the real Helm SDK init that would
// otherwise require a live apiserver. Production callers omit this and the
// engine uses its own newActionConfig.
func WithActionConfigFactory(f func(string) (*action.Configuration, error)) Option {
	return func(e *engine) { e.cfgFactory = f }
}

// New creates a new Helm engine.
func New(logger *slog.Logger, config *rest.Config, opts ...Option) Engine {
	e := &engine{
		logger:   logger,
		config:   config,
		chartDir: os.TempDir(),
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.runner == nil {
		e.runner = newRealRunner(config)
	}
	if e.cfgFactory == nil {
		e.cfgFactory = e.newActionConfig
	}
	return e
}

// snapshot returns the current settings under a read lock. Callers MUST
// invoke this once at method entry and use the returned struct for the
// remainder of the call; never hold the lock across SDK calls.
func (e *engine) snapshot() EngineSettings {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.settings
}
