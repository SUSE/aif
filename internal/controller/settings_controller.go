package controller

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
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

// Sentinel errors for Secret resolution. Callers classify failures with
// errors.Is — never with strings.Contains on the message (that pattern is
// in CLAUDE.md's Forbidden list).
var (
	ErrSecretNotFound   = stderrors.New("secret not found")
	ErrInvalidSecretKey = stderrors.New("invalid secret key")
)

const (
	settingsFinalizerName = "ai.suse.com/cleanup"

	// Event reasons
	eventSettingsApplied   = "SettingsApplied"
	eventSecretNotFound    = "SecretNotFound"
	eventInvalidSecretKey  = "InvalidSecretKey"
)

// SettingsReconciler reconciles a Settings object.
//
// Per ARCHITECTURE.md §6.2 / §8.2.1, P5-4 will replace direct credential
// resolution with an EngineSettings push to pkg/{apps,git,nvidia}.Engine.
// Until then, the reconciler resolves Secrets, validates them, and discards
// the values — the goal of P1-4 is wiring + status only, no engine state.
type SettingsReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// Reconcile handles Settings reconciliation
// +kubebuilder:rbac:groups=ai.suse.com,resources=settings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.suse.com,resources=settings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=settings/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
func (r *SettingsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Settings resource
	var settings aifv1.Settings
	if err := r.Get(ctx, req.NamespacedName, &settings); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Settings resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get Settings")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !settings.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &settings)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&settings, settingsFinalizerName) {
		controllerutil.AddFinalizer(&settings, settingsFinalizerName)
		if err := r.Update(ctx, &settings); err != nil {
			logger.Error(err, "failed to add finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("added finalizer, requeuing")
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile
	return r.reconcile(ctx, &settings)
}

// reconcile performs the main reconciliation logic.
//
// In P1-4 the resolved Secret values are validated and then discarded; P5-4
// will replace this with an EngineSettings push to pkg/{apps,git,nvidia}.Engine
// per ARCHITECTURE.md §6.2.
func (r *SettingsReconciler) reconcile(ctx context.Context, settings *aifv1.Settings) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Resolve SUSE Registry credentials (validates Secret presence + key)
	if settings.Spec.SUSERegistry != nil {
		if _, err := r.resolveSecretKeyRef(ctx, settings.Spec.SUSERegistry.UserSecretRef); err != nil {
			return r.handleSecretError(ctx, settings, err, "SUSERegistry.userSecretRef")
		}
		if _, err := r.resolveSecretKeyRef(ctx, settings.Spec.SUSERegistry.TokenSecretRef); err != nil {
			return r.handleSecretError(ctx, settings, err, "SUSERegistry.tokenSecretRef")
		}
	}

	// Resolve Application Collection credentials (validates Secret presence + key)
	if settings.Spec.ApplicationCollection != nil {
		if _, err := r.resolveSecretKeyRef(ctx, settings.Spec.ApplicationCollection.UserSecretRef); err != nil {
			return r.handleSecretError(ctx, settings, err, "ApplicationCollection.userSecretRef")
		}
		if _, err := r.resolveSecretKeyRef(ctx, settings.Spec.ApplicationCollection.TokenSecretRef); err != nil {
			return r.handleSecretError(ctx, settings, err, "ApplicationCollection.tokenSecretRef")
		}
	}

	// Resolve Fleet credentials (validates Secret presence + key)
	if settings.Spec.Fleet != nil {
		if _, err := r.resolveSecretKeyRef(ctx, settings.Spec.Fleet.CredSecretRef); err != nil {
			return r.handleSecretError(ctx, settings, err, "Fleet.credSecretRef")
		}
	}

	// Update status
	settings.Status.ObservedGeneration = settings.Generation
	settings.Status.LastApplied = metav1.Now()
	r.setCondition(settings, conditions.TypeReady, metav1.ConditionTrue, conditions.ReasonReconciled, "Settings applied successfully")

	if err := r.Status().Update(ctx, settings); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	// Emit event
	r.Recorder.Eventf(settings, nil, corev1.EventTypeNormal, eventSettingsApplied, conditions.ActionApplying, "Settings applied successfully")

	return ctrl.Result{}, nil
}

// resolveSecretKeyRef resolves a SecretKeySelector to its value
func (r *SettingsReconciler) resolveSecretKeyRef(ctx context.Context, ref *corev1.SecretKeySelector) (string, error) {
	if ref == nil {
		return "", nil
	}

	var secret corev1.Secret
	secretName := client.ObjectKey{
		// Settings is a singleton resource in the aif namespace per ARCHITECTURE.md §4.5.
		// All referenced Secrets must exist in the same namespace.
		Namespace: "aif",
		Name:      ref.Name,
	}

	if err := r.Get(ctx, secretName, &secret); err != nil {
		if errors.IsNotFound(err) {
			return "", fmt.Errorf("secret %s: %w", ref.Name, ErrSecretNotFound)
		}
		return "", fmt.Errorf("failed to get secret %s: %w", ref.Name, err)
	}

	value, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %s in secret %s: %w", ref.Key, ref.Name, ErrInvalidSecretKey)
	}

	return string(value), nil
}

