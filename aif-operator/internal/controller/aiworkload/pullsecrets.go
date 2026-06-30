package aiworkload

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/cluster"
	"github.com/SUSE/aif-operator/internal/credentials"
)

// reconcilePullSecrets ensures every operator-delivered pull-secret is
// merged into every ServiceAccount in the namespace(s) the workload installs
// into on the operator's own cluster. Walks Status.PullSecretDeliveries so
// blueprints that fan components out across multiple namespaces patch each
// namespace's SAs independently. Returns settled=true when no SA needed
// patching this round; the caller decides whether to RequeueAfter.
// targetsLocalCluster reports whether the workload should act on the operator's
// own (local) cluster. An empty TargetClusters list means the local-default
// install; an explicit "local" or "" entry also counts. A purely downstream
// list does not, so the local-cluster paths (secret write + SA-merge) are
// skipped for those workloads.
func targetsLocalCluster(w *aiplatformv1alpha1.AIWorkload) bool {
	if len(w.Spec.TargetClusters) == 0 {
		return true
	}
	for _, c := range w.Spec.TargetClusters {
		if c == "" || c == "local" {
			return true
		}
	}
	return false
}

func (r *AIWorkloadReconciler) reconcilePullSecrets(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
) (settled bool, err error) {
	l := log.FromContext(ctx)

	if len(w.Status.PullSecretDeliveries) == 0 {
		return true, nil
	}

	// SA-merge runs on the operator's own (local) cluster. Skip it entirely
	// when the workload targets only downstream clusters — otherwise we'd
	// patch local ServiceAccounts in a same-named namespace the workload never
	// installs into here (review #4). Downstream SA-merge is handled by the
	// per-cluster Fleet bundle's own merge Job.
	if !targetsLocalCluster(w) {
		return true, nil
	}

	settled = true
	for _, d := range w.Status.PullSecretDeliveries {
		if d.Namespace == "" || len(d.Names) == 0 {
			continue
		}
		nsSettled, err := r.reconcilePullSecretsForNamespace(ctx, d.Namespace, d.Names, l)
		if err != nil {
			return false, err
		}
		if !nsSettled {
			settled = false
		}
	}

	return settled, nil
}

// reconcilePullSecretsForNamespace handles a single (namespace, names) pair:
// list chart-managed SAs in the namespace, merge the missing pull-secret
// references, then bounce any chart-managed pod still stuck in
// ImagePullBackOff so the kubelet re-reads the SA's imagePullSecrets at
// admission time.
//
// Owner scope: SAs and pods are filtered by label
// app.kubernetes.io/managed-by=Helm so the operator does NOT touch
// cluster-admin-created resources that happen to share the namespace.
//
// Retry bound: the pod-bounce step caps restarts per pod-owner at
// chartPodMaxBounces — see restartImagePullBackOffPods. Genuinely unpullable
// images surface as a permanent ImagePullBackOff rather than churning.
func (r *AIWorkloadReconciler) reconcilePullSecretsForNamespace(
	ctx context.Context,
	namespace string,
	secretNames []string,
	l logr.Logger,
) (settled bool, err error) {
	var sas corev1.ServiceAccountList
	if err := r.List(ctx, &sas,
		client.InNamespace(namespace),
		client.MatchingLabels{chartManagedByLabel: chartManagedByHelm},
	); err != nil {
		return false, fmt.Errorf("list ServiceAccounts in %s: %w", namespace, err)
	}

	// Also include the namespace "default" SA. Charts (and bundled subcharts
	// like the bitnami postgresql dependency of litellm) frequently run pods
	// under "default" rather than a chart-created SA; those pods still need the
	// pull secrets (e.g. a SUSE-registry chart whose subchart pulls images from
	// AppCollection). mergeImagePullSecrets is a union, so patching "default"
	// only adds entries and never clobbers existing ones.
	var def corev1.ServiceAccount
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "default"}, &def); err == nil {
		listed := false
		for i := range sas.Items {
			if sas.Items[i].Name == "default" {
				listed = true
				break
			}
		}
		if !listed {
			sas.Items = append(sas.Items, def)
		}
	} else if client.IgnoreNotFound(err) != nil {
		return false, fmt.Errorf("get default ServiceAccount in %s: %w", namespace, err)
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

	bounced, err := r.restartImagePullBackOffPods(ctx, namespace)
	if err != nil {
		return false, err
	}
	if bounced > 0 {
		settled = false
	}
	return settled, nil
}

