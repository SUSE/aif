package aiworkload

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// reconcileAppPullSecrets runs the vendor-specific secret injector for an
// App-sourced AIWorkload so the resulting secret names end up on
// w.Status.PullSecretDeliveries (per-namespace bucket). Unlike the
// Blueprint path, this does NOT
// create or patch any HelmOp — App-sourced workloads have their HelmOp
// (or direct Helm release) written by the UI, not the operator.
//
// The flow is intentionally minimal: resolve the chart's ClusterRepo,
// hand control to the matching injector, and merge whatever secret
// names it persisted into Status. The existing post-reconcile block in
// aiworkload_controller.go then takes over: deliverPullSecrets ships
// the secrets to local + each downstream cluster (one consolidated
// Bundle per cluster, with an SA-merge Job), and reconcilePullSecrets
// merges the names into every ServiceAccount on the local cluster.
//
// Pod-spec values injection (e.g. nvidia chart values.imagePullSecrets)
// happens inside the injector via the vals map argument. We pass an
// empty map and discard the mutated result here because the App's
// HelmOp values are owned by the UI — overwriting them from the
// operator would race with the UI's writes. The SA-merge Job covers
// the case where a chart-created pod doesn't reference
// imagePullSecrets explicitly: the kubelet pulls from the SA's
// defaulted list instead.
func (r *AIWorkloadReconciler) reconcileAppPullSecrets(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
) error {
	src := w.Spec.Source.App
	if src == nil || w.Spec.TargetNamespace == "" {
		return nil
	}

	// Resolve the ClusterRepo for the chart so the suseInjector can
	// pick up the repo's own basic-auth credentials when assembling the
	// combined pull secret. The nvidiaInjector ignores repoInfo (it
	// reads NVIDIA Settings directly).
	//
	// Fail soft when the ClusterRepo is missing or the catalog.cattle.io
	// API is unavailable: pull-secret injection is an enhancement, not a
	// precondition, and the rest of the App reconcile (status mirroring,
	// finalizer, etc.) must keep running. Other resolve errors (network,
	// permission) propagate so we don't silently mask infrastructure
	// problems.
	repoInfo, err := r.resolveClusterRepo(ctx, src.ChartRepo)
	if err != nil {
		if errors.IsNotFound(err) || meta.IsNoMatchError(err) {
			log.FromContext(ctx).Info("App pull-secret injection skipped: ClusterRepo not available",
				"chartRepo", src.ChartRepo, "reason", err.Error())
			return nil
		}
		return fmt.Errorf("resolve ClusterRepo %q for App workload %s/%s: %w",
			src.ChartRepo, w.Namespace, w.Name, err)
	}

	// vals is intentionally discarded — see function comment.
	vals := map[string]any{}
	created, err := r.injectorFor(src.Vendor).Apply(ctx, r.localCC(), w.Spec.TargetNamespace, repoInfo, vals)
	if err != nil {
		return fmt.Errorf("inject pull secrets for App workload %s/%s (vendor=%q): %w",
			w.Namespace, w.Name, src.Vendor, err)
	}
	w.Status.PullSecretDeliveries = mergePullSecretDelivery(w.Status.PullSecretDeliveries, w.Spec.TargetNamespace, created)
	return nil
}
