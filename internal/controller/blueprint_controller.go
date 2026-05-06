package controller

import (
	"context"
	"fmt"
	"time"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/conditions"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	blueprintFinalizerName = "ai.suse.com/cleanup"
)

// BlueprintReconciler reconciles a Blueprint object
type BlueprintReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
	Manager  blueprint.Manager
}

// +kubebuilder:rbac:groups=ai.suse.com,resources=blueprints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.suse.com,resources=blueprints/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=blueprints/finalizers,verbs=update
// +kubebuilder:rbac:groups=ai.suse.com,resources=workloads,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconcile loop for Blueprint resources
func (r *BlueprintReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Blueprint
	var bp aifv1.Blueprint
	if err := r.Get(ctx, req.NamespacedName, &bp); err != nil {
		if errors.IsNotFound(err) {
			// Blueprint was deleted, nothing to do
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get Blueprint")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !bp.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &bp)
	}

	// Add finalizer if missing
	if !controllerutil.ContainsFinalizer(&bp, blueprintFinalizerName) {
		controllerutil.AddFinalizer(&bp, blueprintFinalizerName)
		if err := r.Update(ctx, &bp); err != nil {
			logger.Error(err, "failed to add finalizer")
			return ctrl.Result{}, err
		}
		// Return and requeue to continue reconciliation with finalizer present
		return ctrl.Result{Requeue: true}, nil
	}

	// Main reconciliation
	if err := r.reconcile(ctx, &bp); err != nil {
		logger.Error(err, "reconciliation failed")
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.Status().Update(ctx, &bp); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcile performs the core reconciliation logic
func (r *BlueprintReconciler) reconcile(ctx context.Context, bp *aifv1.Blueprint) error {
	logger := log.FromContext(ctx)

	// Validate Blueprint spec
	if err := r.Manager.ValidateSpec(bp); err != nil {
		logger.Info("Blueprint validation failed", "error", err)

		// Set Ready=False
		r.setCondition(bp, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonBlueprintInvalid,
			Message:            fmt.Sprintf("Validation failed: %v", err),
			ObservedGeneration: bp.Generation,
		})

		// Record event
		r.Recorder.Eventf(bp, nil, "Warning", conditions.ReasonBlueprintInvalid, conditions.ActionValidating, err.Error())

		// Set ObservedGeneration
		bp.Status.ObservedGeneration = bp.Generation

		return nil // Don't return error - status has been updated
	}

	// List all Workloads to compute deploymentCount
	var workloadList aifv1.WorkloadList
	if err := r.List(ctx, &workloadList); err != nil {
		logger.Error(err, "failed to list Workloads")
		return err
	}

	// Compute deployment count
	deploymentCount := r.Manager.ComputeDeploymentCount(bp, workloadList.Items)
	bp.Status.DeploymentCount = deploymentCount

	// Set phase to Active if not already set
	if bp.Status.Phase == "" {
		bp.Status.Phase = aifv1.BlueprintPhaseActive
	}

	// Set Ready=True
	r.setCondition(bp, metav1.Condition{
		Type:               conditions.TypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonBlueprintValidated,
		Message:            "Blueprint validated successfully",
		ObservedGeneration: bp.Generation,
	})

	// Record success event
	r.Recorder.Eventf(bp, nil, "Normal", conditions.ReasonBlueprintValidated, conditions.ActionValidating, "Blueprint validated successfully")

	// Set ObservedGeneration
	bp.Status.ObservedGeneration = bp.Generation

	return nil
}

// handleDeletion handles Blueprint deletion with finalizer cleanup
func (r *BlueprintReconciler) handleDeletion(ctx context.Context, bp *aifv1.Blueprint) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(bp, blueprintFinalizerName) {
		return ctrl.Result{}, nil
	}

	// List all Workloads
	var workloads aifv1.WorkloadList
	if err := r.List(ctx, &workloads); err != nil {
		return ctrl.Result{}, err
	}

	// Compute current deployment count
	count := r.Manager.ComputeDeploymentCount(bp, workloads.Items)

	if count > 0 {
		// Cannot delete - active Workloads exist
		r.Recorder.Eventf(bp, nil, "Warning", "BlueprintHasActiveWorkloads", conditions.ActionDeleting,
			"Cannot delete Blueprint with %d active Workloads", count)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Safe to delete - remove finalizer
	controllerutil.RemoveFinalizer(bp, blueprintFinalizerName)
	if err := r.Update(ctx, bp); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// setCondition sets or updates a condition in the Blueprint status.
// Delegates to conditions.Set so LastTransitionTime is preserved when the
// status hasn't actually changed (pre-setting it breaks that contract).
func (r *BlueprintReconciler) setCondition(bp *aifv1.Blueprint, condition metav1.Condition) {
	conditions.Set(&bp.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager
func (r *BlueprintReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aifv1.Blueprint{}).
		Watches(
			&aifv1.Workload{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				w, ok := obj.(*aifv1.Workload)
				if !ok || w.Spec.Source.Kind != aifv1.WorkloadSourceKindBlueprint || w.Spec.Source.Blueprint == nil {
					return nil
				}
				// Enqueue the Blueprint CR named "{blueprintName}.{version}"
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Name: fmt.Sprintf("%s.%s", w.Spec.Source.Blueprint.Name, w.Spec.Source.Blueprint.Version),
					},
				}}
			}),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc:  func(e event.CreateEvent) bool { return true },   // new Workload → recompute count
				DeleteFunc:  func(e event.DeleteEvent) bool { return true },   // gone Workload → recompute count
				UpdateFunc:  func(e event.UpdateEvent) bool { return false },  // CRITICAL: status updates don't change deploymentCount membership
				GenericFunc: func(e event.GenericEvent) bool { return false },
			}),
		).
		Complete(r)
}
