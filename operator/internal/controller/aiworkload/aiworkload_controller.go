/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aiworkload

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	log "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

const aiWorkloadFinalizer = "ai-factory.suse.com/cleanup"

const (
	conditionTypeReady        = "Ready"
	reasonClusterRepoNotReady = "ClusterRepoNotReady"
	reasonReconciled          = "Reconciled"
	// Deletion-path (uninstall safety-net) reasons, surfaced on the terminating CR.
	reasonAwaitingUninstall    = "AwaitingUninstall"
	reasonUninstalling         = "Uninstalling"
	reasonRancherTokenRejected = "RancherTokenRejected"
)

// setCondition upserts a status condition on the AIWorkload, mirroring the
// InstallAIExtension controller's helper. meta.SetStatusCondition handles
// LastTransitionTime and de-duplication.
func setCondition(conditions *[]metav1.Condition, condType string, status metav1.ConditionStatus, reason, message string, generation int64) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// truncateForCondition bounds a message so it cannot overflow the CRD's
// 32768-byte condition.message cap or flood the log. Fetch errors can carry a
// whole HTML error page from an ingress or service mesh.
func truncateForCondition(s string) string {
	const max = 1024
	if len(s) <= max {
		return s
	}
	return s[:max] + "… (truncated)"
}

var (
	bundleDeploymentGVK = schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeployment"}
	bundleGVK           = schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
	helmOpGVK           = schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "HelmOp"}
	fleetNamespaces     = []string{"fleet-local", "fleet-default"}
)

// AIWorkloadReconciler reconciles AIWorkload objects.
type AIWorkloadReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	OperatorNamespace string
	// CatalogClient holds the current Rancher catalog client used to fetch charts
	// from git-backed ClusterRepos. The Settings controller rebuilds and swaps
	// the client into this holder when the rancherCatalog config changes. A nil
	// holder or an empty holder means git-backed repos are unconfigured;
	// git-backed components then report a clear condition and http/oci components
	// are unaffected.
	CatalogClient *rancher.Holder
	Recorder      record.EventRecorder
}

// event records a Kubernetes event, tolerating a nil Recorder (unit tests / early boot).
func (r *AIWorkloadReconciler) event(w *aiplatformv1alpha1.AIWorkload, eventtype, reason, msgFmt string, args ...any) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(w, eventtype, reason, msgFmt, args...)
}

// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=aiworkloads,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=aiworkloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=aiworkloads/finalizers,verbs=update
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=settings,verbs=get;list;watch
// +kubebuilder:rbac:groups=fleet.cattle.io,resources=bundledeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=blueprints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=clusterrepos,verbs=get;list;watch
// +kubebuilder:rbac:groups=fleet.cattle.io,resources=helmops,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=fleet.cattle.io,resources=bundles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups="",resources=services;configmaps;persistentvolumeclaims,verbs=get;list;delete
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;replicasets;daemonsets,verbs=get;list;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=create;get;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *AIWorkloadReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	var w aiplatformv1alpha1.AIWorkload
	if err := r.Get(ctx, req.NamespacedName, &w); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !w.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &w)
	}

	if !controllerutil.ContainsFinalizer(&w, aiWorkloadFinalizer) {
		controllerutil.AddFinalizer(&w, aiWorkloadFinalizer)
		return ctrl.Result{Requeue: true}, r.Update(ctx, &w)
	}

	if handled, err := r.handleTriggers(ctx, &w); err != nil {
		return ctrl.Result{}, err
	} else if handled {
		return ctrl.Result{Requeue: true}, nil
	}

	result, reconcileErr := r.reconcileStatus(ctx, &w)

	// Advance ObservedGeneration only when reconciliation reached a terminal
	// (non-requeue, non-error) state for this generation.
	if reconcileErr == nil && !result.Requeue && result.RequeueAfter == 0 {
		w.Status.ObservedGeneration = w.Generation
	}

	// Always persist status — even when reconcile failed or asked for a
	// requeue — so failure conditions/phase reach the UI instead of being
	// dropped by an early return.
	if err := r.Status().Update(ctx, &w); err != nil {
		// The object may have been deleted by reconcileGitOpsStatus (HelmOp gone path).
		if reconcileErr != nil {
			return ctrl.Result{}, reconcileErr
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	}
	if result.Requeue || result.RequeueAfter > 0 {
		return result, nil
	}

	if res, err := r.reconcileOperation(ctx, &w); err != nil || res.RequeueAfter > 0 {
		return res, err
	}

	if len(w.Status.PullSecretDeliveries) > 0 {
		if err := r.deliverPullSecrets(ctx, &w, r.pullSecretFactory(ctx)); err != nil {
			return ctrl.Result{}, err
		}
		settled, err := r.reconcilePullSecrets(ctx, &w)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !settled {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	}

	l.Info("reconciled AIWorkload", "phase", w.Status.Phase)
	return ctrl.Result{}, nil
}

// reconcileStatus dispatches to the strategy-specific status reconciler.
func (r *AIWorkloadReconciler) reconcileStatus(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) (ctrl.Result, error) {
	if w.Spec.Source.SourceType == aiplatformv1alpha1.AIWorkloadSourceBlueprint {
		return r.reconcileBlueprintStatus(ctx, w)
	}
	// For App-sourced workloads, run the secret injector before the
	// strategy-specific status path. The injector only populates
	// Status.PullSecretDeliveries (per-namespace bucket); the
	// post-reconcile block (line ~92) drives the actual local-write +
	// downstream Fleet Bundle + SA-merge Job.
	if w.Spec.Source.SourceType == aiplatformv1alpha1.AIWorkloadSourceApp {
		if err := r.reconcileAppPullSecrets(ctx, w); err != nil {
			return ctrl.Result{}, err
		}
	}
	switch w.Spec.DeployStrategy {
	case aiplatformv1alpha1.AIWorkloadDeployHelm:
		return ctrl.Result{}, r.reconcileHelmStatus(ctx, w)
	case aiplatformv1alpha1.AIWorkloadDeployFleetBundle:
		return ctrl.Result{}, r.reconcileFleetStatus(ctx, w)
	case aiplatformv1alpha1.AIWorkloadDeployGitOps:
		return ctrl.Result{}, r.reconcileGitOpsStatus(ctx, w)
	}
	return ctrl.Result{}, nil
}

// ── Helm path ────────────────────────────────────────────────────────────────

func (r *AIWorkloadReconciler) reconcileHelmStatus(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) error {
	if w.Spec.Source.App == nil {
		return nil
	}
	exists, err := r.helmReleaseExists(ctx, w.Spec.TargetNamespace, w.Spec.Source.App.Release)
	if err != nil {
		return err
	}
	if exists {
		w.Status.Phase = aiplatformv1alpha1.AIWorkloadPhaseRunning
	} else {
		w.Status.Phase = aiplatformv1alpha1.AIWorkloadPhaseUnknown
		w.Status.ClusterStatuses = nil
	}
	return nil
}

// helmReleaseExists returns true when at least one Helm release secret exists for the given release.
func (r *AIWorkloadReconciler) helmReleaseExists(ctx context.Context, namespace, releaseName string) (bool, error) {
	var list corev1.SecretList
	if err := r.List(ctx, &list,
		client.InNamespace(namespace),
		client.MatchingLabels{"owner": "helm", "name": releaseName},
	); err != nil {
		return false, err
	}
	return len(list.Items) > 0, nil
}

// appUninstaller is the subset of the Rancher catalog client the deletion path
// needs. *rancher.CatalogClient satisfies it. Kept local so the finalizer does
// not depend on the whole catalog surface.
type appUninstaller interface {
	UninstallApp(ctx context.Context, namespace, releaseName string) error
	AppUninstallInProgress(ctx context.Context, namespace, releaseName string) (bool, error)
}

// handleHelmRelease is the non-orphaning uninstall safety-net for App/Helm
// workloads. Uninstall itself is UI-driven (Rancher action=uninstall under the
// user's session); this finalizer step only guarantees the CR is not removed
// until the Helm release is actually gone, so the workload never disappears from
// the Workloads page while the chart lingers in "uninstalling".
//
// Returns done=true only when the release is gone; otherwise it returns
// done=false with a requeue so the caller retains the finalizer.
func (r *AIWorkloadReconciler) handleHelmRelease(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) (bool, ctrl.Result, error) {
	l := log.FromContext(ctx)
	ns, release := w.Spec.TargetNamespace, w.Spec.Source.App.Release

	exists, err := r.helmReleaseExists(ctx, ns, release)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	if !exists {
		return true, ctrl.Result{}, nil // release gone — safe to finalize
	}

	// Release still present. If a Rancher token is configured, actively delegate
	// the uninstall to Rancher (covers headless kubectl/GitOps deletes); the
	// helm-operation runs privileged so it deletes every chart resource kind.
	if u := r.appUninstaller(); u != nil {
		// rejectToken surfaces a token rejection on the terminating CR and requeues.
		// Shared by the state read and the uninstall call, which fail the same way.
		rejectToken := func(err error) (bool, ctrl.Result, error) {
			l.Error(err, "rancher rejected the catalog token during uninstall",
				"namespace", ns, "release", release)
			r.setUninstallCondition(ctx, w, reasonRancherTokenRejected,
				"Rancher rejected the catalog token during uninstall — re-authorize under Settings → Rancher API Access.")
			return false, ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}

		// Only request an uninstall Rancher is not already running — otherwise we
		// would spawn a fresh helm-operation on every reconcile tick.
		inProgress, err := u.AppUninstallInProgress(ctx, ns, release)
		if err != nil {
			if stderrors.Is(err, rancher.ErrUnauthorized) {
				return rejectToken(err)
			}
			l.Error(err, "could not read Rancher app state — will retry",
				"namespace", ns, "release", release)
			return false, ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		if !inProgress {
			if err := u.UninstallApp(ctx, ns, release); err != nil {
				if stderrors.Is(err, rancher.ErrUnauthorized) {
					return rejectToken(err)
				}
				l.Error(err, "rancher uninstall delegation failed — will retry",
					"namespace", ns, "release", release)
				return false, ctrl.Result{RequeueAfter: 15 * time.Second}, nil
			}
			l.Info("requested Rancher uninstall; waiting for release to clear",
				"namespace", ns, "release", release)
		}
		r.setUninstallCondition(ctx, w, reasonUninstalling,
			"Uninstalling the Helm release via Rancher; waiting for it to clear.")
		return false, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// No token: uninstall is the UI's job. Wait for the release to disappear
	// rather than orphan it. Surface a condition so a headless/GitOps delete that
	// stalls here is visible on the CR, not only in the operator log.
	l.Info("Helm release still present; retaining finalizer until it is uninstalled "+
		"(uninstall from the Apps page, or configure a Rancher token to enable headless uninstall)",
		"namespace", ns, "release", release)
	r.setUninstallCondition(ctx, w, reasonAwaitingUninstall,
		"Helm release still present; uninstall from the Apps page, or configure a Rancher token "+
			"(Settings → Rancher API Access) to enable headless uninstall.")
	return false, ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// setUninstallCondition surfaces the deletion-path state on the terminating CR
// (Ready=False with a deletion-specific reason) so a workload stuck waiting on an
// uninstall is visible in `kubectl get`/the UI, not only in the operator log. It
// skips the write when the condition already carries the same reason, so a
// waiting workload does not emit a status update on every reconcile.
func (r *AIWorkloadReconciler) setUninstallCondition(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, reason, message string) {
	if c := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady); c != nil &&
		c.Status == metav1.ConditionFalse && c.Reason == reason {
		return
	}
	setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, reason, truncateForCondition(message), w.Generation)
	if err := r.Status().Update(ctx, w); err != nil {
		log.FromContext(ctx).Error(err, "failed to update AIWorkload uninstall status condition")
	}
}

// appUninstaller returns the configured Rancher client if it can perform an App
// uninstall, or nil when no token is configured.
func (r *AIWorkloadReconciler) appUninstaller() appUninstaller {
	if r.CatalogClient == nil {
		return nil
	}
	if u, ok := r.CatalogClient.Get().(appUninstaller); ok {
		return u
	}
	return nil
}

// ── FleetBundle / GitOps path ─────────────────────────────────────────────────

// reconcileFleetStatus handles the FleetBundle strategy reconcile loop.
func (r *AIWorkloadReconciler) reconcileFleetStatus(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) error {
	if len(w.Spec.FleetBundleNames) == 0 {
		return nil
	}
	ho, err := r.getHelmOp(ctx, w.Spec.FleetBundleNames[0])
	if err != nil {
		return err
	}
	if ho == nil {
		w.Status.Phase = aiplatformv1alpha1.AIWorkloadPhaseUnknown
		w.Status.ClusterStatuses = nil
		return nil
	}
	return r.mirrorFleetStatus(ctx, w)
}

// deleteHelmOp deletes the HelmOp from whichever fleet workspace namespace it lives in.
// It attempts every namespace and joins any non-NotFound errors, so a failure in one
// namespace does not skip cleanup in the others.
func (r *AIWorkloadReconciler) deleteHelmOp(ctx context.Context, name string) error {
	var errs []error
	for _, ns := range fleetNamespaces {
		ho := &unstructured.Unstructured{}
		ho.SetGroupVersionKind(helmOpGVK)
		ho.SetName(name)
		ho.SetNamespace(ns)
		if err := r.Delete(ctx, ho); err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete HelmOp %s/%s: %w", ns, name, err))
		}
	}
	return stderrors.Join(errs...)
}

// deleteBundle deletes the Fleet Bundle the HelmOp generated (it shares the HelmOp's
// name). Fleet links this Bundle to its HelmOp only by a label — there is no
// ownerReference — so deleting the HelmOp does not garbage-collect it, and Fleet's
// own cleanup is racy. We delete the Bundle directly so teardown is deterministic;
// the Bundle's finalizer then prunes the BundleDeployment and deployed resources.
func (r *AIWorkloadReconciler) deleteBundle(ctx context.Context, name string) error {
	var errs []error
	for _, ns := range fleetNamespaces {
		b := &unstructured.Unstructured{}
		b.SetGroupVersionKind(bundleGVK)
		b.SetName(name)
		b.SetNamespace(ns)
		if err := r.Delete(ctx, b); err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete Bundle %s/%s: %w", ns, name, err))
		}
	}
	return stderrors.Join(errs...)
}

