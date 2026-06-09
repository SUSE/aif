package aiworkload

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiplatformv1alpha1 "github.com/SUSE/suse-ai-operator/api/v1alpha1"
	"github.com/SUSE/suse-ai-operator/internal/cluster"
)

// reconcilePullSecrets ensures every named pull-secret is merged into every
// ServiceAccount in the workload's target namespace on the operator's own
// cluster. Returns settled=true when no SA needed patching this round.
// The caller decides whether to RequeueAfter.
func (r *AIWorkloadReconciler) reconcilePullSecrets(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	secretNames []string,
) (settled bool, err error) {
	l := log.FromContext(ctx)

	if w.Spec.TargetNamespace == "" || len(secretNames) == 0 {
		return true, nil
	}

	var sas corev1.ServiceAccountList
	if err := r.List(ctx, &sas, client.InNamespace(w.Spec.TargetNamespace)); err != nil {
		return false, fmt.Errorf("list ServiceAccounts in %s: %w", w.Spec.TargetNamespace, err)
	}

	settled = true
	for i := range sas.Items {
		sa := &sas.Items[i]
		if mergeImagePullSecrets(sa, secretNames) {
			if err := r.Update(ctx, sa); err != nil {
				return false, fmt.Errorf("update SA %s/%s: %w", sa.Namespace, sa.Name, err)
			}
			l.Info("merged pull secrets into ServiceAccount",
				"namespace", sa.Namespace, "name", sa.Name, "secrets", secretNames)
			settled = false
		}
	}

	// After SA mutations, bounce any pod stuck in ImagePullBackOff so the
	// kubelet re-reads the SA's imagePullSecrets at admission time.
	bounced, err := r.restartImagePullBackOffPods(ctx, w.Spec.TargetNamespace)
	if err != nil {
		return false, err
	}
	if bounced > 0 {
		settled = false
	}

	return settled, nil
}

// mergeImagePullSecrets adds each name to sa.ImagePullSecrets if not already
// present. Returns true if the SA was mutated. Order: existing entries first
// (preserved verbatim), then any new names in input order; duplicates in the
// input list are added once.
func mergeImagePullSecrets(sa *corev1.ServiceAccount, names []string) bool {
	have := make(map[string]struct{}, len(sa.ImagePullSecrets))
	for _, ref := range sa.ImagePullSecrets {
		have[ref.Name] = struct{}{}
	}
	mutated := false
	for _, name := range names {
		if _, ok := have[name]; ok {
			continue
		}
		sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		have[name] = struct{}{}
		mutated = true
	}
	return mutated
}

// restartImagePullBackOffPods deletes pods in `namespace` whose container
// statuses report ImagePullBackOff or ErrImagePull. The pod's controller
// (Deployment, StatefulSet, ReplicaSet, DaemonSet, Job) recreates it; the
// recreated pod picks up its ServiceAccount's current .imagePullSecrets at
// admission time. Returns the count of pods deleted.
func (r *AIWorkloadReconciler) restartImagePullBackOffPods(ctx context.Context, namespace string) (int, error) {
	l := log.FromContext(ctx)

	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(namespace)); err != nil {
		return 0, fmt.Errorf("list pods in %s: %w", namespace, err)
	}

	bounced := 0
	for i := range pods.Items {
		p := &pods.Items[i]
		if !isPodImagePullBackOff(p) {
			continue
		}
		if err := r.Delete(ctx, p); err != nil {
			if client.IgnoreNotFound(err) == nil {
				continue
			}
			return bounced, fmt.Errorf("delete pod %s/%s: %w", p.Namespace, p.Name, err)
		}
		l.Info("bounced ImagePullBackOff pod", "namespace", p.Namespace, "name", p.Name)
		bounced++
	}
	return bounced, nil
}

