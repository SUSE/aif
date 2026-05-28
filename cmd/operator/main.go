package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	aifv1alpha1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/internal/api"
	"github.com/SUSE/aif/internal/manager"
	internalpublish "github.com/SUSE/aif/internal/publish"
	internalworkload "github.com/SUSE/aif/internal/workload"
	"github.com/SUSE/aif/pkg/apps"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/fleet"
	"github.com/SUSE/aif/pkg/git"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
	"github.com/SUSE/aif/pkg/publish"
	"github.com/SUSE/aif/pkg/source_collection"
	"github.com/SUSE/aif/pkg/workload"
	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	addr                   string
	healthProbeBindAddress string
	metricsBindAddress     string
	webhookBindAddress     string
	chartsDir              string
	gitDir                 string
	leaderElect            bool
	logLevel               string
	logFormat              string
	allowedOrigin          string
	catalogRefreshDuration time.Duration
)

func main() {
	// Parse flags
	flag.StringVar(&addr, "addr", ":8080", "API server bind address")
	flag.StringVar(&healthProbeBindAddress, "health-probe-bind-address", ":8081", "Health probe bind address")
	flag.StringVar(&metricsBindAddress, "metrics-bind-address", ":8082", "Metrics bind address")
	flag.StringVar(&webhookBindAddress, "webhook-bind-address", ":9443", "Webhook bind address")
	flag.StringVar(&chartsDir, "charts-dir", "/data/charts", "Writable directory for Helm chart downloads and repository cache")
	flag.StringVar(&gitDir, "git-dir", "/data/git", "Directory for Git operations")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&logFormat, "log-format", "json", "Log format (json, text)")
	flag.StringVar(&allowedOrigin, "allowed-origin", "", "CORS allowed origin")
	flag.DurationVar(&catalogRefreshDuration, "catalog-refresh", 5*time.Minute, "Catalog refresh interval")
	flag.Parse()

	// Setup logging
	logger := setupLogger(logLevel, logFormat)
	slog.SetDefault(logger)
	logger.Info("Starting AIF Operator",
		"addr", addr,
		"healthProbeBindAddress", healthProbeBindAddress,
		"metricsBindAddress", metricsBindAddress,
		"webhookBindAddress", webhookBindAddress,
		"chartsDir", chartsDir,
		"leaderElect", leaderElect,
		"logLevel", logLevel,
		"logFormat", logFormat,
	)

	// Get Kubernetes config
	k8sConfig := ctrl.GetConfigOrDie()

	// Create manager components
	helmEngine := helm.New(logger, k8sConfig, helm.WithChartDir(chartsDir))
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(k8sConfig)
	if err != nil {
		logger.Error("failed to create discovery client", slog.Any("error", err))
		os.Exit(1)
	}
	gitEngine := git.NewEngine(logger)
	nvidiaDiscovery, nvidiaAnnReader := nvidia.NewDiscovery(logger)
	nvidiaDeployer := nvidia.NewDeployer(logger)
	appcoClient, appcoAnnReader := source_collection.NewClient(logger)

	// Apps Catalog assembles entries from both upstream Sources via
	// thin adapters (P2-3 Option B hexagonal: source packages stay
	// unaware of pkg/apps; translation lives at the integration
	// boundary). AddSource is the bootstrap-only registry method
	// (decision d); appsCatalog.Start fans out to per-Source ticker
	// goroutines via the Lifecycle pattern (decision e).
	appsCatalog := apps.New(logger, catalogRefreshDuration)
	appsCatalog.AddSource(apps.NewNVIDIASource(nvidiaDiscovery, nvidiaAnnReader, logger, catalogRefreshDuration))
	appsCatalog.AddSource(apps.NewAppCoSource(appcoClient, appcoAnnReader, logger, catalogRefreshDuration))
	blueprintManager := blueprint.New(logger)
	// publish.Workflow takes Repository ports; the Repositories are constructed
	// after ctrl.NewManager below (they need the manager's client). Defer
	// construction until that's available.
	var publishWorkflow publish.Workflow

	// Scheme registration must precede client.New (for the Fleet engine's
	// non-cached client) and manager.NewManager. The same scheme instance
	// is shared by both — the standard Kubernetes types are needed for
	// SettingsReconciler's Secret reads, AIF CRDs for our own reconcilers,
	// and fleetv1 for the Fleet Bundle SSA path.
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		logger.Error("Failed to add built-in Kubernetes types to scheme", "error", err)
		os.Exit(1)
	}
	if err := aifv1alpha1.AddToScheme(scheme); err != nil {
		logger.Error("Failed to add AIF API types to scheme", "error", err)
		os.Exit(1)
	}
	if err := fleetv1.AddToScheme(scheme); err != nil {
		logger.Error("Failed to add Fleet API types to scheme", "error", err)
		os.Exit(1)
	}

	// Fleet engine uses a non-cached client: its Apply uses server-side-apply
	// (Patch with client.Apply — bypasses cache), Teardown uses Delete, and
	// the post-SSA Get for status read-back is acceptable without caching.
	// Building it here lets the bus and the manager Options share a single
	// FleetBundleEngine instance — needed so that when P5-7 populates
	// FleetSettings with downstream-cluster auth, the bus's UpdateSettings
	// push reaches the engine the deployer uses.
	fleetClient, err := client.New(k8sConfig, client.Options{Scheme: scheme})
	if err != nil {
		logger.Error("failed to create non-cached client for Fleet engine", "error", err)
		os.Exit(1)
	}
	fleetBundleEngine := fleet.NewBundleEngine(logger, fleetClient)
	fleetGitRepoEngine := fleet.NewGitRepoEngine(logger, fleetClient, gitEngine)

	// Bus that propagates Settings to all engines on every reconcile (P5-7).
	engineBus := manager.NewEngineBus(helmEngine, fleetBundleEngine, fleetGitRepoEngine, nvidiaDiscovery, nvidiaDeployer, appcoClient, logger)

	// Log manager types so vars stay "used" while their consumers (later
	// stories wire gitEngine, etc.) come online. Logging the values
	// directly (rather than `x != nil`) avoids staticcheck SA4023 on
	// always-true comparisons against constructor returns. engineBus
	// is consumed by manager.NewManager below; the others land with
	// their respective stories.
	logger.Debug("Managers created",
		"helmEngine", fmt.Sprintf("%T", helmEngine),
		"discoveryClient", fmt.Sprintf("%T", discoveryClient),
		"gitEngine", fmt.Sprintf("%T", gitEngine),
		"nvidiaDiscovery", fmt.Sprintf("%T", nvidiaDiscovery),
		"nvidiaDeployer", fmt.Sprintf("%T", nvidiaDeployer),
		"appsCatalog", fmt.Sprintf("%T", appsCatalog),
		"blueprintManager", fmt.Sprintf("%T", blueprintManager),
	)

	// Setup controller-runtime manager with all controllers and webhooks
	logger.Info("Creating controller-runtime manager")

	// helm.New returns the narrow Engine interface, but the underlying
	// *engine also satisfies helm.ValueRenderer (per ValueRenderer doc)
	// and helm.ChartInspector (per ChartInspector doc). Assert here to
	// expose all three ports without changing the constructor's return
	// type.
	helmRenderer, ok := helmEngine.(helm.ValueRenderer)
	if !ok {
		logger.Error("helm.Engine does not satisfy helm.ValueRenderer — unexpected helm package contract change")
		os.Exit(1)
	}
	helmInspector, ok := helmEngine.(helm.ChartInspector)
	if !ok {
		logger.Error("helm.Engine does not satisfy helm.ChartInspector — unexpected helm package contract change")
		os.Exit(1)
	}

	// OperatorNamespace is read from the downward-API POD_NAMESPACE env
	// var set by the operator Deployment; default to the chart's install
	// namespace when running out-of-cluster (make run).
	operatorNS := os.Getenv("POD_NAMESPACE")
	if operatorNS == "" {
		operatorNS = "aif"
	}

	mgr, err := manager.NewManager(scheme, k8sConfig, manager.Options{
		LeaderElection:     leaderElect,
		LeaderElectionID:   "aif-operator-leader",
		MetricsAddr:        metricsBindAddress,
		HealthAddr:         healthProbeBindAddress,
		WebhookPort:        parsePort(webhookBindAddress),
		BlueprintManager:   blueprintManager,
		HelmEngine:         helmEngine,
		HelmRenderer:       helmRenderer,
		FleetBundleEngine:  fleetBundleEngine,
		FleetGitRepoEngine: fleetGitRepoEngine,
		OperatorNamespace:  operatorNS,
		Discovery:          discoveryClient,
		Logger:             logger,
		EngineBus:          engineBus,
		NvidiaDiscovery:    nvidiaDiscovery,
		NvidiaDeployer:     nvidiaDeployer,
	})
	if err != nil {
		logger.Error("Failed to create manager", "error", err)
		os.Exit(1)
	}
	logger.Info("Manager created successfully")

	// Repos for the publish workflow use the cached client from mgr.GetClient()
	// so reads go through the informer cache rather than the API server.
	bundleRepo := bundle.NewK8sRepository(mgr.GetClient())
	blueprintRepo := blueprint.NewK8sRepository(mgr.GetClient())

	publishRecorder := internalpublish.NewEventRecorder(mgr.GetEventRecorder("publish-workflow"))

	publishWorkflow = publish.New(publish.Deps{
		Bundles:    bundleRepo,
		Blueprints: blueprintRepo,
		Authz:      publish.AllowAllAuthorizer{},
		Recorder:   publishRecorder,
		Logger:     logger,
	})
	// Loud, unmissable warning: AllowAllAuthorizer approves every publisher
	// action. Safe in dev (no REST handlers consume the Workflow yet), but the
	// moment a P3-x handler ships, this becomes a security hole unless the
	// authorizer is swapped. Logged at Warn so it surfaces in any log
	// aggregator that filters above Info, and so CI smoke tests can grep for
	// the message to ensure prod builds replace it.
	logger.Warn(
		"publish.Workflow wired with AllowAllAuthorizer — INSECURE, DO NOT DEPLOY TO PRODUCTION",
		"replacement", "pkg/authz with SubjectAccessReview-backed adapter (lands with P7-5)",
		"ready", publishWorkflow != nil,
	)

	publishHandler := api.NewPublishHandler(publishWorkflow, logger)
	settingsHandler := api.NewSettingsHandler(mgr.GetClient(), nil) // nil applier: engine propagation is async via SettingsReconciler

	// P5-3 Workload upgrade wiring. The upgrader depends on three narrow
	// consumer-defined ports — all aifv1-free. The internal/workload
	// adapters wrap pkg/{workload,blueprint}.Repository and translate
	// apierrors into pkg/workload sentinels at the K8s boundary.
	//   - upgradeStore:    workloadStore  (Get + PatchBlueprintVersion)
	//   - blueprintReader: blueprintReader (GetForUpgrade)
	//   - upgradeRecorder: UpgradeEventRecorder (k8s events adapter)
	// All reads go through mgr.GetClient() so they hit the informer cache.
	workloadK8sRepo := workload.NewK8sRepository(mgr.GetClient())
	workloadRepo := workloadK8sRepo.AsRepository()
	upgradeStore := internalworkload.NewUpgradeStore(workloadRepo)
	upgradeBlueprintReader := internalworkload.NewBlueprintReader(blueprintRepo)
	upgradeRecorder := internalworkload.NewEventRecorder(mgr.GetEventRecorder("workload-upgrader"))
	workloadUpgrader := workload.NewUpgrader(upgradeStore, upgradeBlueprintReader, upgradeRecorder, logger)
	// SAR-backed AuthChecker for the workload CRUD endpoints. Built once and
	// shared with the workloads handler; the underlying cache is goroutine-
	// safe (sync.Map).
	kubeClient, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		logger.Error("failed to create kubernetes client for SAR checker", slog.Any("error", err))
		os.Exit(1)
	}
	sarChecker := api.NewSARAuthChecker(kubeClient)
	// The same K8s adapter serves the upgrader workflow and the CRUD handler —
	// two narrow ports (workload.Reader + the consumer-defined mutator port in
	// internal/api), one backing struct.
	workloadsHandler := api.NewWorkloadsHandler(workloadUpgrader, workloadK8sRepo, workloadK8sRepo, sarChecker, logger)

	// AIDEV-NOTE(wave-1-task-2-1): Blueprint write handler. blueprintRepo is
	// the concrete *k8sRepository (NewK8sRepository returns the struct, not
	// the narrow Repository interface), so the handler's consumer-defined
	// port pulls Create/Delete/FindByLineageVersion/UpdateStatus directly.
	// The deployment counter comes from the workload K8s repo — DELETE
	// refuses to proceed while any Workload still references the blueprint.
	blueprintsHandler := api.NewBlueprintsHandler(blueprintRepo, workloadK8sRepo.AsDeploymentCounter(), sarChecker, logger)

	// Setup API server
	mux := http.NewServeMux()
	// Register the REST handlers via the api.Handler interface. Future
	// handlers plug in the same way — pass them as additional varargs.
	appsAPIHandler := api.NewAppsHandler(appsCatalog, helmInspector)
	nimHandler := api.NewNIMHandler(nvidiaDiscovery)
	handler := manager.Register(mux, logger, allowedOrigin, appsAPIHandler, nimHandler, publishHandler, settingsHandler, workloadsHandler, blueprintsHandler)

	apiServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start the Apps Catalog per-Source ticker goroutines (decision e:
	// each adapter owns its own ticker; Aggregator.Start fans out via
	// the optional Lifecycle interface). The goroutines exit when ctx
	// is canceled by SIGINT/SIGTERM.
	logger.Info("Starting Apps Catalog (per-Source tickers)")
	appsCatalog.Start(ctx)

	// Start API server
	go func() {
		logger.Info("Starting API server", "addr", addr)
		if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("API server failed", "error", err)
			cancel()
		}
	}()

	// Start controller-runtime manager (it will stop when ctx is cancelled)
	go func() {
		logger.Info("Starting controller-runtime manager")
		if err := mgr.Start(ctx); err != nil {
			logger.Error("Manager failed", "error", err)
			cancel()
		}
		logger.Info("Manager stopped")
	}()

	// Wait for shutdown signal
	logger.Info("Waiting for shutdown signal...")
	<-ctx.Done()
	logger.Info("Shutting down gracefully", "reason", ctx.Err())

	// Shutdown with 15s timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	// Shutdown API server
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		if err := apiServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("API server shutdown error", "error", err)
		}
	}()

	// Wait for shutdown or timeout
	select {
	case <-shutdownDone:
		logger.Info("Shutdown complete")
	case <-shutdownCtx.Done():
		logger.Warn("Shutdown timeout exceeded")
	}
}

func setupLogger(level, format string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: logLevel}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

func parsePort(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		slog.Warn("Failed to parse port from address, using default", "addr", addr, "default", 9443)
		return 9443
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		return 9443
	}
	return port
}