func (r *AIWorkloadReconciler) mirrorFleetStatus(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) error {
	bdList := &unstructured.UnstructuredList{}
	bdList.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeploymentList",
	})
	// App-sourced workloads always have exactly one bundle; Blueprint workloads use mirrorBlueprintStatus.
	if err := r.List(ctx, bdList, client.MatchingLabels{
		"fleet.cattle.io/bundle-name": w.Spec.FleetBundleNames[0],
	}); err != nil {
		return err
	}

	statuses := make([]aiplatformv1alpha1.AIWorkloadClusterStatus, 0, len(bdList.Items))
	for _, bd := range bdList.Items {
		clusterID, _, _ := unstructured.NestedString(bd.Object, "metadata", "labels", "fleet.cattle.io/cluster")
		if clusterID == "" {
			continue
		}
		state, _, _ := unstructured.NestedString(bd.Object, "status", "display", "state")
		message, _, _ := unstructured.NestedString(bd.Object, "status", "display", "message")

		phase := fleetStateToClusterPhase(state)
		if phase == aiplatformv1alpha1.AIWorkloadClusterPhaseRunning {
			message = ""
		}
		statuses = append(statuses, aiplatformv1alpha1.AIWorkloadClusterStatus{
			ClusterID: clusterID,
			Phase:     phase,
			Message:   message,
		})
	}

	w.Status.ClusterStatuses = statuses
	w.Status.Phase = guardPhaseTransition(derivePhase(statuses), w.Status.Phase, w.CreationTimestamp.Time)
	return nil
}