func isPodImagePullBackOff(p *corev1.Pod) bool {
	for _, cs := range p.Status.InitContainerStatuses {
		if waitingIsImagePullFailure(cs.State.Waiting) {
			return true
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		if waitingIsImagePullFailure(cs.State.Waiting) {
			return true
		}
	}
	return false
}

func waitingIsImagePullFailure(w *corev1.ContainerStateWaiting) bool {
	if w == nil {
		return false
	}
	switch w.Reason {
	case "ImagePullBackOff", "ErrImagePull":
		return true
	}
	return false
}

// mergePullSecretNames adds each name from add to existing if not already
// present. Used to accumulate secret names from per-component injector runs
// onto AIWorkload.Status.PullSecretNames.
func mergePullSecretNames(existing, add []string) []string {
	if len(add) == 0 {
		return existing
	}
	have := make(map[string]struct{}, len(existing))
	for _, n := range existing {
		have[n] = struct{}{}
	}
	out := existing
	for _, n := range add {
		if _, ok := have[n]; ok {
			continue
		}
		out = append(out, n)
		have[n] = struct{}{}
	}
	return out
}

// PullSecretFactory builds a Secret object (without writing it) for a given
// target namespace and secret name. The injector/caller supplies this so
// deliverPullSecrets stays agnostic to credential plumbing. Returning
// (nil, nil) signals "credentials not configured — skip this secret"; the
// caller treats this as a no-op rather than an error.
type PullSecretFactory func(targetNamespace, secretName string) (*corev1.Secret, error)

// deliverPullSecrets ensures the secret names listed in
// w.Status.PullSecretNames are delivered to:
//   - the operator's own cluster (always, in w.Spec.TargetNamespace),
//   - each downstream cluster in w.Spec.TargetClusters (via Fleet Bundle).
//
// The "local" string in TargetClusters is skipped on the downstream loop
// because it's already covered by the unconditional local-write — emitting
// a Fleet Bundle for "local" would duplicate delivery.
func (r *AIWorkloadReconciler) deliverPullSecrets(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	factory PullSecretFactory,
) error {
	if w.Spec.TargetNamespace == "" || len(w.Status.PullSecretNames) == 0 || factory == nil {
		return nil
	}

	// Stop on first error; the next reconcile retries from a clean state.
	apply := func(cc cluster.Client, name, where string) error {
		sec, err := factory(w.Spec.TargetNamespace, name)
		if err != nil {
			return fmt.Errorf("build pull secret %s for %s: %w", name, where, err)
		}
		if sec == nil {
			return nil // creds not configured — skip
		}
		if err := cc.ApplySecret(ctx, sec); err != nil {
			return fmt.Errorf("apply pull secret %s to %s: %w", name, where, err)
		}
		return nil
	}

	// Local cluster — always.
	local := r.localCC()
	for _, name := range w.Status.PullSecretNames {
		if err := apply(local, name, "local cluster"); err != nil {
			return err
		}
	}

	// Downstream — one Bundle per cluster ID × per secret name. Skip "local"
	// (already covered above) and empty entries.
	for _, clusterID := range w.Spec.TargetClusters {
		if clusterID == "" || clusterID == "local" {
			continue
		}
		bc := cluster.NewBundleClient(r.Client, r.Scheme, cluster.BundleClientOptions{
			ClusterID:      clusterID,
			FleetWorkspace: "fleet-default",
			OwnerName:      w.Name,
			OwnerNamespace: w.Namespace,
		})
		for _, name := range w.Status.PullSecretNames {
			if err := apply(bc, name, "cluster "+clusterID); err != nil {
				return err
			}
		}
	}

	return nil
}

// pullSecretFactory returns a PullSecretFactory that produces the
// dockerconfigjson and API-key Secrets the operator delivers. Returning
// (nil, nil) means "credentials not configured — skip"; deliverPullSecrets
// treats this as a no-op.
//
// Today only the NVIDIA-owned secret names (ngc-secret, ngc-api) are
// recognized — these are the names nvidiaInjector creates and persists onto
// Status.PullSecretNames. The suse-vendor combined pull secret stays
// local-only (its content is per-cluster Settings-derived); names that
// don't match anything below are skipped via (nil, nil).
func (r *AIWorkloadReconciler) pullSecretFactory(ctx context.Context) PullSecretFactory {
	return func(targetNamespace, secretName string) (*corev1.Secret, error) {
		switch secretName {
		case nvidiaImagePullSecretName:
			cfg, err := r.buildNGCDockerConfig(ctx)
			if err != nil {
				return nil, err
			}
			if cfg == nil {
				return nil, nil
			}
			return &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: targetNamespace},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data:       map[string][]byte{corev1.DockerConfigJsonKey: cfg},
			}, nil
		case nvidiaAPISecretName:
			// Re-read the token via the same Settings path so we don't have
			// to plumb it back from buildNGCDockerConfig.
			var s aiplatformv1alpha1.Settings
			if err := r.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: operatorSettingsName}, &s); err != nil {
				return nil, nil
			}
			if s.Spec.Nvidia.TokenSecretRef == nil {
				return nil, nil
			}
			token, err := r.readSettingsSecretKey(ctx, s.Spec.Nvidia.TokenSecretRef)
			if err != nil || token == "" {
				return nil, nil
			}
			return &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: targetNamespace},
				Type:       corev1.SecretTypeOpaque,
				Data:       ngcAPISecretData(token),
			}, nil
		default:
			// Unknown secret name (e.g. the suse combined secret); skip.
			// The existing local-only injector path already handled it.
			return nil, nil
		}
	}
}