// chartManagedByLabel + chartManagedByHelm define the label-selector used to
// scope SA and pod operations to chart-managed resources (the standard Helm
// label). Cluster-admin-created SAs and pods are intentionally left alone.
const (
	chartManagedByLabel = "app.kubernetes.io/managed-by"
	chartManagedByHelm  = "Helm"

	// chartPodBounceAnnotation is incremented on a pod's owning controller
	// (ReplicaSet, StatefulSet, DaemonSet, Job, …) every time the operator
	// bounces a pod owned by that controller. It is read to decide whether
	// to keep bouncing or give up.
	chartPodBounceAnnotation = "ai-platform.suse.com/pull-secret-bounce-count"
	// chartPodMaxBounces is the hard cap per controller. Once reached, the
	// operator stops bouncing pods of that controller — the failure
	// (ImagePullBackOff) stays visible so the user can investigate.
	// New ReplicaSets (created by a Deployment spec.template change) start
	// at 0 again, so a chart upgrade naturally resets the counter.
	chartPodMaxBounces = 3
)

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

// restartImagePullBackOffPods deletes chart-managed pods in `namespace`
// whose container statuses report ImagePullBackOff or ErrImagePull. The
// pod's controller (Deployment-owned ReplicaSet, StatefulSet, DaemonSet,
// Job) recreates it; the recreated pod picks up its ServiceAccount's
// current .imagePullSecrets at admission time.
//
// Owner scope: only pods labeled app.kubernetes.io/managed-by=Helm are
// considered — pods unrelated to a chart install aren't churned.
//
// Retry bound: each bounce increments an annotation
// (chartPodBounceAnnotation) on the pod's controllerRef. Once the count
// reaches chartPodMaxBounces, this function stops bouncing pods of that
// controller — leaving the failure visible as a persistent
// ImagePullBackOff rather than masking it with churn. A chart upgrade
// (Deployment spec.template change → new ReplicaSet) naturally resets the
// counter because the new RS object has no annotation yet. Pods without a
// controllerRef are skipped (no place to persist the counter).
//
// Returns the count of pods deleted this pass.
func (r *AIWorkloadReconciler) restartImagePullBackOffPods(ctx context.Context, namespace string) (int, error) {
	l := log.FromContext(ctx)

	var pods corev1.PodList
	if err := r.List(ctx, &pods,
		client.InNamespace(namespace),
		client.MatchingLabels{chartManagedByLabel: chartManagedByHelm},
	); err != nil {
		return 0, fmt.Errorf("list pods in %s: %w", namespace, err)
	}

	bounced := 0
	for i := range pods.Items {
		p := &pods.Items[i]
		if !isPodImagePullBackOff(p) {
			continue
		}
		cr := metav1.GetControllerOf(p)
		if cr == nil {
			l.Info("skipping ImagePullBackOff pod with no controllerRef (cannot track retries)",
				"namespace", p.Namespace, "name", p.Name)
			continue
		}
		count, owner, err := r.readBounceCount(ctx, p.Namespace, cr)
		if err != nil {
			l.Info("could not read bounce counter for controller; will not bounce this round",
				"namespace", p.Namespace, "pod", p.Name,
				"controllerKind", cr.Kind, "controllerName", cr.Name, "err", err.Error())
			continue
		}
		if count >= chartPodMaxBounces {
			l.Info("bounce cap reached for controller; leaving pod in ImagePullBackOff so the failure is visible",
				"namespace", p.Namespace, "pod", p.Name,
				"controllerKind", cr.Kind, "controllerName", cr.Name,
				"cap", chartPodMaxBounces)
			continue
		}
		// Increment the controller's counter BEFORE deleting the pod, so
		// a transient delete failure doesn't double-count, and the cap is
		// enforced even if this pass partially fails.
		if err := r.incrementBounceCount(ctx, owner, count+1); err != nil {
			return bounced, fmt.Errorf("increment bounce counter on %s/%s: %w", owner.GetKind(), owner.GetName(), err)
		}
		if err := r.Delete(ctx, p); err != nil {
			if client.IgnoreNotFound(err) == nil {
				continue
			}
			return bounced, fmt.Errorf("delete pod %s/%s: %w", p.Namespace, p.Name, err)
		}
		l.Info("bounced ImagePullBackOff pod",
			"namespace", p.Namespace, "name", p.Name,
			"controllerKind", cr.Kind, "controllerName", cr.Name,
			"bounce", count+1, "cap", chartPodMaxBounces)
		bounced++
	}
	return bounced, nil
}