// ── Finalizer / deletion ──────────────────────────────────────────────────────

func (r *AIWorkloadReconciler) handleDeletion(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	switch w.Spec.DeployStrategy {
	case aiplatformv1alpha1.AIWorkloadDeployHelm:
		if w.Spec.Source.App != nil {
			done, res, err := r.handleHelmRelease(ctx, w)
			if !done {
				// Keep the finalizer: the Helm release still exists. Removing it
				// now is exactly the bug that orphaned the release in "uninstalling".
				return res, err
			}
		}
	case aiplatformv1alpha1.AIWorkloadDeployFleetBundle:
		for _, name := range w.Spec.FleetBundleNames {
			// Delete the HelmOp first so Fleet does not re-create the Bundle,
			// then delete the Bundle directly (Fleet links them by label only —
			// no ownerReference — so Fleet's own cleanup is unreliable).
			//
			// Keep the finalizer and retry (return the error) if either delete
			// fails: removing it now would leave the Bundle and its deployed
			// resources orphaned forever, which is the exact failure this fix
			// targets. Only delete the Bundle once the HelmOp delete has
			// succeeded — otherwise a still-live HelmOp could be reconciled and
			// re-generate the Bundle.
			if err := r.deleteHelmOp(ctx, name); err != nil {
				l.Error(err, "HelmOp delete failed — keeping finalizer, will retry", "name", name)
				return ctrl.Result{}, err
			}
			if err := r.deleteBundle(ctx, name); err != nil {
				l.Error(err, "Fleet Bundle delete failed — keeping finalizer, will retry", "name", name)
				return ctrl.Result{}, err
			}
		}
	case aiplatformv1alpha1.AIWorkloadDeployGitOps:
		// Delete only the git file — it is the source of truth. Fleet's GitRepo
		// controller then removes the generated HelmOp and Bundle. Do NOT delete
		// the Bundle directly here (as the FleetBundle case does): the git state
		// still references it, so Fleet would race to re-create it.
		for _, name := range w.Spec.FleetBundleNames {
			if err := r.deleteGitFileByName(ctx, w, name); err != nil {
				l.Error(err, "git file deletion failed — proceeding with finalizer removal", "name", name)
			}
		}
	}

	if err := r.cleanupPullSecretBundles(ctx, w); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.pruneLocalSAImagePullSecrets(ctx, w); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(w, aiWorkloadFinalizer)
	return ctrl.Result{}, r.Update(ctx, w)
}

