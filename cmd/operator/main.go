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
	"github.com/SUSE/aif/internal/controller"
	"github.com/SUSE/aif/internal/manager"
	"github.com/SUSE/aif/pkg/apps"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/git"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
	"github.com/SUSE/aif/pkg/publish"
	"github.com/SUSE/aif/pkg/workload"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
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
	appsCatalog := apps.New(logger, catalogRefreshDuration)
	bundleManager := bundle.New(logger)
	blueprintManager := blueprint.New(logger)
	publishWorkflow := publish.New(logger)
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

	// Setup controller-runtime manager
	logger.Info("Creating controller-runtime manager")
	webhookServer := webhook.NewServer(webhook.Options{
		Port: parsePort(webhookBindAddress),
	})
	mgr, err := ctrl.NewManager(k8sConfig, ctrl.Options{
		LeaderElection:         leaderElect,
		LeaderElectionID:       "aif-operator-leader",
		HealthProbeBindAddress: healthProbeBindAddress,
		Metrics: metricsserver.Options{
			BindAddress: metricsBindAddress,
		},
		WebhookServer: webhookServer,
	})
	if err != nil {
		logger.Error("Failed to create manager", "error", err)
		os.Exit(1)
	}
	logger.Info("Manager created successfully", "webhookPort", parsePort(webhookBindAddress))

	// Register API types with scheme
	if err := aifv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		logger.Error("Failed to add API types to scheme", "error", err)
		os.Exit(1)
	}

	// Setup WorkloadReconciler
	workloadReconciler := &controller.WorkloadReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("workload-controller"), //nolint:staticcheck // GetEventRecorderFor deprecated but required for record.EventRecorder interface
	}
	if err := workloadReconciler.SetupWithManager(mgr); err != nil {
		logger.Error("Failed to setup WorkloadReconciler", "error", err)
		os.Exit(1)
	}
	logger.Info("WorkloadReconciler registered")

	// Setup SettingsReconciler
	settingsReconciler := &controller.SettingsReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("settings-controller"), //nolint:staticcheck // GetEventRecorderFor deprecated but required for record.EventRecorder interface
	}
	if err := settingsReconciler.SetupWithManager(mgr); err != nil {
		logger.Error("Failed to setup SettingsReconciler", "error", err)
		os.Exit(1)
	}
	logger.Info("SettingsReconciler registered")

	// Setup BundleReconciler
	bundleReconciler := &controller.BundleReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("bundle-controller"), //nolint:staticcheck // GetEventRecorderFor deprecated but required for record.EventRecorder interface
		Manager:  bundleManager,
	}
	if err := bundleReconciler.SetupWithManager(mgr); err != nil {
		logger.Error("Failed to setup BundleReconciler", "error", err)
		os.Exit(1)
	}
	logger.Info("BundleReconciler registered")

	// Setup BlueprintReconciler
	blueprintReconciler := &controller.BlueprintReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("blueprint-controller"), //nolint:staticcheck // GetEventRecorderFor deprecated but required for record.EventRecorder interface
		Manager:  blueprintManager,
	}
	if err := blueprintReconciler.SetupWithManager(mgr); err != nil {
		logger.Error("Failed to setup BlueprintReconciler", "error", err)
		os.Exit(1)
	}
	logger.Info("BlueprintReconciler registered")

	// Setup webhooks
	if err := manager.SetupWebhooks(mgr); err != nil {
		logger.Error("Failed to setup webhooks", "error", err)
		os.Exit(1)
	}
	logger.Info("Webhooks registered")

	// Setup InstallAIExtensionReconciler
	installAIExtensionReconciler := &controller.InstallAIExtensionReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		Logger:     logger,
		HelmEngine: helmEngine,
		Discovery:  discoveryClient,
		Recorder:   mgr.GetEventRecorderFor("installaiextension-controller"), //nolint:staticcheck // GetEventRecorderFor deprecated but required for record.EventRecorder interface
	}
	if err := installAIExtensionReconciler.SetupWithManager(mgr); err != nil {
		logger.Error("Failed to setup InstallAIExtensionReconciler", "error", err)
		os.Exit(1)
	}
	logger.Info("InstallAIExtensionReconciler registered")

	// Add health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error("Failed to add healthz check", "error", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error("Failed to add readyz check", "error", err)
		os.Exit(1)
	}

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
