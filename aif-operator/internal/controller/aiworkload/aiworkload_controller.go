package aiworkload

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/action"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	log "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const aiWorkloadFinalizer = "ai-platform.suse.com/cleanup"

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
	RestConfig        *rest.Config
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=aiworkloads,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=aiworkloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=aiworkloads/finalizers,verbs=update
// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=settings,verbs=get;list;watch
// +kubebuilder:rbac:groups=fleet.cattle.io,resources=bundledeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=fleet.cattle.io,resources=bundles,verbs=get;list;delete
// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=blueprints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=clusterrepos,verbs=get;list;watch
// +kubebuilder:rbac:groups=fleet.cattle.io,resources=helmops,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods;services;configmaps;serviceaccounts;persistentvolumeclaims,verbs=get;list;delete
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;replicasets;daemonsets,verbs=get;list;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=create;get;patch

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

	result, err := r.reconcileStatus(ctx, &w)
	if err != nil {
		return ctrl.Result{}, err
	}
	if result.Requeue || result.RequeueAfter > 0 {
		return result, nil
	}

	w.Status.ObservedGeneration = w.Generation
	if err := r.Status().Update(ctx, &w); err != nil {
		// The object may have been deleted by reconcileGitOpsStatus (HelmOp gone path).
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	l.Info("reconciled AIWorkload", "phase", w.Status.Phase)
	return ctrl.Result{}, nil
}

// reconcileStatus dispatches to the strategy-specific status reconciler.
func (r *AIWorkloadReconciler) reconcileStatus(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) (ctrl.Result, error) {
	if w.Spec.Source.SourceType == aiplatformv1alpha1.AIWorkloadSourceBlueprint {
		return r.reconcileBlueprintStatus(ctx, w)
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

// uninstallHelm uses the Helm SDK to fully uninstall a release and its deployed resources.
func (r *AIWorkloadReconciler) uninstallHelm(namespace, releaseName string) error {
	getter := newRESTClientGetter(r.RestConfig, namespace)
	cfg := new(action.Configuration)
	if err := cfg.Init(getter, namespace, "secret", func(string, ...interface{}) {}); err != nil {
		return fmt.Errorf("helm init: %w", err)
	}
	u := action.NewUninstall(cfg)
	u.IgnoreNotFound = true
	if _, err := u.Run(releaseName); err != nil {
		return fmt.Errorf("helm uninstall %s/%s: %w", namespace, releaseName, err)
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
		state, _, _   := unstructured.NestedString(bd.Object, "status", "display", "state")
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
			if err := r.uninstallHelm(w.Spec.TargetNamespace, w.Spec.Source.App.Release); err != nil {
				l.Error(err, "helm uninstall failed — proceeding with finalizer removal")
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

// ── Manager setup ─────────────────────────────────────────────────────────────

func (r *AIWorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	bd := &unstructured.Unstructured{}
	bd.SetGroupVersionKind(bundleDeploymentGVK)

	helmOp := &unstructured.Unstructured{}
	helmOp.SetGroupVersionKind(helmOpGVK)

	isHelmSecret := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetLabels()["owner"] == "helm"
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&aiplatformv1alpha1.AIWorkload{}).
		Watches(bd, handler.EnqueueRequestsFromMapFunc(r.bundleDeploymentToAIWorkloads)).
		Watches(helmOp, handler.EnqueueRequestsFromMapFunc(r.helmOpToAIWorkloads)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.helmSecretToAIWorkloads),
			builder.WithPredicates(isHelmSecret)).
		Complete(r)
}