// ── Phase derivation ──────────────────────────────────────────────────────────

// fleetStateToClusterPhase maps a Fleet BundleDeployment display state to our cluster phase.
// "Modified" means drift (e.g. a completed Job was cleaned up) — the workload is still running.
// Only "ErrApplied" is a true deployment failure.
func fleetStateToClusterPhase(state string) aiplatformv1alpha1.AIWorkloadClusterPhase {
	switch state {
	case "Ready", "Modified":
		return aiplatformv1alpha1.AIWorkloadClusterPhaseRunning
	case "ErrApplied":
		return aiplatformv1alpha1.AIWorkloadClusterPhaseFailed
	default:
		// Transient states (Pending, Progressing, WaitApplied, NotReady, "") — not yet failed.
		return aiplatformv1alpha1.AIWorkloadClusterPhasePending
	}
}

func derivePhase(statuses []aiplatformv1alpha1.AIWorkloadClusterStatus) aiplatformv1alpha1.AIWorkloadPhase {
	if len(statuses) == 0 {
		return aiplatformv1alpha1.AIWorkloadPhasePending
	}
	running, pending, failed := 0, 0, 0
	for _, s := range statuses {
		switch s.Phase {
		case aiplatformv1alpha1.AIWorkloadClusterPhaseRunning:
			running++
		case aiplatformv1alpha1.AIWorkloadClusterPhaseFailed:
			failed++
		default:
			pending++
		}
	}
	switch {
	case failed == 0 && pending == 0:
		return aiplatformv1alpha1.AIWorkloadPhaseRunning
	case failed == 0 && running == 0:
		// Nothing deployed yet — all clusters still in startup window.
		return aiplatformv1alpha1.AIWorkloadPhasePending
	case running == 0 && pending == 0:
		return aiplatformv1alpha1.AIWorkloadPhaseFailed
	default:
		// Covers running+pending (partially deployed), running+failed, and
		// pending+failed (no running). All surface as Degraded so the user
		// inspects per-cluster status.
		return aiplatformv1alpha1.AIWorkloadPhaseDegraded
	}
}

const initialDeployGracePeriod = 5 * time.Minute

