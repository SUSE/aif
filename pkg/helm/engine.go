package helm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// InstallChartFromRepo installs a chart pulled from an OCI repo. Idempotent: if a
// release with the same name exists, performs an upgrade instead.
func (e *engine) InstallChartFromRepo(ctx context.Context, req InstallRequest) (ReleaseStatus, error) {
	e.logger.Info("installing chart from OCI repo",
		slog.String("component", "helm.engine"),
		slog.String("namespace", req.Namespace),
		slog.String("release", req.ReleaseName),
		slog.String("chart_ref", req.ChartRef))

	// Create action configuration for the target namespace
	actionConfig, err := e.newActionConfig(req.Namespace)
	if err != nil {
		e.logger.Error("failed to create action config",
			slog.String("component", "helm.engine"),
			slog.String("namespace", req.Namespace),
			slog.Any("error", err))
		return ReleaseStatus{}, fmt.Errorf("failed to create action config: %w", err)
	}

	// Check if release already exists
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	releases, err := histClient.Run(req.ReleaseName)
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		e.logger.Error("failed to check release history",
			slog.String("component", "helm.engine"),
			slog.String("namespace", req.Namespace),
			slog.String("release", req.ReleaseName),
			slog.Any("error", err))
		return ReleaseStatus{}, fmt.Errorf("failed to check release history: %w", err)
	}

	// If release exists, perform upgrade instead
	if len(releases) > 0 {
		e.logger.Info("release exists, performing upgrade",
			slog.String("component", "helm.engine"),
			slog.String("namespace", req.Namespace),
			slog.String("release", req.ReleaseName))
		return e.upgradeChart(ctx, actionConfig, req)
	}

	// Pull and load the OCI chart
	chartPath, err := e.pullChart(ctx, req.ChartRef, req.Namespace)
	if err != nil {
		e.logger.Error("failed to pull chart",
			slog.String("component", "helm.engine"),
			slog.String("namespace", req.Namespace),
			slog.String("chart_ref", req.ChartRef),
			slog.Any("error", err))
		return ReleaseStatus{}, fmt.Errorf("failed to pull chart: %w", err)
	}

	chart, err := loader.Load(chartPath)
	if err != nil {
		e.logger.Error("failed to load chart",
			slog.String("component", "helm.engine"),
			slog.String("namespace", req.Namespace),
			slog.String("chart_path", chartPath),
			slog.Any("error", err))
		return ReleaseStatus{}, fmt.Errorf("failed to load chart: %w", err)
	}

	// Create install action
	installClient := action.NewInstall(actionConfig)
	installClient.Namespace = req.Namespace
	installClient.ReleaseName = req.ReleaseName
	installClient.Wait = req.Wait
	installClient.Timeout = req.Timeout
	if installClient.Timeout == 0 {
		installClient.Timeout = 5 * time.Minute // default timeout
	}

	// Execute install
	rel, err := installClient.RunWithContext(ctx, chart, req.Values)
	if err != nil {
		e.logger.Error("helm install failed",
			slog.String("component", "helm.engine"),
			slog.String("namespace", req.Namespace),
			slog.String("release", req.ReleaseName),
			slog.Any("error", err))
		return ReleaseStatus{}, fmt.Errorf("helm install failed: %w", err)
	}

	e.logger.Info("chart installed successfully",
		slog.String("component", "helm.engine"),
		slog.String("namespace", req.Namespace),
		slog.String("release", req.ReleaseName),
		slog.Int("revision", rel.Version))

	return ReleaseStatus{
		Name:     rel.Name,
		Revision: rel.Version,
		Status:   rel.Info.Status.String(),
		Updated:  rel.Info.LastDeployed.Time,
	}, nil
}