// cleanupPullSecretBundles deletes every Fleet Bundle the operator created
// on behalf of this AIWorkload, identified by the owner-name / owner-namespace
// labels bundleClient applies on every Bundle it emits. Fleet removes the
// projected Secrets on downstream clusters when the Bundle goes away. List
// is cluster-scoped (no namespace selector) so the cleanup catches bundles
// regardless of which Fleet workspace they ended up in.
func (r *AIWorkloadReconciler) cleanupPullSecretBundles(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
) error {
	l := log.FromContext(ctx)

	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"})
	selector := client.MatchingLabels{
		"ai-platform.suse.com/owner-name":      w.Name,
		"ai-platform.suse.com/owner-namespace": w.Namespace,
	}
	if err := r.List(ctx, &bundles, selector); err != nil {
		return fmt.Errorf("list pull-secret bundles for %s/%s: %w", w.Namespace, w.Name, err)
	}
	for i := range bundles.Items {
		b := &bundles.Items[i]
		if err := r.Delete(ctx, b); err != nil && client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete bundle %s/%s: %w", b.GetNamespace(), b.GetName(), err)
		}
		l.Info("deleted pull-secret bundle", "namespace", b.GetNamespace(), "name", b.GetName())
	}
	return nil
}

// pruneLocalSAImagePullSecrets removes the workload's pull-secret entries
// from every ServiceAccount in the target namespace on the operator's own
// cluster. Non-workload entries (e.g. a pre-existing "regcred") are
// preserved. Used by the finalizer when the AIWorkload is deleted; pairs
// with cleanupPullSecretBundles which handles downstream pruning via Fleet.
//
// Returns nil (no-op) when Status.PullSecretNames is empty: nothing was
// ever injected, so there's nothing to prune. A pre-status-write crash
// before any successful reconcile is the only way to reach this branch
// with stale SA entries — and in that case there are no SA entries either,
// because injection persists status and SA-merge in the same reconcile.
func (r *AIWorkloadReconciler) pruneLocalSAImagePullSecrets(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
) error {
	if w.Spec.TargetNamespace == "" || len(w.Status.PullSecretNames) == 0 {
		return nil
	}
	ours := make(map[string]struct{}, len(w.Status.PullSecretNames))
	for _, n := range w.Status.PullSecretNames {
		ours[n] = struct{}{}
	}

	var sas corev1.ServiceAccountList
	if err := r.List(ctx, &sas, client.InNamespace(w.Spec.TargetNamespace)); err != nil {
		return fmt.Errorf("list SAs in %s: %w", w.Spec.TargetNamespace, err)
	}
	for i := range sas.Items {
		sa := &sas.Items[i]
		kept := sa.ImagePullSecrets[:0]
		mutated := false
		for _, ref := range sa.ImagePullSecrets {
			if _, isOurs := ours[ref.Name]; isOurs {
				mutated = true
				continue
			}
			kept = append(kept, ref)
		}
		if !mutated {
			continue
		}
		sa.ImagePullSecrets = kept
		if err := r.Update(ctx, sa); err != nil {
			return fmt.Errorf("update SA %s/%s: %w", sa.Namespace, sa.Name, err)
		}
	}
	return nil
}