// guardPhaseTransition prevents a workload from jumping directly to Failed
// when it has never reached Running. Transient Fleet errors during initial
// deployment would otherwise flash a "Failed" badge for a few seconds.
// After initialDeployGracePeriod the suppression expires so genuine failures
// are not hidden indefinitely.
func guardPhaseTransition(derived, current aiplatformv1alpha1.AIWorkloadPhase, createdAt time.Time) aiplatformv1alpha1.AIWorkloadPhase {
	if derived == aiplatformv1alpha1.AIWorkloadPhaseFailed {
		switch current {
		case aiplatformv1alpha1.AIWorkloadPhaseRunning, aiplatformv1alpha1.AIWorkloadPhaseDegraded, aiplatformv1alpha1.AIWorkloadPhaseFailed:
		default:
			if time.Since(createdAt) < initialDeployGracePeriod {
				return aiplatformv1alpha1.AIWorkloadPhasePending
			}
		}
	}
	return derived
}

// ── Watch mappers ─────────────────────────────────────────────────────────────

func (r *AIWorkloadReconciler) bundleDeploymentToAIWorkloads(ctx context.Context, obj client.Object) []reconcile.Request {
	bundleName := obj.GetLabels()["fleet.cattle.io/bundle-name"]
	if bundleName == "" {
		return nil
	}
	return r.workloadsWithFleetBundle(ctx, bundleName)
}

func (r *AIWorkloadReconciler) helmOpToAIWorkloads(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.workloadsWithFleetBundle(ctx, obj.GetName())
}

// bundleToAIWorkloads maps a Fleet Bundle to its owning AIWorkload — first by the workload-uid
// label (set on the generating HelmOp and copied by Fleet onto the Bundle), then by the
// bundle-name → FleetBundleNames lookup as a fallback for pre-labeled bundles.
func (r *AIWorkloadReconciler) bundleToAIWorkloads(ctx context.Context, obj client.Object) []reconcile.Request {
	if uid := obj.GetLabels()[workloadUIDLabel]; uid != "" {
		var list aiplatformv1alpha1.AIWorkloadList
		if err := r.List(ctx, &list); err == nil {
			for i := range list.Items {
				if string(list.Items[i].UID) == uid {
					return []reconcile.Request{{NamespacedName: types.NamespacedName{
						Name: list.Items[i].Name, Namespace: list.Items[i].Namespace}}}
				}
			}
		}
	}
	return r.workloadsWithFleetBundle(ctx, obj.GetName())
}

func (r *AIWorkloadReconciler) workloadsWithFleetBundle(ctx context.Context, bundleName string) []reconcile.Request {
	var list aiplatformv1alpha1.AIWorkloadList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for _, w := range list.Items {
		for _, name := range w.Spec.FleetBundleNames {
			if name == bundleName {
				reqs = append(reqs, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: w.Name, Namespace: w.Namespace},
				})
				break
			}
		}
	}
	return reqs
}

func (r *AIWorkloadReconciler) helmSecretToAIWorkloads(ctx context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	if labels["owner"] != "helm" {
		return nil
	}
	releaseName := labels["name"]
	if releaseName == "" {
		return nil
	}
	namespace := obj.GetNamespace()

	var list aiplatformv1alpha1.AIWorkloadList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for _, w := range list.Items {
		if w.Spec.DeployStrategy == aiplatformv1alpha1.AIWorkloadDeployHelm &&
			w.Spec.Source.App != nil &&
			w.Spec.Source.App.Release == releaseName &&
			w.Spec.TargetNamespace == namespace {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: w.Name, Namespace: w.Namespace},
			})
		}
	}
	return reqs
}

// credentialSecretToAIWorkloads re-enqueues every AIWorkload when a well-known
// registry credential secret (application-collection / nvidia-registry /
// suse-registry, including their aliases) in the operator namespace changes.
// The operator derives dockerconfigjson pull secrets (suse-ai-pull-combined,
// ngc-secret, ngc-api) from those source credentials and delivers them per
// workload (local SA-merge + downstream Fleet bundles); a key rotation must
// rebuild and re-deliver them. Without this, the SettingsReconciler refreshes
// the basic-auth ClusterRepo mirrors but the delivered pull secrets keep the
// old credentials. Enqueuing all workloads is safe — delivery is idempotent SSA.
func (r *AIWorkloadReconciler) credentialSecretToAIWorkloads(ctx context.Context, obj client.Object) []reconcile.Request {
	if obj.GetNamespace() != r.OperatorNamespace {
		return nil
	}
	if !credentials.IsWellKnownSecret(obj.GetName()) {
		return nil
	}
	return r.allAIWorkloadRequests(ctx)
}