// upgradeChart performs an upgrade when the release already exists
func (e *engine) upgradeChart(ctx context.Context, actionConfig *action.Configuration, req InstallRequest) (ReleaseStatus, error) {
	// Pull and load the OCI chart
	chartPath, err := e.pullChart(ctx, req.ChartRef, req.Namespace)
	if err != nil {
		e.logger.Error("failed to pull chart",
			slog.String("component", "helm.engine"),
			slog.String("namespace", req.Namespace),
			slog.String("chart_ref", req.ChartRef),
			slog.Any("error", err))
		return ReleaseStatus{}, fmt.Errorf("failed to pull chart: %w", err)
	}

	chart, err := loader.Load(chartPath)
	if err != nil {
		e.logger.Error("failed to load chart",
			slog.String("component", "helm.engine"),
			slog.String("namespace", req.Namespace),
			slog.String("chart_path", chartPath),
			slog.Any("error", err))
		return ReleaseStatus{}, fmt.Errorf("failed to load chart: %w", err)
	}

	// Create upgrade action
	upgradeClient := action.NewUpgrade(actionConfig)
	upgradeClient.Namespace = req.Namespace
	upgradeClient.Wait = req.Wait
	upgradeClient.Timeout = req.Timeout
	if upgradeClient.Timeout == 0 {
		upgradeClient.Timeout = 5 * time.Minute // default timeout
	}

	// Execute upgrade
	rel, err := upgradeClient.RunWithContext(ctx, req.ReleaseName, chart, req.Values)
	if err != nil {
		e.logger.Error("helm upgrade failed",
			slog.String("component", "helm.engine"),
			slog.String("namespace", req.Namespace),
			slog.String("release", req.ReleaseName),
			slog.Any("error", err))
		return ReleaseStatus{}, fmt.Errorf("helm upgrade failed: %w", err)
	}

	e.logger.Info("chart upgraded successfully",
		slog.String("component", "helm.engine"),
		slog.String("namespace", req.Namespace),
		slog.String("release", req.ReleaseName),
		slog.Int("revision", rel.Version))

	return ReleaseStatus{
		Name:     rel.Name,
		Revision: rel.Version,
		Status:   rel.Info.Status.String(),
		Updated:  rel.Info.LastDeployed.Time,
	}, nil
}

// pullChart pulls an OCI chart to a temporary location and returns the path.
// Cleanup of downloaded chart files is the caller's responsibility.
func (e *engine) pullChart(ctx context.Context, chartRef string, namespace string) (string, error) {
	// Create action configuration for the namespace
	actionConfig, err := e.newActionConfig(namespace)
	if err != nil {
		e.logger.Error("failed to create action config",
			slog.String("component", "helm.engine"),
			slog.String("namespace", namespace),
			slog.Any("error", err))
		return "", fmt.Errorf("failed to create action config: %w", err)
	}

	// Create registry client
	// TODO(P5-7): Configure registry auth from e.settings.RegistryEndpoints.SUSERegistry
	// when Settings reconciler integration is added
	registryClient, err := registry.NewClient()
	if err != nil {
		e.logger.Error("failed to create registry client",
			slog.String("component", "helm.engine"),
			slog.String("namespace", namespace),
			slog.Any("error", err))
		return "", fmt.Errorf("failed to create registry client: %w", err)
	}

	// Configure action with registry client
	actionConfig.RegistryClient = registryClient

	// Create pull action
	pullClient := action.NewPullWithOpts(action.WithConfig(actionConfig))
	pullClient.Settings = cli.New()

	// Pull the chart
	output, err := pullClient.Run(chartRef)
	if err != nil {
		e.logger.Error("failed to pull chart",
			slog.String("component", "helm.engine"),
			slog.String("namespace", namespace),
			slog.String("chart_ref", chartRef),
			slog.Any("error", err))
		return "", fmt.Errorf("failed to pull chart %s: %w", chartRef, err)
	}

	return output, nil
}

