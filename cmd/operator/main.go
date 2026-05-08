package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	aifv1alpha1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/internal/manager"
	"github.com/SUSE/aif/pkg/apps"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/git"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
	"github.com/SUSE/aif/pkg/publish"
	"github.com/SUSE/aif/pkg/source_collection"
	"github.com/SUSE/aif/pkg/workload"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
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
	flag.StringVar(&chartsDir, "charts-dir", "/charts", "Directory containing Helm charts")
	flag.StringVar(&gitDir, "git-dir", "/git", "Directory for Git operations")
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
		"gitDir", gitDir,
		"leaderElect", leaderElect,
		"logLevel", logLevel,
		"logFormat", logFormat,
	)

	// Get Kubernetes config
	k8sConfig := ctrl.GetConfigOrDie()

	// Create manager components
	helmEngine := helm.New(logger, k8sConfig)
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(k8sConfig)
	if err != nil {
		logger.Error("failed to create discovery client", slog.Any("error", err))
		os.Exit(1)
	}
	gitEngine := git.NewFleetEngine(logger, gitDir)
	nvidiaDiscovery := nvidia.NewDiscovery(logger)
	nvidiaDeployer := nvidia.NewDeployer(logger)
	appcoClient := source_collection.NewClient(logger)

	// Apps Catalog assembles entries from both upstream Sources via
	// thin adapters (P2-3 Option B hexagonal: source packages stay
	// unaware of pkg/apps; translation lives at the integration
	// boundary). AddSource is the bootstrap-only registry method
	// (decision d); appsCatalog.Start fans out to per-Source ticker
	// goroutines via the Lifecycle pattern (decision e).
	appsCatalog := apps.New(logger, catalogRefreshDuration)
	appsCatalog.AddSource(apps.NewNVIDIASource(nvidiaDiscovery, logger, catalogRefreshDuration))
	appsCatalog.AddSource(apps.NewAppCoSource(appcoClient, logger, catalogRefreshDuration))
	// Note: appsCatalog.Start(ctx) is invoked alongside the
	// controller-runtime manager start, below, so it shares the
	// signal-driven cancellation lifecycle.

	bundleManager := bundle.New(logger)
	blueprintManager := blueprint.New(logger)
	// publish.Workflow takes Repository ports; the Repositories are constructed
	// after ctrl.NewManager below (they need the manager's client). Defer
	// construction until that's available.
	var publishWorkflow publish.Workflow
	workloadManager := workload.New(logger)

	// Log manager creation (prevent unused variable warnings)
	logger.Debug("Managers created",
		"helmEngine", helmEngine != nil,
		"discoveryClient", discoveryClient != nil,
		"gitEngine", gitEngine != nil,
		"nvidiaDiscovery", nvidiaDiscovery != nil,
		"nvidiaDeployer", nvidiaDeployer != nil,
		"appsCatalog", appsCatalog != nil,
		"bundleManager", bundleManager != nil,
		"blueprintManager", blueprintManager != nil,
		"publishWorkflow", publishWorkflow != nil,
		"workloadManager", workloadManager != nil,
	)

	// Setup controller-runtime manager with all controllers and webhooks
	logger.Info("Creating controller-runtime manager")
	scheme := runtime.NewScheme()

	// Register the standard Kubernetes built-in types (corev1, appsv1, batchv1,
	// rbacv1, networkingv1, …). Without this, controller-runtime's typed client
	// cannot Get/List/Watch any non-CRD object — most concretely, the
	// SettingsReconciler's r.Get(ctx, secretName, &corev1.Secret{}) fails with
	// "no kind is registered for the type v1.Secret in scheme".
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		logger.Error("Failed to add built-in Kubernetes types to scheme", "error", err)
		os.Exit(1)
	}
	// Register the AIF CRDs.
	if err := aifv1alpha1.AddToScheme(scheme); err != nil {
		logger.Error("Failed to add AIF API types to scheme", "error", err)
		os.Exit(1)
	}

	mgr, err := manager.NewManager(scheme, k8sConfig, manager.Options{
		LeaderElection:   leaderElect,
		LeaderElectionID: "aif-operator-leader",
		MetricsAddr:      metricsBindAddress,
		HealthAddr:       healthProbeBindAddress,
		WebhookPort:      parsePort(webhookBindAddress),
		BundleManager:    bundleManager,
		BlueprintManager: blueprintManager,
		HelmEngine:       helmEngine,
		Discovery:        discoveryClient,
		Logger:           logger,
	})
	if err != nil {
		logger.Error("Failed to create manager", "error", err)
		os.Exit(1)
	}
	logger.Info("Manager created successfully")

	// Construct the publish.Workflow now that the controller-runtime client
	// is available (Repositories need it). The workflow has no consumer yet —
	// REST handlers in P3-x will pick it up via manager.Register. AllowAllAuthorizer
	// is a stub until pkg/authz lands a SubjectAccessReview-backed impl.
	publishWorkflow = publish.New(publish.Deps{
		Bundles:    bundle.NewK8sRepository(mgr.GetClient()),
		Blueprints: blueprint.NewK8sRepository(mgr.GetClient()),
		Authz:      publish.AllowAllAuthorizer{},
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

	// Setup API server
	mux := http.NewServeMux()
	manager.Register(mux, logger, allowedOrigin)

	apiServer := &http.Server{
		Addr:    addr,
		Handler: mux,
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
