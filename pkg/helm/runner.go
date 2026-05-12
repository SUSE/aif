// pkg/helm/runner.go
package helm

import (
	"context"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/rest"
)

// installArgs is the pure-data form of action.NewInstall(cfg) parameters.
// Keeps the runner adapter free of branching logic.
type installArgs struct {
	Namespace       string
	ReleaseName     string
	Chart           *chart.Chart
	Values          map[string]any
	Wait            bool
	Timeout         time.Duration
	CreateNamespace bool
}

// upgradeArgs mirrors installArgs for the upgrade path.
type upgradeArgs struct {
	Namespace string
	Chart     *chart.Chart
	Values    map[string]any
	Wait      bool
	Timeout   time.Duration
}

// helmLifecycler covers the install / upgrade / uninstall path.
// 3 methods (≤4 per ISP).
type helmLifecycler interface {
	Install(ctx context.Context, cfg *action.Configuration, args installArgs) (*release.Release, error)
	Upgrade(ctx context.Context, cfg *action.Configuration, releaseName string, args upgradeArgs) (*release.Release, error)
	Uninstall(ctx context.Context, cfg *action.Configuration, name string) error
}

// helmInspector covers status / history / rollback / exists-probe.
// 4 methods (≤4 per ISP).
type helmInspector interface {
	Get(ctx context.Context, cfg *action.Configuration, name string) (*release.Release, error)
	History(ctx context.Context, cfg *action.Configuration, name string) ([]*release.Release, error)
	Rollback(ctx context.Context, cfg *action.Configuration, name string, revision int) error
	Exists(ctx context.Context, cfg *action.Configuration, name string) (bool, error)
}

// helmPuller covers chart pulls. Split from lifecycler so consumers that
// only need to pull (e.g. catalog inspection in P5-3) can depend on the
// smaller port.
type helmPuller interface {
	Pull(ctx context.Context, cfg *action.Configuration, ref, destDir string) (string, error)
}

// helmRunner is the composite the engine struct holds.
type helmRunner interface {
	helmLifecycler
	helmInspector
	helmPuller
}

// realRunner is the production adapter wrapping helm.sh/helm/v3/pkg/action
// directly. All branching (release-exists, retry, etc.) lives in engine.go.
type realRunner struct{}

func newRealRunner(_ *rest.Config) helmRunner {
	// rest.Config is currently unused at construction time but kept in the
	// signature so future runner state (cached registry client, etc.) can
	// take it without breaking call sites.
	return &realRunner{}
}

func (realRunner) Install(ctx context.Context, cfg *action.Configuration, args installArgs) (*release.Release, error) {
	c := action.NewInstall(cfg)
	c.Namespace = args.Namespace
	c.ReleaseName = args.ReleaseName
	c.Wait = args.Wait
	c.Timeout = args.Timeout
	c.CreateNamespace = args.CreateNamespace
	return c.RunWithContext(ctx, args.Chart, args.Values)
}

func (realRunner) Upgrade(ctx context.Context, cfg *action.Configuration, releaseName string, args upgradeArgs) (*release.Release, error) {
	c := action.NewUpgrade(cfg)
	c.Namespace = args.Namespace
	c.Wait = args.Wait
	c.Timeout = args.Timeout
	return c.RunWithContext(ctx, releaseName, args.Chart, args.Values)
}

func (realRunner) Uninstall(_ context.Context, cfg *action.Configuration, name string) error {
	_, err := action.NewUninstall(cfg).Run(name)
	return err
}

func (realRunner) Get(_ context.Context, cfg *action.Configuration, name string) (*release.Release, error) {
	return action.NewGet(cfg).Run(name)
}

func (realRunner) History(_ context.Context, cfg *action.Configuration, name string) ([]*release.Release, error) {
	return action.NewHistory(cfg).Run(name)
}

func (realRunner) Rollback(_ context.Context, cfg *action.Configuration, name string, revision int) error {
	c := action.NewRollback(cfg)
	c.Version = revision
	return c.Run(name)
}

func (realRunner) Exists(_ context.Context, cfg *action.Configuration, name string) (bool, error) {
	hist := action.NewHistory(cfg)
	hist.Max = 1
	releases, err := hist.Run(name)
	if err != nil {
		return false, err
	}
	return len(releases) > 0, nil
}

func (realRunner) Pull(_ context.Context, cfg *action.Configuration, ref, destDir string) (string, error) {
	regClient, err := registry.NewClient()
	if err != nil {
		return "", err
	}
	cfg.RegistryClient = regClient

	pull := action.NewPullWithOpts(action.WithConfig(cfg))
	pull.Settings = cli.New()
	pull.DestDir = destDir
	return pull.Run(ref)
}