// Uninstall removes a release. Returns nil if the release doesn't exist.
func (e *engine) Uninstall(ctx context.Context, namespace, releaseName string) error {
	e.logger.Info("uninstalling release",
		slog.String("component", "helm.engine"),
		slog.String("namespace", namespace),
		slog.String("release", releaseName))

	// Create action configuration for the target namespace
	actionConfig, err := e.newActionConfig(namespace)
	if err != nil {
		e.logger.Error("failed to create action config",
			slog.String("component", "helm.engine"),
			slog.String("namespace", namespace),
			slog.Any("error", err))
		return fmt.Errorf("failed to create action config: %w", err)
	}

	// Create uninstall action
	uninstallClient := action.NewUninstall(actionConfig)

	// Execute uninstall
	_, err = uninstallClient.Run(releaseName)
	if err != nil {
		// Treat "not found" as success (idempotent)
		if errors.Is(err, driver.ErrReleaseNotFound) {
			e.logger.Info("release not found, treating as success",
				slog.String("component", "helm.engine"),
				slog.String("namespace", namespace),
				slog.String("release", releaseName))
			return nil
		}
		e.logger.Error("helm uninstall failed",
			slog.String("component", "helm.engine"),
			slog.String("namespace", namespace),
			slog.String("release", releaseName),
			slog.Any("error", err))
		return fmt.Errorf("helm uninstall failed: %w", err)
	}

	e.logger.Info("release uninstalled successfully",
		slog.String("component", "helm.engine"),
		slog.String("namespace", namespace),
		slog.String("release", releaseName))

	return nil
}

// Status returns the current Helm release status.
func (e *engine) Status(ctx context.Context, namespace, releaseName string) (ReleaseStatus, error) {
	return ReleaseStatus{}, errors.New("not implemented")
}

// Rollback rolls back to a specific revision (per §4.4 Recovery procedure).
func (e *engine) Rollback(ctx context.Context, namespace, releaseName string, revision int) error {
	return errors.New("not implemented")
}

// History returns release revision history (newest first).
func (e *engine) History(ctx context.Context, namespace, releaseName string) ([]RevisionInfo, error) {
	return nil, errors.New("not implemented")
}

// UpdateSettings is called by SettingsReconciler.applySettingsToEngines to push
// the latest registry endpoints + image-rewrite rules. Engine holds no Settings
// reference; the reconciler pushes scalars on every reconcile (per §4.5 defaults
// policy + §8.2.1 settings propagation pattern).
func (e *engine) UpdateSettings(s EngineSettings) {
	e.settings = s
	e.logger.Info("updated engine settings",
		slog.String("component", "helm.engine"),
		slog.String("suse_registry", s.RegistryEndpoints.SUSERegistry),
		slog.Bool("image_rewrite_enabled", s.ImageRewrite.Enabled))
}

// newActionConfig creates a Helm action configuration for the given namespace
func (e *engine) newActionConfig(namespace string) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)

	// Initialize with Kubernetes REST config
	// Use the "secrets" storage driver (standard Helm 3 behavior)
	if err := actionConfig.Init(
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

	return actionConfig, nil
}

// restClientGetter implements the genericclioptions.RESTClientGetter interface required by Helm
type restClientGetter struct {
	config    *rest.Config
	namespace string
}

func (r *restClientGetter) ToRESTConfig() (*rest.Config, error) {
	return r.config, nil
}

func (r *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.config)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(discoveryClient), nil
}

func (r *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := r.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	return mapper, nil
}

func (r *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return &simpleClientConfig{config: r.config, namespace: r.namespace}
}

// simpleClientConfig implements clientcmd.ClientConfig interface
type simpleClientConfig struct {
	config    *rest.Config
	namespace string
}

func (s *simpleClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, errors.New("raw config not available")
}

func (s *simpleClientConfig) ClientConfig() (*rest.Config, error) {
	return s.config, nil
}

func (s *simpleClientConfig) Namespace() (string, bool, error) {
	return s.namespace, false, nil
}

func (s *simpleClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return nil
}
