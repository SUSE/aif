// pkg/helm/engine.go
package helm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const defaultInstallTimeout = 5 * time.Minute

// InstallChartFromRepo installs a chart pulled from an OCI repo. Idempotent:
// if a release with the same name exists, performs an upgrade.
func (e *engine) InstallChartFromRepo(ctx context.Context, req InstallRequest) (ReleaseStatus, error) {
	e.logger.Info("installing chart from OCI repo",
		slog.String("component", "helm.engine"),
		slog.String("namespace", req.Namespace),
		slog.String("release", req.ReleaseName),
		slog.String("chart_ref", req.ChartRef))

	cfg, err := e.cfgFactory(req.Namespace)
	if err != nil {
		return ReleaseStatus{}, fmt.Errorf("failed to create action config: %w", err)
	}

	exists, err := e.runner.Exists(ctx, cfg, req.ReleaseName)
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return ReleaseStatus{}, fmt.Errorf("failed to check release history: %w", err)
	}

	chartPath, err := e.runner.Pull(ctx, cfg, req.ChartRef, e.chartDir)
	if err != nil {
		return ReleaseStatus{}, fmt.Errorf("%w: %s: %v", ErrPullFailed, req.ChartRef, err)
	}
	defer os.RemoveAll(chartPath)

	ch, err := loader.Load(chartPath)
	if err != nil {
		return ReleaseStatus{}, fmt.Errorf("failed to load chart: %w", err)
	}

	timeout := req.Timeout
	if timeout == 0 {
		timeout = defaultInstallTimeout
	}

	if exists {
		rel, err := e.runner.Upgrade(ctx, cfg, req.ReleaseName, upgradeArgs{
			Namespace: req.Namespace,
			Chart:     ch,
			Values:    req.Values,
			Wait:      req.Wait,
			Timeout:   timeout,
		})
		if err != nil {
			return ReleaseStatus{}, fmt.Errorf("helm upgrade failed: %w", err)
		}
		return toReleaseStatus(rel), nil
	}

	rel, err := e.runner.Install(ctx, cfg, installArgs{
		Namespace:       req.Namespace,
		ReleaseName:     req.ReleaseName,
		Chart:           ch,
		Values:          req.Values,
		Wait:            req.Wait,
		Timeout:         timeout,
		CreateNamespace: true,
	})
	if err != nil {
		return ReleaseStatus{}, fmt.Errorf("helm install failed: %w", err)
	}
	return toReleaseStatus(rel), nil
}

// Uninstall removes a release. Returns nil if the release doesn't exist.
func (e *engine) Uninstall(ctx context.Context, namespace, releaseName string) error {
	e.logger.Info("uninstalling release",
		slog.String("component", "helm.engine"),
		slog.String("namespace", namespace),
		slog.String("release", releaseName))

	cfg, err := e.cfgFactory(namespace)
	if err != nil {
		return fmt.Errorf("failed to create action config: %w", err)
	}

	if err := e.runner.Uninstall(ctx, cfg, releaseName); err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil
		}
		return fmt.Errorf("helm uninstall failed: %w", err)
	}
	return nil
}

// Status returns the current Helm release status. Returns
// ErrReleaseNotFound if the release does not exist.
func (e *engine) Status(ctx context.Context, namespace, releaseName string) (ReleaseStatus, error) {
	cfg, err := e.cfgFactory(namespace)
	if err != nil {
		return ReleaseStatus{}, fmt.Errorf("failed to create action config: %w", err)
	}
	rel, err := e.runner.Get(ctx, cfg, releaseName)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return ReleaseStatus{}, ErrReleaseNotFound
		}
		return ReleaseStatus{}, fmt.Errorf("helm get failed: %w", err)
	}
	return toReleaseStatus(rel), nil
}

// Rollback rolls back to a specific revision (per §4.4 Recovery procedure).
func (e *engine) Rollback(ctx context.Context, namespace, releaseName string, revision int) error {
	cfg, err := e.cfgFactory(namespace)
	if err != nil {
		return fmt.Errorf("failed to create action config: %w", err)
	}
	if err := e.runner.Rollback(ctx, cfg, releaseName, revision); err != nil {
		return fmt.Errorf("helm rollback failed: %w", err)
	}
	return nil
}

// History returns release revision history sorted newest-first.
func (e *engine) History(ctx context.Context, namespace, releaseName string) ([]RevisionInfo, error) {
	cfg, err := e.cfgFactory(namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create action config: %w", err)
	}
	rels, err := e.runner.History(ctx, cfg, releaseName)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, ErrReleaseNotFound
		}
		return nil, fmt.Errorf("helm history failed: %w", err)
	}
	out := make([]RevisionInfo, 0, len(rels))
	for _, r := range rels {
		if r == nil || r.Info == nil {
			continue
		}
		out = append(out, RevisionInfo{
			Revision:    r.Version,
			Updated:     r.Info.LastDeployed.Time,
			Status:      r.Info.Status.String(),
			Description: r.Info.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// UpdateSettings is the SOLE writer of e.settings per §8.2.1. Takes a write
// lock around the swap; callers reading via snapshot() take a read lock.
func (e *engine) UpdateSettings(s EngineSettings) {
	e.mu.Lock()
	e.settings = s
	e.mu.Unlock()

	e.logger.Info("updated engine settings",
		slog.String("component", "helm.engine"),
		slog.String("suse_registry", s.RegistryEndpoints.SUSERegistry),
		slog.Bool("image_rewrite_enabled", s.ImageRewrite.Enabled))
}

func toReleaseStatus(rel *release.Release) ReleaseStatus {
	if rel == nil || rel.Info == nil {
		return ReleaseStatus{}
	}
	return ReleaseStatus{
		Name:     rel.Name,
		Revision: rel.Version,
		Status:   rel.Info.Status.String(),
		Updated:  rel.Info.LastDeployed.Time,
	}
}

// newActionConfig creates a Helm action configuration for the given namespace.
func (e *engine) newActionConfig(namespace string) (*action.Configuration, error) {
	cfg := new(action.Configuration)
	if err := cfg.Init(
		&restClientGetter{config: e.config, namespace: namespace},
		namespace,
		"secrets",
		func(format string, v ...interface{}) {
			e.logger.Debug(fmt.Sprintf(format, v...),
				slog.String("component", "helm.engine"),
				slog.String("namespace", namespace))
		},
	); err != nil {
		return nil, fmt.Errorf("failed to initialize action config: %w", err)
	}
	return cfg, nil
}

// restClientGetter implements genericclioptions.RESTClientGetter for Helm.
type restClientGetter struct {
	config    *rest.Config
	namespace string
}

func (r *restClientGetter) ToRESTConfig() (*rest.Config, error) {
	return r.config, nil
}

func (r *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(r.config)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(dc), nil
}

func (r *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	dc, err := r.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(dc), nil
}

func (r *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return &simpleClientConfig{config: r.config, namespace: r.namespace}
}

type simpleClientConfig struct {
	config    *rest.Config
	namespace string
}

func (s *simpleClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, errors.New("raw config not available")
}
func (s *simpleClientConfig) ClientConfig() (*rest.Config, error) { return s.config, nil }
func (s *simpleClientConfig) Namespace() (string, bool, error)    { return s.namespace, false, nil }
func (s *simpleClientConfig) ConfigAccess() clientcmd.ConfigAccess { return nil }