// handleSecretError processes Secret resolution errors and updates status
func (r *SettingsReconciler) handleSecretError(ctx context.Context, settings *aifv1.Settings, err error, field string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Classify error (check more specific patterns first)
	var reason string
	var eventType string
	if isInvalidSecretKey(err) {
		reason = conditions.ReasonInvalidSecretKey
		eventType = eventInvalidSecretKey
	} else if isSecretNotFound(err) {
		reason = conditions.ReasonSecretNotFound
		eventType = eventSecretNotFound
	} else {
		reason = conditions.ReasonInvalidSpec
		eventType = eventInvalidSecretKey
	}

	msg := fmt.Sprintf("Failed to resolve %s: %v", field, err)

	// Update condition
	r.setCondition(settings, conditions.TypeReady, metav1.ConditionFalse, reason, msg)

	// Update status
	if statusErr := r.Status().Update(ctx, settings); statusErr != nil {
		logger.Error(statusErr, "failed to update status")
		return ctrl.Result{}, statusErr
	}

	// Emit event
	r.Recorder.Eventf(settings, nil, corev1.EventTypeWarning, eventType, conditions.ActionResolving, msg)

	// Requeue after 30s to retry secret resolution
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// isSecretNotFound checks if an error indicates a missing Secret.
// Uses errors.Is on the package's sentinel; falls through to apierrors.IsNotFound
// to also catch the "not yet wrapped" path (defensive, in case a caller adds a
// new code path that returns the apiserver's NotFound directly).
func isSecretNotFound(err error) bool {
	return err != nil && (stderrors.Is(err, ErrSecretNotFound) || errors.IsNotFound(err))
}

// isInvalidSecretKey checks if an error indicates a missing key in a Secret.
func isInvalidSecretKey(err error) bool {
	return err != nil && stderrors.Is(err, ErrInvalidSecretKey)
}

// handleDeletion handles Settings deletion
func (r *SettingsReconciler) handleDeletion(ctx context.Context, settings *aifv1.Settings) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(settings, settingsFinalizerName) {
		// P1-4: No cleanup logic yet (no engines to disconnect)
		// P5-4 will add engine cleanup here

		controllerutil.RemoveFinalizer(settings, settingsFinalizerName)
		if err := r.Update(ctx, settings); err != nil {
			logger.Error(err, "failed to remove finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("removed finalizer")
	}

	return ctrl.Result{}, nil
}

// setCondition updates a condition in the Settings status.
// Delegates to conditions.Set (built on meta.SetStatusCondition).
func (r *SettingsReconciler) setCondition(settings *aifv1.Settings, condType string, status metav1.ConditionStatus, reason, message string) {
	conditions.Set(&settings.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: settings.Generation,
		Reason:             reason,
		Message:            message,
	})
}

// SetupWithManager sets up the controller with the Manager
func (r *SettingsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aifv1.Settings{}).
		Complete(r)
}
