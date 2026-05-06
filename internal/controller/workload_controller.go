package controller

import (
	"context"
	"fmt"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
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
	if err := r.reconcile(ctx, &w); err != nil {
		logger.Error(err, "reconciliation failed")
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.Status().Update(ctx, &w); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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

	// Set Ready=False Reason=AwaitingDeployer
	r.setCondition(w, metav1.Condition{
		Type:               conditions.TypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             conditions.ReasonAwaitingDeployer,
		Message:            "Workload validated, waiting for deployment logic (Phase 4/5)",
		ObservedGeneration: w.Generation,
	})

	// Record success event
	r.Recorder.Eventf(w, nil, "Normal", "WorkloadCreated", conditions.ActionValidating, "Workload validated successfully")

	// Set ObservedGeneration
	w.Status.ObservedGeneration = w.Generation

	return nil
}

// handleDeletion handles Workload deletion with finalizer cleanup
func (r *WorkloadReconciler) handleDeletion(ctx context.Context, w *aifv1.Workload) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(w, workloadFinalizerName) {
		return ctrl.Result{}, nil
	}

	// No cleanup needed in P1-3 (no Helm releases yet - Phase 4/5)
	// Just remove finalizer
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

// SetupWithManager sets up the controller with the Manager
func (r *WorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aifv1.Workload{}).
		Complete(r)
}