// settingsToAIWorkloads re-enqueues every blueprint-sourced AIWorkload when
// Settings changes. The Settings controller rebuilds the Rancher catalog client
// (CatalogClient holder) from Settings.Spec.RancherCatalog at runtime; without
// this watch, a git-backed workload that failed with CatalogClientNotConfigured
// before the token was set would stay Failed until the next informer resync.
// Only blueprint-sourced workloads can consume git-backed ClusterRepos, so
// helm/app workloads are skipped; reconcile is idempotent so re-enqueuing the
// rest is safe.
func (r *AIWorkloadReconciler) settingsToAIWorkloads(ctx context.Context, _ client.Object) []reconcile.Request {
	var list aiplatformv1alpha1.AIWorkloadList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for i := range list.Items {
		if list.Items[i].Spec.Source.Blueprint == nil {
			continue
		}
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: list.Items[i].Name, Namespace: list.Items[i].Namespace},
		})
	}
	return reqs
}

func (r *AIWorkloadReconciler) allAIWorkloadRequests(ctx context.Context) []reconcile.Request {
	var list aiplatformv1alpha1.AIWorkloadList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: list.Items[i].Name, Namespace: list.Items[i].Namespace},
		})
	}
	return reqs
}

// ── Manager setup ─────────────────────────────────────────────────────────────

func (r *AIWorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	bd := &unstructured.Unstructured{}
	bd.SetGroupVersionKind(bundleDeploymentGVK)

	bundle := &unstructured.Unstructured{}
	bundle.SetGroupVersionKind(bundleGVK)

	helmOp := &unstructured.Unstructured{}
	helmOp.SetGroupVersionKind(helmOpGVK)

	isHelmSecret := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetLabels()["owner"] == "helm"
	})

	// A rotation of a well-known registry credential secret in the operator
	// namespace must re-deliver the dockerconfigjson pull secrets the operator
	// derives from it (suse-ai-pull-combined, ngc-secret, ngc-api), which only
	// happens on an AIWorkload reconcile. Mirrors the SettingsReconciler's
	// secret watch, which keeps the basic-auth ClusterRepo mirrors in lockstep
	// on the same rotation.
	isCredentialSecret := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetNamespace() == r.OperatorNamespace && credentials.IsWellKnownSecret(obj.GetName())
	})

	// Settings carries far more than the catalog config, and every field of it
	// is edited from the UI. Without this filter each unrelated edit (and each
	// status write the Settings controller itself makes) re-enqueues every
	// blueprint workload, which for git-backed components means re-downloading
	// their charts from Rancher. Only spec.rancherCatalog can change the
	// outcome of a git-chart reconcile, so that is all we react to.
	catalogSettingsChanged := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldS, ok1 := e.ObjectOld.(*aiplatformv1alpha1.Settings)
			newS, ok2 := e.ObjectNew.(*aiplatformv1alpha1.Settings)
			if !ok1 || !ok2 {
				return true
			}
			return !reflect.DeepEqual(oldS.Spec.RancherCatalog, newS.Spec.RancherCatalog)
		},
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&aiplatformv1alpha1.AIWorkload{}).
		Watches(bd, handler.EnqueueRequestsFromMapFunc(r.bundleDeploymentToAIWorkloads)).
		Watches(helmOp, handler.EnqueueRequestsFromMapFunc(r.helmOpToAIWorkloads)).
		Watches(bundle, handler.EnqueueRequestsFromMapFunc(r.bundleToAIWorkloads)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.helmSecretToAIWorkloads),
			builder.WithPredicates(isHelmSecret)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.credentialSecretToAIWorkloads),
			builder.WithPredicates(isCredentialSecret)).
		Watches(&aiplatformv1alpha1.Settings{}, handler.EnqueueRequestsFromMapFunc(r.settingsToAIWorkloads),
			builder.WithPredicates(catalogSettingsChanged)).
		Complete(r)
}
