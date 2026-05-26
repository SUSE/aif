package manager

import (
	"fmt"
	"log/slog"

	"github.com/SUSE/aif/internal/controller"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/fleet"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
	"github.com/SUSE/aif/pkg/workload"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// Options configures the controller-runtime manager and its reconcilers.
type Options struct {
	LeaderElection   bool
	LeaderElectionID string
	MetricsAddr      string
	HealthAddr       string
	WebhookPort      int

	BlueprintManager blueprint.Manager
	HelmEngine       helm.Engine
	// HelmRenderer is the value-only rendering port used by the
	// workload deployer's Fleet path (helm.Engine and helm.ValueRenderer
	// are both satisfied by the same concrete *engine; main.go composes
	// them at wire time — see pkg/helm/interface.go ValueRenderer doc).
	HelmRenderer helm.ValueRenderer
	Discovery    discovery.DiscoveryInterface
	Logger       *slog.Logger

	// EngineBus pushes Settings snapshots to all settings-aware engines on
	// every reconcile. Constructed in cmd/operator/main.go via NewEngineBus
	// (P5-7).
	EngineBus controller.SettingsApplier

	// Engine ports needed to construct the production WorkloadDeployer
	// inside setupControllers (post-manager so repos use mgr.GetClient()).
	NvidiaDiscovery    nvidia.Discovery
	NvidiaDeployer     nvidia.Deployer
	FleetBundleEngine  fleet.FleetBundleEngine
	FleetGitRepoEngine fleet.FleetGitRepoEngine

	// OperatorNamespace is the namespace the operator runs in. The
	// WorkloadReconciler uses it to fetch the suse-registry-creds Secret
	// it then mirrors into each workload namespace (P4-3b).
	OperatorNamespace string
}

func (o Options) leaderElectionID() string {
	if o.LeaderElectionID != "" {
		return o.LeaderElectionID
	}
	return "aif-operator-leader"
}

func (o Options) webhookPort() int {
	if o.WebhookPort > 0 {
		return o.WebhookPort
	}
	return 9443
}

// NewManager creates a controller-runtime manager with all reconcilers,
// webhooks, and health checks registered.
func NewManager(scheme *runtime.Scheme, cfg *rest.Config, opts Options) (ctrlmanager.Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("rest.Config must not be nil")
	}
	if scheme == nil {
		return nil, fmt.Errorf("scheme must not be nil")
	}

	webhookServer := webhook.NewServer(webhook.Options{
		Port: opts.webhookPort(),
	})

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         opts.LeaderElection,
		LeaderElectionID:       opts.leaderElectionID(),
		HealthProbeBindAddress: opts.HealthAddr,
		Metrics: metricsserver.Options{
			BindAddress: opts.MetricsAddr,
		},
		WebhookServer: webhookServer,
	})
	if err != nil {
		return nil, fmt.Errorf("creating controller manager: %w", err)
	}

	if err := setupControllers(mgr, opts); err != nil {
		return nil, fmt.Errorf("setting up controllers: %w", err)
	}

	if err := SetupWebhooks(mgr); err != nil {
		return nil, fmt.Errorf("setting up webhooks: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("adding healthz check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("adding readyz check: %w", err)
	}

	return mgr, nil
}

func setupControllers(mgr ctrlmanager.Manager, opts Options) error {
	// Construct repos + Deployer post-manager so they use the cached client.
	// Repos are stateless adapter structs; constructing them here (for the
	// Deployer) and again in main.go (for publishWorkflow) is safe.
	blueprintRepo := blueprint.NewK8sRepository(mgr.GetClient())
	workloadDeployer := workload.NewDeployer(
		opts.Logger,
		opts.HelmRenderer, // P4-3b: value-only renderer for the Fleet path
		opts.FleetBundleEngine,
		opts.FleetGitRepoEngine,
		blueprintRepo,
		opts.NvidiaDiscovery,
		opts.NvidiaDeployer,
	)

	workloadReconciler := &controller.WorkloadReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorder("workload-controller"),
		Deployer:          workloadDeployer,
		Repository:        workload.NewK8sRepository(mgr.GetClient()).AsRepository(),
		OperatorNamespace: opts.OperatorNamespace,
	}
	if err := workloadReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up WorkloadReconciler: %w", err)
	}

	settingsReconciler := &controller.SettingsReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder("settings-controller"),
		Applier:  opts.EngineBus,
	}
	if err := settingsReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up SettingsReconciler: %w", err)
	}

	bundleReconciler := &controller.BundleReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder("bundle-controller"),
	}
	if err := bundleReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up BundleReconciler: %w", err)
	}

	blueprintReconciler := &controller.BlueprintReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder("blueprint-controller"),
		Manager:  opts.BlueprintManager,
	}
	if err := blueprintReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up BlueprintReconciler: %w", err)
	}

	installExtReconciler := &controller.InstallAIExtensionReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		Logger:     opts.Logger,
		HelmEngine: opts.HelmEngine,
		Discovery:  opts.Discovery,
		Recorder:   mgr.GetEventRecorder("installaiextension-controller"),
	}
	if err := installExtReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up InstallAIExtensionReconciler: %w", err)
	}

	return nil
}
