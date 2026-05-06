package controller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/SUSE/aif/pkg/helm"
)

const (
	uiPluginNamespace   = "cattle-ui-plugin-system"
	uiPluginReleaseName = "aif-ui"
)

// InstallAIExtensionReconciler reconciles an InstallAIExtension object
type InstallAIExtensionReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Logger     *slog.Logger
	HelmEngine helm.Engine
	Discovery  discovery.DiscoveryInterface
	Recorder   record.EventRecorder
}

// +kubebuilder:rbac:groups=ai.suse.com,resources=installaiextensions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.suse.com,resources=installaiextensions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=installaiextensions/finalizers,verbs=update
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=uiplugins,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

// Reconcile implements the reconcile loop for InstallAIExtension resources
func (r *InstallAIExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With(
		slog.String("namespace", req.Namespace),
		slog.String("name", req.Name),
	)

	// Fetch the InstallAIExtension CR
	var ext aifv1.InstallAIExtension
	if err := r.Get(ctx, req.NamespacedName, &ext); err != nil {
		if errors.IsNotFound(err) {
			// InstallAIExtension was deleted, nothing to do
			logger.Info("InstallAIExtension not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("failed to get InstallAIExtension", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !ext.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &ext)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&ext, finalizerName) {
		controllerutil.AddFinalizer(&ext, finalizerName)
		if err := r.Update(ctx, &ext); err != nil {
			logger.Error("failed to add finalizer", slog.Any("error", err))
			return ctrl.Result{}, err
		}
		// Return and requeue to continue reconciliation with finalizer present
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile the extension installation
	result, err := r.reconcile(ctx, &ext)
	if err != nil {
		logger.Error("reconciliation failed", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.Status().Update(ctx, &ext); err != nil {
		logger.Error("failed to update status", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	return result, nil
}

// reconcile performs the core reconciliation logic
func (r *InstallAIExtensionReconciler) reconcile(ctx context.Context, ext *aifv1.InstallAIExtension) (ctrl.Result, error) {
	logger := r.Logger.With(
		slog.String("namespace", ext.Namespace),
		slog.String("name", ext.Name),
	)

	// Set phase to Installing
	ext.Status.Phase = aifv1.InstallAIExtensionPhaseInstalling
	ext.Status.ObservedGeneration = ext.Generation

	// Step 1: Check UIPlugin CRD exists
	if err := r.checkUIPluginCRD(ctx, ext); err != nil {
		r.setCondition(ext, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonUIPluginCRDMissing,
			Message:            fmt.Sprintf("UIPlugin CRD check failed: %v", err),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		r.Recorder.Event(ext, "Warning", conditions.ReasonUIPluginCRDMissing, err.Error())
		return ctrl.Result{}, nil // Don't requeue - this is a permanent error
	}

	// Step 2: Install Helm chart
	if err := r.installChart(ctx, ext); err != nil {
		r.setCondition(ext, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonInstallFailed,
			Message:            fmt.Sprintf("Helm chart installation failed: %v", err),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		r.Recorder.Event(ext, "Warning", conditions.ReasonInstallFailed, err.Error())
		return ctrl.Result{}, err // Requeue to retry installation
	}

	// Step 3: Verify UIPlugin created
	if err := r.verifyUIPlugin(ctx, ext); err != nil {
		r.setCondition(ext, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonUIPluginNotCreated,
			Message:            fmt.Sprintf("UIPlugin verification pending: %v", err),
			ObservedGeneration: ext.Generation,
		})
		// Keep phase as Installing, not Failed - this is expected during async creation
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseInstalling
		r.Recorder.Event(ext, "Normal", conditions.ReasonUIPluginNotCreated, "Waiting for UIPlugin to be created")
		// Requeue after 5s to check again - don't use error (which triggers exponential backoff)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Success - set Ready=True
	r.setCondition(ext, metav1.Condition{
		Type:               conditions.TypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonInstalled,
		Message:            "Extension installed successfully",
		ObservedGeneration: ext.Generation,
	})
	ext.Status.Phase = aifv1.InstallAIExtensionPhaseInstalled
	r.Recorder.Event(ext, "Normal", conditions.ReasonInstalled, "AIExtension installed successfully")

	logger.Info("InstallAIExtension reconciled successfully")
	return ctrl.Result{}, nil
}

// handleDeletion handles InstallAIExtension deletion
func (r *InstallAIExtensionReconciler) handleDeletion(ctx context.Context, ext *aifv1.InstallAIExtension) (ctrl.Result, error) {
	logger := r.Logger.With(
		slog.String("namespace", ext.Namespace),
		slog.String("name", ext.Name),
	)

	if !controllerutil.ContainsFinalizer(ext, finalizerName) {
		// Finalizer already removed, nothing to do
		return ctrl.Result{}, nil
	}

	// Perform cleanup
	if err := r.cleanup(ctx, ext); err != nil {
		logger.Error("cleanup failed", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(ext, finalizerName)
	if err := r.Update(ctx, ext); err != nil {
		logger.Error("failed to remove finalizer", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	logger.Info("InstallAIExtension deleted successfully")
	return ctrl.Result{}, nil
}

// cleanup uninstalls the Helm release and performs cleanup
func (r *InstallAIExtensionReconciler) cleanup(ctx context.Context, ext *aifv1.InstallAIExtension) error {
	logger := r.Logger.With(
		slog.String("namespace", ext.Namespace),
		slog.String("name", ext.Name),
		slog.String("releaseName", uiPluginReleaseName),
	)

	logger.Info("uninstalling Helm release")

	// Uninstall the Helm release from the UI plugin namespace
	if err := r.HelmEngine.Uninstall(ctx, uiPluginNamespace, uiPluginReleaseName); err != nil {
		logger.Error("failed to uninstall Helm release", slog.Any("error", err))
		return fmt.Errorf("uninstall Helm release: %w", err)
	}

	logger.Info("Helm release uninstalled successfully")
	return nil
}

// checkUIPluginCRD checks if the UIPlugin CRD exists in the cluster
func (r *InstallAIExtensionReconciler) checkUIPluginCRD(ctx context.Context, ext *aifv1.InstallAIExtension) error {
	// Use discovery client to check for UIPlugin CRD
	// catalog.cattle.io/v1 is the group/version for Rancher UI plugins
	_, err := r.Discovery.ServerResourcesForGroupVersion("catalog.cattle.io/v1")
	if err != nil {
		r.Logger.Warn("UIPlugin CRD not found",
			slog.String("namespace", ext.Namespace),
			slog.String("name", ext.Name),
			slog.Any("error", err))

		return fmt.Errorf("UIPlugin CRD not found: %w", err)
	}

	r.Logger.Info("UIPlugin CRD found",
		slog.String("namespace", ext.Namespace),
		slog.String("name", ext.Name))

	return nil
}

// installChart installs the Helm chart for the UI extension
func (r *InstallAIExtensionReconciler) installChart(ctx context.Context, ext *aifv1.InstallAIExtension) error {
	// Build Helm install request from InstallAIExtension spec
	req := helm.InstallRequest{
		Namespace:   uiPluginNamespace,
		ReleaseName: uiPluginReleaseName,
		ChartRef:    ext.Spec.Helm.URL,
		Values:      make(map[string]any), // Empty values map
		Wait:        true,
		Timeout:     5 * time.Minute,
	}

	r.Logger.Info("Installing Helm chart",
		slog.String("namespace", ext.Namespace),
		slog.String("name", ext.Name),
		slog.String("chart", req.ChartRef),
		slog.String("release", req.ReleaseName))

	// Call Helm engine to install
	status, err := r.HelmEngine.InstallChartFromRepo(ctx, req)
	if err != nil {
		r.Logger.Error("Helm install failed",
			slog.String("namespace", ext.Namespace),
			slog.String("name", ext.Name),
			slog.Any("error", err))
		return fmt.Errorf("failed to install Helm chart: %w", err)
	}

	r.Logger.Info("Helm chart installed successfully",
		slog.String("namespace", ext.Namespace),
		slog.String("name", ext.Name),
		slog.String("release", status.Name),
		slog.Int("revision", status.Revision))

	return nil
}

// verifyUIPlugin verifies that the UIPlugin resource was created successfully.
// Returns error if UIPlugin not found (triggers requeue) or on fatal errors.
func (r *InstallAIExtensionReconciler) verifyUIPlugin(ctx context.Context, ext *aifv1.InstallAIExtension) error {
	// Create ObjectKey for Get operation
	key := client.ObjectKey{
		Namespace: uiPluginNamespace,
		Name:      uiPluginReleaseName,
	}

	// Get UIPlugin resource
	var plugin unstructured.Unstructured
	plugin.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "catalog.cattle.io",
		Version: "v1",
		Kind:    "UIPlugin",
	})

	err := r.Get(ctx, key, &plugin)
	if err == nil {
		// Success - UIPlugin found
		r.Logger.Info("UIPlugin verified",
			slog.String("namespace", ext.Namespace),
			slog.String("name", ext.Name),
			slog.String("uiplugin", uiPluginReleaseName))
		return nil
	}

	// Check if error is NotFound (will retry via requeue) vs other error (fatal)
	if !errors.IsNotFound(err) {
		// Fatal error - API server issue
		r.Logger.Error("failed to get UIPlugin",
			slog.String("namespace", ext.Namespace),
			slog.String("name", ext.Name),
			slog.Any("error", err))
		return fmt.Errorf("failed to get UIPlugin: %w", err)
	}

	// Not found - will be retried via controller requeue
	r.Logger.Debug("UIPlugin not yet created, will retry",
		slog.String("namespace", ext.Namespace),
		slog.String("name", ext.Name),
		slog.String("expected_uiplugin", uiPluginReleaseName))

	return fmt.Errorf("UIPlugin %s not yet created in namespace %s", uiPluginReleaseName, uiPluginNamespace)
}

// setCondition updates or appends a condition to the InstallAIExtension status
func (r *InstallAIExtensionReconciler) setCondition(ext *aifv1.InstallAIExtension, condition metav1.Condition) {
	condition.LastTransitionTime = metav1.Now()
	meta.SetStatusCondition(&ext.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager
func (r *InstallAIExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aifv1.InstallAIExtension{}).
		Complete(r)
}
