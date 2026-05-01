package controller

import (
	"context"
	"fmt"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/conditions"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const finalizerName = "ai.suse.com/cleanup"

// BundleReconciler reconciles a Bundle object
type BundleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Manager  bundle.Manager
}

// +kubebuilder:rbac:groups=ai.suse.com,resources=bundles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.suse.com,resources=bundles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=bundles/finalizers,verbs=update
// +kubebuilder:rbac:groups=ai.suse.com,resources=blueprints,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile implements the reconcile loop for Bundle resources
func (r *BundleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Bundle
	var bundleCR aifv1.Bundle
	if err := r.Get(ctx, req.NamespacedName, &bundleCR); err != nil {
		if errors.IsNotFound(err) {
			// Bundle was deleted, nothing to do
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get Bundle")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !bundleCR.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &bundleCR)
	}

	// Add finalizer if missing
	if !controllerutil.ContainsFinalizer(&bundleCR, finalizerName) {
		controllerutil.AddFinalizer(&bundleCR, finalizerName)
		if err := r.Update(ctx, &bundleCR); err != nil {
			logger.Error(err, "failed to add finalizer")
			return ctrl.Result{}, err
		}
		// Return and requeue to continue reconciliation with finalizer present
		return ctrl.Result{Requeue: true}, nil
	}

	// Skip reconciliation if paused
	if bundleCR.Spec.Paused {
		logger.Info("Bundle reconciliation paused")
		return ctrl.Result{}, nil
	}

	// Reconcile Bundle
	if err := r.reconcile(ctx, &bundleCR); err != nil {
		logger.Error(err, "reconciliation failed")
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.Status().Update(ctx, &bundleCR); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcile performs the core reconciliation logic
func (r *BundleReconciler) reconcile(ctx context.Context, bundleCR *aifv1.Bundle) error {
	logger := log.FromContext(ctx)

	// Convert CR to domain model
	domainBundle := bundle.BundleFromCR(bundleCR)

	// Validate via Manager
	if err := r.Manager.Upsert(ctx, domainBundle); err != nil {
		// Validation failed - set Ready=False
		r.setCondition(bundleCR, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonInvalidSpec,
			Message:            fmt.Sprintf("Validation failed: %v", err),
			ObservedGeneration: bundleCR.Generation,
		})
		r.Recorder.Event(bundleCR, "Warning", conditions.ReasonInvalidSpec, err.Error())
		bundleCR.Status.ObservedGeneration = bundleCR.Generation
		return nil // Don't requeue - user must fix spec
	}

	// Check for partial approval and heal if needed (ARCHITECTURE.md §6.5.2)
	r.checkAndHealPartialApproval(ctx, bundleCR)

	// Validation passed - set Ready=True
	r.setCondition(bundleCR, metav1.Condition{
		Type:               conditions.TypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonReconciled,
		Message:            "Bundle successfully reconciled",
		ObservedGeneration: bundleCR.Generation,
	})

	bundleCR.Status.ObservedGeneration = bundleCR.Generation

	logger.Info("Bundle reconciled successfully")
	return nil
}

// handleDeletion handles Bundle deletion
func (r *BundleReconciler) handleDeletion(ctx context.Context, bundleCR *aifv1.Bundle) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(bundleCR, finalizerName) {
		// Finalizer already removed, nothing to do
		return ctrl.Result{}, nil
	}

	// TODO: P3-1 will add cache cleanup via Manager.Delete()
	// For P1-1, cache is in-memory only and will be GC'd with operator restart
	logger.Info("Bundle deleted, cache cleanup deferred to P3-1")

	// Remove finalizer
	controllerutil.RemoveFinalizer(bundleCR, finalizerName)
	if err := r.Update(ctx, bundleCR); err != nil {
		logger.Error(err, "failed to remove finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// setCondition updates or appends a condition to the Bundle status
func (r *BundleReconciler) setCondition(bundleCR *aifv1.Bundle, condition metav1.Condition) {
	// Find existing condition
	for i := range bundleCR.Status.Conditions {
		if bundleCR.Status.Conditions[i].Type == condition.Type {
			// Update existing condition
			bundleCR.Status.Conditions[i] = condition
			bundleCR.Status.Conditions[i].LastTransitionTime = metav1.Now()
			return
		}
	}

	// Append new condition
	condition.LastTransitionTime = metav1.Now()
	bundleCR.Status.Conditions = append(bundleCR.Status.Conditions, condition)
}

// checkAndHealPartialApproval implements ARCHITECTURE.md §6.5.2 self-healing.
// Detects Submitted bundles with matching published Blueprint and recovers
// by resetting phase to Draft, appending to publishedVersions, and clearing
// submission/review. This is best-effort recovery - logs errors but doesn't
// fail the reconcile.
func (r *BundleReconciler) checkAndHealPartialApproval(ctx context.Context, bundleCR *aifv1.Bundle) {
	logger := log.FromContext(ctx)

	// Only heal if phase is Submitted
	if bundleCR.Status.Phase != aifv1.BundlePhaseSubmitted {
		return
	}

	// Must have submission data
	if bundleCR.Status.Submission == nil {
		return
	}

	// Construct expected Blueprint name: {targetBlueprint}.{proposedVersion}
	blueprintName := fmt.Sprintf("%s.%s", bundleCR.Spec.TargetBlueprint, bundleCR.Status.Submission.ProposedVersion)

	// Try to fetch the Blueprint (cluster-scoped)
	var bp aifv1.Blueprint
	if err := r.Get(ctx, client.ObjectKey{Name: blueprintName}, &bp); err != nil {
		if errors.IsNotFound(err) {
			// Blueprint doesn't exist - no healing needed
			return
		}
		// Other error - log but don't fail reconcile
		logger.Error(err, "self-healing: failed to get Blueprint", "blueprint", blueprintName)
		return
	}

	// Verify Blueprint was published from this Bundle
	if bp.Spec.Source.Type != aifv1.BlueprintSourcePublished {
		// Not a published Blueprint - no healing
		return
	}
	if bp.Spec.Source.PublishedFrom == nil {
		// Invalid state - log warning
		logger.Info("self-healing: Blueprint has Source.Type=Published but PublishedFrom is nil", "blueprint", blueprintName)
		return
	}

	pubFrom := bp.Spec.Source.PublishedFrom
	if pubFrom.BundleNamespace != bundleCR.Namespace ||
		pubFrom.BundleName != bundleCR.Name ||
		pubFrom.BundleGeneration != bundleCR.Status.Submission.GenerationAtSubmit {
		// Blueprint from different Bundle - no healing
		return
	}

	// Match found - heal the Bundle status
	logger.Info("self-healing: detected partial approval, recovering Bundle status",
		"bundle", bundleCR.Name,
		"namespace", bundleCR.Namespace,
		"blueprint", blueprintName,
		"version", bp.Spec.Version)

	// Append to publishedVersions
	publishedVersion := aifv1.PublishedVersionRef{
		BlueprintName: bp.Spec.BlueprintName,
		Version:       bp.Spec.Version,
		PublishedAt:   bp.Spec.PublishedAt,
		PublishedBy:   bp.Spec.PublishedBy,
	}
	bundleCR.Status.PublishedVersions = append(bundleCR.Status.PublishedVersions, publishedVersion)

	// Reset phase to Draft
	bundleCR.Status.Phase = aifv1.BundlePhaseDraft

	// Clear submission and review
	bundleCR.Status.Submission = nil
	bundleCR.Status.Review = nil

	// Record event
	r.Recorder.Event(bundleCR, "Normal", conditions.ReasonReconciled,
		fmt.Sprintf("Self-healing: recovered from partial approval, Blueprint %s published", blueprintName))

	logger.Info("self-healing: Bundle status recovered successfully")
}

// SetupWithManager sets up the controller with the Manager
func (r *BundleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aifv1.Bundle{}).
		Complete(r)
}
