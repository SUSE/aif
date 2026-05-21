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

// WorkloadReconciler reconciles a Workload object.
//
// Repository is the K8s-backed CRUD port for Workload CRs. The reconciler
// routes its own CR reads/writes (Get on enter, Update for finalizer
// add/remove) through the port to keep the reconcile body framework-free.
// The embedded client.Client is kept for the controller-runtime watch
// setup; production and the envtest suite both wire the same K8sRepository.
type WorkloadReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   events.EventRecorder
	Deployer   workload.Deployer   // P4-2: Helm deployment engine
	Repository workload.Repository // P5-1: CR CRUD via the port
}

// +kubebuilder:rbac:groups=ai.suse.com,resources=workloads,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=workloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=workloads/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconcile loop for Workload resources
func (r *WorkloadReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Workload via the repository.
	w, err := r.Repository.Get(ctx, req.Namespace, req.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get Workload")
		return ctrl.Result{}, err
	}

	// Handle deletion.
	if !w.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, w)
	}

	// Add finalizer if missing.
	if !controllerutil.ContainsFinalizer(w, workloadFinalizerName) {
		controllerutil.AddFinalizer(w, workloadFinalizerName)
		if err := r.Repository.Update(ctx, w); err != nil {
			logger.Error(err, "failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Main reconciliation.
	deployErr := r.reconcile(ctx, w)

	// Persist status via the repository.
	if err := r.Repository.UpdateStatus(ctx, w); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	// Phase-driven requeue cadence (deployErr is folded into Phase upstream
	// via applyErrorPhaseOverrides; requeueForPhase reads the persisted
	// Status.Phase).
	_ = deployErr // intentionally discarded: phase + Ready Condition already
	// encode the failure; controller-runtime should not see it
	// and trigger an exponential backoff on top of our cadence.
	return requeueForPhase(w.Status.Phase), nil
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

// reconcile performs the core reconciliation logic.
func (r *WorkloadReconciler) reconcile(ctx context.Context, w *aifv1.Workload) error {
	logger := log.FromContext(ctx)
	// Captured BEFORE the Phase=Pending bootstrap below so applyErrorPhaseOverrides
	// correctly sees "" on first reconcile (nothing to preserve). The inner
	// capture in computePhaseWithTransitions reads the post-bootstrap value
	// because its counter-mutation semantics need a stable prior phase.
	priorPhase := workload.Phase(w.Status.Phase)

	// Validate source.
	if err := r.validateSource(w); err != nil {
		logger.Info("Workload validation failed", "error", err)
		r.setCondition(w, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonInvalidSpec,
			Message:            fmt.Sprintf("Validation failed: %v", err),
			ObservedGeneration: w.Generation,
		})
		r.Recorder.Eventf(w, nil, corev1.EventTypeWarning, "WorkloadInvalid", conditions.ActionValidating, err.Error())
		w.Status.ObservedGeneration = w.Generation
		return nil
	}

	// Bootstrap Phase=Pending on first reconcile so computePhaseWithTransitions
	// reads a stable prior phase (it inspects w.Status.Phase internally to
	// decide counter mutations, e.g. "entering Degraded from Pending" vs
	// "entering Degraded from empty"). The outer priorPhase captured above
	// keeps the original "" so applyErrorPhaseOverrides preserves nothing on
	// first reconcile.
	if w.Status.Phase == "" {
		w.Status.Phase = aifv1.WorkloadPhasePending
	}

	// Translate to DeployRequest, call Deployer, project per-component
	// outcome back into status (NOT Phase — controller owns that below).
	req := workload.WorkloadToDeployRequest(w)
	result, deployErr := r.Deployer.Deploy(ctx, req)
	workload.ApplyDeployResult(w, result)

	// Controller-owned phase computation. computePhaseWithTransitions does
	// the increment/reset side effects on Status.RecoveryFailureCount.
	newPhase := computePhaseWithTransitions(w)
	applyErrorPhaseOverrides(priorPhase, &newPhase, deployErr)
	w.Status.Phase = workload.PhaseToCR(newPhase)

	// Events + Ready condition. emitDeployEvents reads w.Status.Phase
	// (post-override) so the event matches the persisted phase.
	r.emitDeployEvents(w, aifv1.WorkloadPhase(priorPhase), result, deployErr)
	r.setReadyCondition(w, deployErr)

	w.Status.ObservedGeneration = w.Generation
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
	previous := workload.ComponentReleasesFromCR(w.Status.ComponentReleases)
	if err := r.Deployer.Teardown(ctx, w.Namespace, previous); err != nil {
		r.Recorder.Eventf(w, nil, corev1.EventTypeWarning, "TeardownFailed",
			conditions.ActionDeleting, "Failed to teardown releases: %v", err)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	controllerutil.RemoveFinalizer(w, workloadFinalizerName)
	if err := r.Repository.Update(ctx, w); err != nil {
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
		case aifv1.WorkloadPhaseDegraded:
			r.Recorder.Eventf(w, nil, corev1.EventTypeWarning, "Degraded",
				conditions.ActionReconciling, "Workload degraded (recovery failure count %d)",
				w.Status.RecoveryFailureCount)
		case aifv1.WorkloadPhaseRecoveryInProgress:
			r.Recorder.Eventf(w, nil, corev1.EventTypeNormal, "RecoveryInProgress",
				conditions.ActionReconciling, "Workload recovery in progress")
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

// computePhaseWithTransitions runs RecomputePhase, performs the counter
// mutations on Status.RecoveryFailureCount (increment-on-Degraded-entry,
// reset-on-Running-entry, reset-on-spec-change-from-Failed), and re-runs
// RecomputePhase if the counter changed so a threshold-crossing produces
// the correct terminal phase in the same reconcile pass.
//
// Terminal phase on threshold crossing depends on
// Spec.Strategy.AutomaticRecovery.Enabled (ARCHITECTURE.md §4.4 rule 2):
//
//   - recovery enabled  → Degraded → RecoveryInProgress (P5-1 owns entry;
//     P5-6 owns the rollback exit)
//   - recovery disabled → Failed-immediate on first failed component, so
//     the candidate is NEVER Degraded; the counter increment branch
//     below does not fire and the counter stays at its prior value.
//
// The counter increments ONLY on transition into Degraded — never on
// RecoveryInProgress entry, since the counter is already at threshold
// when we cross over (ARCHITECTURE.md §4.4 line 737).
//
// All counter side effects are confined here; RecomputePhase itself is pure.
func computePhaseWithTransitions(w *aifv1.Workload) workload.Phase {
	priorPhase := workload.Phase(w.Status.Phase)
	priorCount := w.Status.RecoveryFailureCount

	// Spec-change-from-Failed: reset the counter so the user can recover
	// by editing the CR. Without this, a Failed Workload stays Failed
	// forever even after spec edits (e.g., bumping chart version).
	if priorPhase == workload.PhaseFailed && w.Generation != w.Status.ObservedGeneration {
		w.Status.RecoveryFailureCount = 0
	}

	candidate := workload.RecomputePhase(workload.PhaseInputFromCR(w))

	// Increment on Degraded entry (transition into Degraded, not stay).
	if candidate == workload.PhaseDegraded && priorPhase != workload.PhaseDegraded {
		w.Status.RecoveryFailureCount++
	}
	// Reset on Running entry.
	if candidate == workload.PhaseRunning && priorPhase != workload.PhaseRunning {
		w.Status.RecoveryFailureCount = 0
	}

	// Re-run RecomputePhase if the counter changed — pure function, so
	// safe; the threshold may now promote Degraded → Failed in this pass.
	if w.Status.RecoveryFailureCount != priorCount {
		candidate = workload.RecomputePhase(workload.PhaseInputFromCR(w))
	}
	return candidate
}

// applyErrorPhaseOverrides folds terminal/transient deploy errors into the
// computed phase. Terminal sentinels (nested Blueprint) force Failed;
// unclassified errors preserve the prior phase so transient cluster I/O
// failures don't flap the user-visible phase.
//
// Note: an earlier draft proposed checking `*phase == ""` to restore prior
// phase, but RecomputePhase always returns at least PhasePending — that
// check never fires. Preserve prior phase whenever the prior is non-empty
// AND the error is unclassified.
func applyErrorPhaseOverrides(priorPhase workload.Phase, phase *workload.Phase, err error) {
	if err == nil {
		return
	}
	if stderrors.Is(err, workload.ErrNestedBlueprintNotSupported) {
		*phase = workload.PhaseFailed
		return
	}
	// Classified-but-recoverable errors (ErrSourceNotResolved,
	// ErrComponentInstallFailed, ErrComponentUninstallFailed) are NOT
	// overridden — the rule-driven phase from RecomputePhase already
	// reflects the per-component status those errors caused.
	reason, _, _ := mapDeployError(err)
	if reason == conditions.ReasonReconcileFailed && priorPhase != "" {
		*phase = priorPhase
	}
}

// requeueForPhase picks the per-phase requeue cadence from
// pkg/workload/constants.go. Failed and RecoveryInProgress both currently
// resolve to a zero-value cadence (no automatic requeue — Failed waits
// for a spec change; RecoveryInProgress will wait for P5-6's rollback
// completion event) but are kept on distinct switch arms so P5-6 can
// give RecoveryInProgress its own poll cadence without revisiting Failed.
func requeueForPhase(p aifv1.WorkloadPhase) ctrl.Result {
	switch p {
	case aifv1.WorkloadPhasePending:
		return ctrl.Result{RequeueAfter: workload.RequeuePending}
	case aifv1.WorkloadPhaseDeploying:
		return ctrl.Result{RequeueAfter: workload.RequeueDeploying}
	case aifv1.WorkloadPhaseRunning:
		return ctrl.Result{RequeueAfter: workload.RequeueRunning}
	case aifv1.WorkloadPhaseDegraded:
		return ctrl.Result{RequeueAfter: workload.RequeueDegraded}
	case aifv1.WorkloadPhaseRecoveryInProgress:
		return ctrl.Result{RequeueAfter: workload.RequeueRecoveryInProgress}
	case aifv1.WorkloadPhaseFailed:
		return ctrl.Result{RequeueAfter: workload.RequeueFailed}
	default:
		// Unknown phase (shouldn't happen — enum is validated at admission).
		// Default to the Running cadence as a conservative fallback.
		return ctrl.Result{RequeueAfter: workload.RequeueRunning}
	}
}

// setReadyCondition drives the Ready Condition from the persisted Phase.
// Each phase maps to a Status + Reason from pkg/conditions/types.go;
// CI's grep-guard against raw condition strings stays green because all
// reasons are constants.
//
// When deployErr is non-nil and unclassified, ReasonReconcileFailed wins
// (set inline here rather than via Phase→Reason mapping) so the user can
// distinguish transient errors from phase-driven Ready=False states.
func (r *WorkloadReconciler) setReadyCondition(w *aifv1.Workload, deployErr error) {
	cond := metav1.Condition{
		Type:               conditions.TypeReady,
		ObservedGeneration: w.Generation,
	}

	if deployErr != nil {
		reason, _, _ := mapDeployError(deployErr)
		cond.Status = metav1.ConditionFalse
		cond.Reason = reason
		cond.Message = deployErr.Error()
		r.setCondition(w, cond)
		return
	}

	switch w.Status.Phase {
	case aifv1.WorkloadPhaseRunning:
		cond.Status = metav1.ConditionTrue
		cond.Reason = conditions.ReasonWorkloadRunning
		cond.Message = "All components deployed and ready"
	case aifv1.WorkloadPhasePending:
		cond.Status = metav1.ConditionFalse
		cond.Reason = conditions.ReasonWorkloadPending
		cond.Message = "Workload reconciliation pending"
	case aifv1.WorkloadPhaseDeploying:
		cond.Status = metav1.ConditionFalse
		cond.Reason = conditions.ReasonWorkloadDeploying
		cond.Message = "Workload deployment in progress"
	case aifv1.WorkloadPhaseDegraded:
		cond.Status = metav1.ConditionFalse
		cond.Reason = conditions.ReasonWorkloadDegraded
		cond.Message = fmt.Sprintf("Workload degraded (recovery failure count %d)", w.Status.RecoveryFailureCount)
	case aifv1.WorkloadPhaseRecoveryInProgress:
		cond.Status = metav1.ConditionFalse
		cond.Reason = conditions.ReasonWorkloadRecoveryInProgress
		cond.Message = "Workload recovery in progress"
	case aifv1.WorkloadPhaseFailed:
		cond.Status = metav1.ConditionFalse
		cond.Reason = conditions.ReasonWorkloadFailed
		cond.Message = "Workload failed; spec change required to recover"
	default:
		cond.Status = metav1.ConditionFalse
		cond.Reason = conditions.ReasonReconcileFailed
		cond.Message = fmt.Sprintf("Workload in unknown phase %q", w.Status.Phase)
	}
	r.setCondition(w, cond)
}

// SetupWithManager sets up the controller with the Manager
func (r *WorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aifv1.Workload{}).
		Complete(r)
}
