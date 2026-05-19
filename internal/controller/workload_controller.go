package controller

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/SUSE/aif/pkg/workload"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	workloadFinalizerName = "ai.suse.com/cleanup"
)

// WorkloadReconciler reconciles a Workload object
type WorkloadReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
	Deployer workload.Deployer // P4-2: Helm deployment engine
}

// +kubebuilder:rbac:groups=ai.suse.com,resources=workloads,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=workloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=workloads/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconcile loop for Workload resources
func (r *WorkloadReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Workload
	var w aifv1.Workload
	if err := r.Get(ctx, req.NamespacedName, &w); err != nil {
		if errors.IsNotFound(err) {
			// Workload was deleted, nothing to do
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get Workload")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !w.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &w)
	}

	// Add finalizer if missing
	if !controllerutil.ContainsFinalizer(&w, workloadFinalizerName) {
		controllerutil.AddFinalizer(&w, workloadFinalizerName)
		if err := r.Update(ctx, &w); err != nil {
			logger.Error(err, "failed to add finalizer")
			return ctrl.Result{}, err
		}
		// Return and requeue to continue reconciliation with finalizer present
		return ctrl.Result{Requeue: true}, nil
	}

	// Main reconciliation
	deployErr := r.reconcile(ctx, &w)

	// Update status
	if err := r.Status().Update(ctx, &w); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	// Calculate requeue interval from deployer error
	var requeueAfter time.Duration
	if deployErr != nil {
		_, requeueAfter, _ = mapDeployError(deployErr)
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// validateSource validates the Workload source discriminated union
func (r *WorkloadReconciler) validateSource(w *aifv1.Workload) error {
	switch w.Spec.Source.Kind {
	case aifv1.WorkloadSourceKindApp:
		if w.Spec.Source.App == nil {
			return fmt.Errorf("source.kind=App requires source.app field")
		}
		return nil

	case aifv1.WorkloadSourceKindBlueprint:
		if w.Spec.Source.Blueprint == nil {
			return fmt.Errorf("source.kind=Blueprint requires source.blueprint field")
		}
		return nil

	case aifv1.WorkloadSourceKindBundleTest:
		if w.Spec.Source.BundleTest == nil {
			return fmt.Errorf("source.kind=BundleTest requires source.bundleTest field")
		}
		return nil

	default:
		return fmt.Errorf("invalid source.kind: %s", w.Spec.Source.Kind)
	}
}

// mapDeployError translates a Deployer error into (reason, requeueAfter,
// terminal) per P4-2 design spec §6.3 (docs/superpowers/specs/2026-05-15-p4-2-workload-deployer-design.md).
// Returns ("", 0, false) for nil errors — caller handles the success path separately.
func mapDeployError(err error) (reason string, requeueAfter time.Duration, terminal bool) {
	switch {
	case err == nil:
		return "", 0, false
	case stderrors.Is(err, workload.ErrNestedBlueprintNotSupported):
		return conditions.ReasonUnsupportedComposition, 0, true
	case stderrors.Is(err, workload.ErrSourceNotResolved):
		return conditions.ReasonSourceNotResolved, 30 * time.Second, false
	case stderrors.Is(err, workload.ErrComponentInstallFailed):
		return conditions.ReasonComponentInstallFailed, 30 * time.Second, false
	case stderrors.Is(err, workload.ErrComponentUninstallFailed):
		return conditions.ReasonOrphanCleanupPending, 30 * time.Second, false
	default:
		return conditions.ReasonReconcileFailed, 30 * time.Second, false
	}
}

// reconcile performs the core reconciliation logic
func (r *WorkloadReconciler) reconcile(ctx context.Context, w *aifv1.Workload) error {
	logger := log.FromContext(ctx)

	// Validate source
	if err := r.validateSource(w); err != nil {
		logger.Info("Workload validation failed", "error", err)

		// Set Ready=False
		r.setCondition(w, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonInvalidSpec,
			Message:            fmt.Sprintf("Validation failed: %v", err),
			ObservedGeneration: w.Generation,
		})

		// Record event
		r.Recorder.Eventf(w, nil, "Warning", "WorkloadInvalid", conditions.ActionValidating, err.Error())

		// Set ObservedGeneration
		w.Status.ObservedGeneration = w.Generation

		return nil // Don't return error - status has been updated
	}

	// Set phase to Pending if not already set
	if w.Status.Phase == "" {
		w.Status.Phase = aifv1.WorkloadPhasePending
	}

	// P4-2: Translate to DeployRequest, call Deployer, apply result.
	priorPhase := w.Status.Phase
	req := workload.WorkloadToDeployRequest(w)
	result, deployErr := r.Deployer.Deploy(ctx, req)
	workload.ApplyDeployResult(w, result)

	// Phase-preservation invariant (P4-2 design spec §6.3): un-classified
	// errors must not lower the user-visible phase. The mapped error path for
	// known sentinels (UnsupportedComposition, SourceNotResolved, etc.)
	// intentionally sets a specific Phase via the deployer's DeployResult; only
	// the catch-all branch needs preservation.
	if deployErr != nil {
		reason, _, _ := mapDeployError(deployErr)
		if reason == conditions.ReasonReconcileFailed && w.Status.Phase == "" {
			w.Status.Phase = priorPhase
		}
	}

	// P4-2 design spec §6.4 events
	r.emitDeployEvents(w, priorPhase, result, deployErr)

	// Map deployer error/result to Ready condition per P4-2 design spec §6.3
	ready := metav1.Condition{
		Type:               conditions.TypeReady,
		ObservedGeneration: w.Generation,
	}
	if deployErr == nil {
		if result.Phase == workload.PhaseRunning {
			ready.Status = metav1.ConditionTrue
			ready.Reason = conditions.ReasonInstalled
			ready.Message = "All components deployed"
		} else {
			ready.Status = metav1.ConditionFalse
			ready.Reason = conditions.ReasonInstalling
			ready.Message = fmt.Sprintf("Workload phase %q", result.Phase)
		}
	} else {
		reason, _, _ := mapDeployError(deployErr)
		ready.Status = metav1.ConditionFalse
		ready.Reason = reason
		ready.Message = deployErr.Error()
	}
	r.setCondition(w, ready)

	// Record success event
	if deployErr == nil {
		r.Recorder.Eventf(w, nil, "Normal", "WorkloadCreated", conditions.ActionValidating, "Workload validated successfully")
	}

	// Set ObservedGeneration
	w.Status.ObservedGeneration = w.Generation

	// Return deployErr for requeue calculation (caller handles mapping)
	return deployErr
}

// handleDeletion handles Workload deletion with finalizer cleanup
func (r *WorkloadReconciler) handleDeletion(ctx context.Context, w *aifv1.Workload) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(w, workloadFinalizerName) {
		return ctrl.Result{}, nil
	}

	// P4-2: call Deployer.Teardown before removing the finalizer.
	// Project status.componentReleases into the domain type the Deployer
	// understands. On failure, keep the finalizer and requeue.
	previous := make([]workload.ComponentRelease, 0, len(w.Status.ComponentReleases))
	for _, c := range w.Status.ComponentReleases {
		previous = append(previous, workload.ComponentRelease{
			Name:        c.Name,
			ReleaseName: c.ReleaseName,
			Status:      c.Status,
			Revision:    c.Revision,
		})
	}
	if err := r.Deployer.Teardown(ctx, w.Namespace, previous); err != nil {
		r.Recorder.Eventf(w, nil, corev1.EventTypeWarning, "TeardownFailed",
			conditions.ActionDeleting, "Failed to teardown releases: %v", err)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	controllerutil.RemoveFinalizer(w, workloadFinalizerName)
	if err := r.Update(ctx, w); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// setCondition sets or updates a condition in the Workload status.
// Delegates to conditions.Set so LastTransitionTime is preserved when the
// status hasn't actually changed (pre-setting it breaks that contract).
func (r *WorkloadReconciler) setCondition(w *aifv1.Workload, condition metav1.Condition) {
	conditions.Set(&w.Status.Conditions, condition)
}

// emitDeployEvents emits P4-2 design spec §6.4 events for the deploy result.
// Stateless — EventRecorder aggregates duplicate Reason/Message within a window.
func (r *WorkloadReconciler) emitDeployEvents(
	w *aifv1.Workload,
	priorPhase aifv1.WorkloadPhase,
	result workload.DeployResult,
	deployErr error,
) {
	// Use w.Status.Phase (post-preservation) rather than result.Phase
	// (pre-preservation). The reconciler may have restored priorPhase for
	// un-classified errors; emit phase-transition events based on the final
	// persisted phase, not the raw deployer output.
	newPhase := w.Status.Phase
	if priorPhase != newPhase {
		switch newPhase {
		case aifv1.WorkloadPhaseDeploying:
			r.Recorder.Eventf(w, nil, corev1.EventTypeNormal, "Deploying",
				conditions.ActionReconciling, "Workload deployment in progress")
		case aifv1.WorkloadPhaseRunning:
			r.Recorder.Eventf(w, nil, corev1.EventTypeNormal, "Running",
				conditions.ActionReconciling, "All components deployed")
		case aifv1.WorkloadPhaseFailed:
			r.Recorder.Eventf(w, nil, corev1.EventTypeWarning, "ComponentInstallFailed",
				conditions.ActionInstalling, "One or more components failed: %v", deployErr)
		}
	}

	// BundleTest generation drift
	if w.Spec.Source.Kind == aifv1.WorkloadSourceKindBundleTest &&
		w.Spec.Source.BundleTest != nil &&
		result.ObservedBundleGeneration != 0 &&
		result.ObservedBundleGeneration != w.Spec.Source.BundleTest.Generation {
		r.Recorder.Eventf(w, nil, corev1.EventTypeNormal, "BundleTestGenerationDrift",
			conditions.ActionReconciling,
			"Bundle generation drifted: recorded=%d observed=%d",
			w.Spec.Source.BundleTest.Generation, result.ObservedBundleGeneration)
	}

	// Source-not-resolved + nested-Blueprint reject events
	if stderrors.Is(deployErr, workload.ErrSourceNotResolved) {
		r.Recorder.Eventf(w, nil, corev1.EventTypeWarning, "SourceNotResolved",
			conditions.ActionResolving, "%v", deployErr)
	}
	if stderrors.Is(deployErr, workload.ErrNestedBlueprintNotSupported) {
		r.Recorder.Eventf(w, nil, corev1.EventTypeWarning, "NestedBlueprintRejected",
			conditions.ActionValidating, "%v", deployErr)
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *WorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aifv1.Workload{}).
		Complete(r)
}