// readBounceCount fetches the pod's owning controller (any Kind) via an
// unstructured Get and returns the integer value of
// chartPodBounceAnnotation, plus the owner object itself so the caller can
// pass it back to incrementBounceCount without a second fetch.
// Returns (0, owner, nil) when the annotation is absent or unparsable.
func (r *AIWorkloadReconciler) readBounceCount(ctx context.Context, namespace string, cr *metav1.OwnerReference) (int, *unstructured.Unstructured, error) {
	gv, err := schema.ParseGroupVersion(cr.APIVersion)
	if err != nil {
		return 0, nil, fmt.Errorf("parse controller apiVersion %q: %w", cr.APIVersion, err)
	}
	owner := &unstructured.Unstructured{}
	owner.SetGroupVersionKind(gv.WithKind(cr.Kind))
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.Name}, owner); err != nil {
		return 0, nil, err
	}
	val := owner.GetAnnotations()[chartPodBounceAnnotation]
	if val == "" {
		return 0, owner, nil
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		// Bad value — treat as 0 and let the next write overwrite it.
		return 0, owner, nil
	}
	return n, owner, nil
}

// incrementBounceCount writes newCount onto the owner's
// chartPodBounceAnnotation. Uses a strategic-merge patch on the annotation
// only — avoids the spec-level conflict potential of a full Update on an
// unstructured object the operator doesn't own.
func (r *AIWorkloadReconciler) incrementBounceCount(ctx context.Context, owner *unstructured.Unstructured, newCount int) error {
	patch := []byte(fmt.Sprintf(
		`{"metadata":{"annotations":{%q:%q}}}`,
		chartPodBounceAnnotation, strconv.Itoa(newCount),
	))
	return r.Patch(ctx, owner, client.RawPatch(types.MergePatchType, patch))
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

// mergePullSecretDelivery merges (namespace, names) into existing — appending
// names into the matching namespace bucket if one already exists, or appending
// a new bucket otherwise. Names within a bucket are deduped (first-seen wins);
// the input order is preserved. Used to accumulate per-component injector
// outputs onto AIWorkload.Status.PullSecretDeliveries when a blueprint fans
// components out across multiple namespaces.
func mergePullSecretDelivery(existing []aiplatformv1alpha1.PullSecretDelivery, namespace string, names []string) []aiplatformv1alpha1.PullSecretDelivery {
	if namespace == "" || len(names) == 0 {
		return existing
	}
	for i := range existing {
		if existing[i].Namespace == namespace {
			existing[i].Names = mergeStringSet(existing[i].Names, names)
			return existing
		}
	}
	return append(existing, aiplatformv1alpha1.PullSecretDelivery{
		Namespace: namespace,
		Names:     mergeStringSet(nil, names),
	})
}

// mergeStringSet returns the union of existing + add, preserving order and
// deduping by membership in existing first then add.
func mergeStringSet(existing, add []string) []string {
	have := make(map[string]struct{}, len(existing)+len(add))
	for _, n := range existing {
		have[n] = struct{}{}
	}
	out := append([]string(nil), existing...)
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

// deliverPullSecrets ensures every (namespace, names) bucket in
// w.Status.PullSecretDeliveries is delivered to:
//   - the operator's own cluster (always), one SSA per secret via the local
//     client into the bucket's namespace,
//   - each downstream cluster in w.Spec.TargetClusters, as one consolidated
//     Fleet Bundle per (cluster, namespace) — see cluster.BundleClient for
//     why we package the namespace + secrets + SA-merge Job together.
//
// Blueprint workloads with components in multiple namespaces produce one
// bucket per distinct namespace and therefore multiple Bundles per downstream
// cluster (each Bundle's identity is `ai-pullsecrets-<owner>-<cluster>-<ns>`).
//
// The "local" string in TargetClusters is skipped on the downstream loop
// because it's already covered by the unconditional local-write.
func (r *AIWorkloadReconciler) deliverPullSecrets(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	factory PullSecretFactory,
) error {
	if len(w.Status.PullSecretDeliveries) == 0 || factory == nil {
		return nil
	}

	// Pre-build every secret per (namespace, names) so each round of factory
	// calls produces both the local view and the downstream view; a nil
	// result from the factory means "creds not configured — skip".
	type nsBundle struct {
		namespace string
		secrets   []*corev1.Secret
	}
	bundles := make([]nsBundle, 0, len(w.Status.PullSecretDeliveries))
	for _, d := range w.Status.PullSecretDeliveries {
		if d.Namespace == "" {
			continue
		}
		secrets := make([]*corev1.Secret, 0, len(d.Names))
		for _, name := range d.Names {
			sec, err := factory(d.Namespace, name)
			if err != nil {
				return fmt.Errorf("build pull secret %s for %s: %w", name, d.Namespace, err)
			}
			if sec == nil {
				continue
			}
			secrets = append(secrets, sec)
		}
		if len(secrets) > 0 {
			bundles = append(bundles, nsBundle{namespace: d.Namespace, secrets: secrets})
		}
	}
	if len(bundles) == 0 {
		return nil
	}

	// Local cluster — only when the workload actually targets it. A
	// downstream-only workload must not write secrets into a same-named local
	// namespace it never installs into here (review #4).
	//
	// Note (review #5): for a local-targeted workload the per-component
	// injectors already wrote these secrets during reconcile; re-applying them
	// here is deliberate, idempotent SSA. The injector's combined secret also
	// carries the component's chart-repo auth, which this delivery copy omits —
	// harmless, since the chart-repo host is not an image registry.
	if targetsLocalCluster(w) {
		local := r.localCC()
		for _, b := range bundles {
			for _, sec := range b.secrets {
				if err := local.ApplySecret(ctx, sec); err != nil {
					return fmt.Errorf("apply pull secret %s/%s to local cluster: %w", b.namespace, sec.Name, err)
				}
			}
		}
	}

	// Downstream — one consolidated Bundle per (cluster, namespace). Skip
	// "local" and empty entries.
	for _, clusterID := range w.Spec.TargetClusters {
		if clusterID == "" || clusterID == "local" {
			continue
		}
		for _, b := range bundles {
			bc := cluster.NewBundleClient(r.Client, r.Scheme, cluster.BundleClientOptions{
				ClusterID:      clusterID,
				FleetWorkspace: "fleet-default",
				OwnerName:      w.Name,
				OwnerNamespace: w.Namespace,
				Namespace:      b.namespace,
			})
			if err := bc.ApplyPullSecretBundle(ctx, b.secrets); err != nil {
				return fmt.Errorf("apply pull-secret bundle to cluster %s ns %s: %w", clusterID, b.namespace, err)
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
// The factory recognizes the three secret names the operator's injectors
// persist onto Status.PullSecretDeliveries[].Names:
//   - ngc-secret              (nvidiaInjector dockerconfigjson)
//   - ngc-api                 (nvidiaInjector Opaque, NGC API keys)
//   - suse-ai-pull-combined   (suseInjector combined dockerconfigjson)
//
// Unknown secret names skip silently — keeps the factory forward-compatible
// with future injector outputs without coupling deliverPullSecrets to a
// hard-coded enum.
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
			var s aiplatformv1alpha1.Settings
			if err := r.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: operatorSettingsName}, &s); err != nil {
				return nil, nil
			}
			_, token, ok, err := r.readRegistryCredentials(ctx, credentials.RegistryNvidia, s.Spec.Nvidia.UserSecretRef, s.Spec.Nvidia.TokenSecretRef)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, nil
			}
			return &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: targetNamespace},
				Type:       corev1.SecretTypeOpaque,
				Data:       ngcAPISecretData(token),
			}, nil
		case combinedPullSecretName:
			// SUSE-vendor downstream delivery. buildSUSECombinedDockerConfig
			// pulls auths from Settings (AppCollection, SUSE Registry, NVIDIA)
			// — no per-component chart-repo entry, because pull-secrets
			// authenticate IMAGE pulls, not chart pulls (Fleet authenticates
			// the chart pull separately via its own helmSecretName).
			cfg, err := r.buildSUSECombinedDockerConfig(ctx)
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
		default:
			// Unknown secret name — silently skip for forward compatibility.
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
// from every ServiceAccount in every namespace the workload installed into,
// on the operator's own cluster. Non-workload entries (e.g. a pre-existing
// "regcred") are preserved. Used by the finalizer when the AIWorkload is
// deleted; pairs with cleanupPullSecretBundles which handles downstream
// pruning via Fleet.
//
// Returns nil (no-op) when Status.PullSecretDeliveries is empty: nothing
// was ever injected, so there's nothing to prune.
func (r *AIWorkloadReconciler) pruneLocalSAImagePullSecrets(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
) error {
	for _, d := range w.Status.PullSecretDeliveries {
		if d.Namespace == "" || len(d.Names) == 0 {
			continue
		}
		ours := make(map[string]struct{}, len(d.Names))
		for _, n := range d.Names {
			ours[n] = struct{}{}
		}

		var sas corev1.ServiceAccountList
		if err := r.List(ctx, &sas, client.InNamespace(d.Namespace)); err != nil {
			return fmt.Errorf("list SAs in %s: %w", d.Namespace, err)
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
	}
	return nil
}
