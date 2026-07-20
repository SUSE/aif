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
	"encoding/base64"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/cluster"
	"github.com/SUSE/aif-operator/internal/credentials"
	igit "github.com/SUSE/aif-operator/internal/git"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
	"github.com/SUSE/aif-operator/internal/naming"
	"github.com/SUSE/aif-operator/internal/registryurl"
)

var clusterRepoGVK = schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"}

// repoKind classifies how a Rancher ClusterRepo serves its charts.
type repoKind string

const (
	repoKindHTTP repoKind = "http"
	repoKindOCI  repoKind = "oci"
	repoKindGit  repoKind = "git"
)

// errClusterRepoNotReady marks a ClusterRepo lookup that failed because the
// repo does not exist yet or has no usable URL — typically because its backing
// registry credentials are not configured. Reconcile surfaces this as a
// Ready=False condition and auto-requeues instead of hard-failing.
var errClusterRepoNotReady = stderrors.New("cluster repo not ready")

// errCatalogClientNotConfigured marks a git-backed ClusterRepo component that
// cannot be deployed because no Rancher catalog client is configured (the
// operator has no Rancher API token). The catalog config is editable at runtime
// via Settings, so reconcile surfaces this as a Ready=False condition and
// requeues: it clears as soon as the token is supplied (the AIWorkload
// controller also watches Settings, so recovery is usually immediate).
var errCatalogClientNotConfigured = stderrors.New("rancher catalog client not configured")

type clusterRepoInfo struct {
	Kind           repoKind // how the repo serves charts (http/oci/git)
	URL            string   // http/oci repos only
	GitRepo        string   // git repos only
	GitBranch      string   // git repos only
	Commit         string   // git repos only: status.commit, the indexed revision
	ClientSecret   string   // name of the basic-auth secret; empty if unauthenticated
	ClientSecretNS string   // namespace of the basic-auth secret (typically cattle-system)
}

// reconcileBlueprintStatus handles blueprint-sourced AIWorkloads.
func (r *AIWorkloadReconciler) reconcileBlueprintStatus(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) (ctrl.Result, error) {
	src := w.Spec.Source.Blueprint
	if src == nil {
		return ctrl.Result{}, nil
	}

	// Step 1: verify Blueprint CR exists.
	crName := bpCRName(src.Name, src.Version)
	var bp aiplatformv1alpha1.Blueprint
	if err := r.Get(ctx, types.NamespacedName{Name: crName}, &bp); err != nil {
		if errors.IsNotFound(err) {
			setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, "BlueprintNotFound",
				fmt.Sprintf("Blueprint %q was not found.", crName), w.Generation)
			w.Status.Phase = guardPhaseTransition(aiplatformv1alpha1.AIWorkloadPhaseFailed, w.Status.Phase, w.CreationTimestamp.Time)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Step 2: populate FleetBundleNames from components on first reconcile.
	if len(w.Spec.FleetBundleNames) == 0 {
		names := make([]string, 0, len(bp.Spec.Components))
		for _, c := range bp.Spec.Components {
			name := naming.TruncateDNS1123Label(w.Name+"-"+naming.Slugify(c.ChartName), 63)
			names = append(names, name)
		}
		w.Spec.FleetBundleNames = names
		if err := r.Update(ctx, w); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Step 3: ensure HelmOps or git files exist for each component bundle.
	// The bundle name is a pure function of (workload, component chart name) — computed here and
	// by desiredHelmOpKeys/cleanup/certification alike — so a version change that adds, removes,
	// renames, or reorders components never desynchronizes the render names from the desired set
	// (the stale FleetBundleNames[i] index is intentionally NOT used for rendering).
	expectedDigests := map[string]string{}
	for _, c := range bp.Spec.Components {
		var digest string
		var err error
		switch w.Spec.DeployStrategy {
		case aiplatformv1alpha1.AIWorkloadDeployFleetBundle:
			digest, err = r.ensureBlueprintHelmOp(ctx, w, c, blueprintBundleName(w.Name, c.ChartName))
		case aiplatformv1alpha1.AIWorkloadDeployGitOps:
			digest, err = r.ensureBlueprintGitFile(ctx, w, c, blueprintBundleName(w.Name, c.ChartName))
		}
		if err != nil {
			if stderrors.Is(err, errCatalogClientNotConfigured) {
				// A git-backed component needs a Rancher API token the operator
				// wasn't given. The catalog config is editable at runtime via
				// Settings, so surface a clear condition + Failed phase and requeue:
				// the AIWorkload controller watches Settings and re-enqueues on the
				// change, and this RequeueAfter is a race-safe net in case the
				// holder is rebuilt after our watch fires.
				msg := fmt.Sprintf("Component %q uses git-backed repo %q, which requires a Rancher API token. Set one under Settings → Rancher API Access in the AI Factory UI (Settings.spec.rancherCatalog.tokenSecretRef).", c.ChartName, c.ChartRepo)
				setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, "CatalogClientNotConfigured", msg, w.Generation)
				w.Status.Phase = guardPhaseTransition(aiplatformv1alpha1.AIWorkloadPhaseFailed, w.Status.Phase, w.CreationTimestamp.Time)
				return ctrl.Result{RequeueAfter: time.Minute}, nil
			}
			if stderrors.Is(err, rancher.ErrUnauthorized) {
				// The token exists but Rancher rejected it — typically because it
				// expired. Rancher clamps a token's TTL to auth-token-max-ttl-minutes
				// (90 days by default), so every configured token eventually lands
				// here. Give it its own reason rather than folding it into the
				// generic fetch error, so the UI can point at the fix. Requeue
				// rather than fail terminally: re-authorizing in Settings resolves
				// it, and the AIWorkload controller watches Settings.
				msg := fmt.Sprintf("Rancher rejected the API token while fetching component %q from git-backed repo %q. The token may have expired — re-authorize under Settings → Rancher API Access in the AI Factory UI.", c.ChartName, c.ChartRepo)
				setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, reasonRancherTokenRejected, msg, w.Generation)
				w.Status.Phase = guardPhaseTransition(aiplatformv1alpha1.AIWorkloadPhaseFailed, w.Status.Phase, w.CreationTimestamp.Time)
				return ctrl.Result{RequeueAfter: time.Minute}, nil
			}
			if stderrors.Is(err, errChartTooLarge) {
				// Terminal: the chart is too big for a Fleet Bundle and no amount
				// of retrying changes that. Returning the error instead would spin
				// the workqueue forever, re-downloading the archive from Rancher on
				// every backoff tick, while the workload kept advertising the stale
				// "Component bundles reconciled" message from its last good pass.
				msg := fmt.Sprintf("Component %q cannot be deployed from git-backed repo %q: %v", c.ChartName, c.ChartRepo, err)
				setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, "ChartTooLarge", msg, w.Generation)
				w.Status.Phase = guardPhaseTransition(aiplatformv1alpha1.AIWorkloadPhaseFailed, w.Status.Phase, w.CreationTimestamp.Time)
				return ctrl.Result{}, nil
			}
			if stderrors.Is(err, errClusterRepoNotReady) {
				// The repo the component needs is missing — usually because its
				// registry credentials are not configured. Surface a condition +
				// Failed phase (subject to the initial grace window) and requeue
				// so the workload recovers once the repo appears.
				msg := fmt.Sprintf("Repository %q is not available yet. If this persists, verify its credentials in Settings.", c.ChartRepo)
				setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, reasonClusterRepoNotReady, msg, w.Generation)
				w.Status.Phase = guardPhaseTransition(aiplatformv1alpha1.AIWorkloadPhaseFailed, w.Status.Phase, w.CreationTimestamp.Time)
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
			// Anything else: the cause is unknown and may be transient, so return
			// the error and let the workqueue back off. Set a condition first —
			// without one the object keeps advertising the previous pass's
			// Ready=True while it is stuck.
			msg := fmt.Sprintf("Component %q from repo %q could not be reconciled: %v", c.ChartName, c.ChartRepo, truncateForCondition(err.Error()))
			setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, "ComponentReconcileFailed", msg, w.Generation)
			w.Status.Phase = guardPhaseTransition(aiplatformv1alpha1.AIWorkloadPhaseFailed, w.Status.Phase, w.CreationTimestamp.Time)
			return ctrl.Result{}, err
		}
		for _, k := range desiredHelmOpKeys(w.Name, w.Spec.TargetClusters, []aiplatformv1alpha1.BlueprintComponent{c}, w.Spec.DeployStrategy) {
			expectedDigests[k.Namespace+"/"+k.Name] = digest
			// GitOps HelmOps are created asynchronously by Fleet's GitRepo controller,
			// so record a render baseline once the HelmOp appears (the FleetBundle path
			// records it inside ensureBlueprintHelmOp) so acceptedFalseTerminal can
			// detect a post-baseline Accepted=False rejection without waiting for the
			// operation deadline.
			if w.Spec.DeployStrategy == aiplatformv1alpha1.AIWorkloadDeployGitOps {
				ho, err := r.getHelmOpIn(ctx, k.Namespace, k.Name)
				if err != nil {
					return ctrl.Result{}, err
				}
				if ho != nil {
					w.Status.RenderBaselines = upsertRenderBaseline(w.Status.RenderBaselines, aiplatformv1alpha1.RenderBaseline{
						HelmOpUID:           string(ho.GetUID()),
						RenderDigest:        digest,
						RetryEpoch:          r.retryEpochValue(w),
						HelmOpGeneration:    ho.GetGeneration(),
						AcceptedFingerprint: acceptedConditionFingerprint(ho),
					})
				}
			}
		}
	}
	w.Status.ObservedGeneration = w.Generation

	// Step 4: cleanup stale HelmOps, then build component matrix, set phase, and certify.
	keys := desiredHelmOpKeys(w.Name, w.Spec.TargetClusters, bp.Spec.Components, w.Spec.DeployStrategy)
	if err := r.cleanupStaleHelmOps(ctx, w, keys); err != nil {
		return ctrl.Result{}, err
	}
	// Prune baselines to match current desired HelmOp UIDs.
	desiredUIDs := r.collectDesiredHelmOpUIDs(ctx, keys)
	w.Status.RenderBaselines = pruneRenderBaselines(w.Status.RenderBaselines, desiredUIDs)
	cells, err := r.buildComponentMatrix(ctx, w, keys, expectedDigests)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !equalComponentStatuses(w.Status.ComponentStatuses, cells) {
		w.Status.ComponentStatuses = cells
	}
	// Derive clusterStatuses from the component matrix (aggregate per clusterId to worst phase).
	clusterStatuses := aggregateClusterStatuses(cells)
	if !equalClusterStatuses(w.Status.ClusterStatuses, clusterStatuses) {
		w.Status.ClusterStatuses = clusterStatuses
	}
	w.Status.Phase = guardPhaseTransition(phaseFromCells(cells), w.Status.Phase, w.CreationTimestamp.Time)
	if err := r.certifyDeployedSource(ctx, w, keys, expectedDigests); err != nil {
		return ctrl.Result{}, err
	}
	// Reconcile completed without error: clear any prior Ready=False (e.g. a
	// transient ClusterRepoNotReady) so the condition recovers alongside the
	// phase. Phase reflects rollout state; Ready reflects reconcile success.
	setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionTrue, reasonReconciled, "Component bundles reconciled", w.Generation)
	return ctrl.Result{}, nil
}

// retryEpochValue reads the durable retry-epoch counter (default 0). Every desired HelmOp is
// rendered with spec.forceSyncGeneration = this value so completing a retry never resets it.
func (r *AIWorkloadReconciler) retryEpochValue(w *aiplatformv1alpha1.AIWorkload) int64 {
	if w.Annotations == nil {
		return 0
	}
	n, err := strconv.ParseInt(w.Annotations[retryEpochAnnotation], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// ensureBlueprintHelmOp creates (or patches) the HelmOp for one blueprint component.
func (r *AIWorkloadReconciler) ensureBlueprintHelmOp(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	c aiplatformv1alpha1.BlueprintComponent,
	bundleName string,
) (string, error) {
	repoInfo, err := r.resolveClusterRepo(ctx, c.ChartRepo)
	if err != nil {
		return "", fmt.Errorf("resolve repo %q: %w", c.ChartRepo, err)
	}

	// Git-backed ClusterRepos have no HTTP/OCI URL a HelmOp could pull from; the
	// operator fetches the chart from Rancher and deploys it as an embedded Fleet
	// Bundle instead.
	if repoInfo.Kind == repoKindGit {
		return r.ensureBlueprintGitChartBundle(ctx, w, c, bundleName, repoInfo, false)
	}

	isOCI := strings.HasPrefix(repoInfo.URL, "oci://")
	helmSpec := map[string]any{
		"version": c.ChartVersion,
		// releaseName uses the chart name (not the full bundleName) so chart
		// sub-resources templated as `{{ .Release.Name }}-foo` fit under the
		// 63-char DNS-label limit. bundleName already includes the workload
		// name and component slug for uniqueness in fleet-default, so on long
		// blueprints the bundleName-derived release name burned all the chart's
		// remaining headroom — e.g. nvidia-blueprint-rag's `-etcd-headless`
		// (14 chars) tipped a 52-char release past 63 and Kubernetes rejected
		// the Service. Helm release names are unique per (cluster, namespace),
		// and Blueprint components are addressed by chart name, so the chart
		// name alone is the right level of granularity here. A component may
		// override this default via its ReleaseName (componentReleaseName).
		"releaseName": capReleaseName(componentReleaseName(c)),
		// Disable Fleet's ${ } value templating: we resolve all values ourselves,
		// and upstream charts legitimately use ${ } (e.g. OTel ${env:MY_POD_IP}),
		// which Fleet would otherwise mis-parse as a template function.
		"disablePreProcess": true,
		// takeOwnership lets the chart's Helm install adopt resources we
		// pre-delivered (ngc-secret, ngc-api, suse-ai-pull-combined via the
		// pull-secret bundle). Many NVIDIA NIM-family charts template their
		// own ngc-secret resource by default — without takeOwnership, the
		// install aborts with "Secret ... cannot be imported into the
		// current release: invalid ownership metadata; key meta.helm.sh/
		// release-name must equal ...". The pull-secret bundle's Helm
		// wrapper stamps a different release-name on those secrets, so the
		// workload chart can't claim them. takeOwnership says "claim them
		// anyway", which is the right call here: the secrets logically
		// belong to whichever workload uses them.
		"takeOwnership": true,
	}
	if !isOCI {
		helmSpec["repo"] = repoInfo.URL
		helmSpec["chart"] = c.ChartName
	} else {
		helmSpec["repo"] = repoInfo.URL + "/" + c.ChartName
	}
	vals := map[string]any{}
	if c.Values != nil {
		_ = json.Unmarshal(c.Values.Raw, &vals)
	}
	// Per-component namespace (ai-factory's componentNamespace helper) lets a
	// blueprint component override the workload-level TargetNamespace. The
	// injector and the HelmOp's defaultNamespace below both consume this.
	ns := componentNamespace(w, c)
	created, err := r.injectorFor(c.Vendor).Apply(ctx, r.localCC(), ns, repoInfo, vals, targetsLocalCluster(w))
	if err != nil {
		return "", fmt.Errorf("inject secrets for %s: %w", c.ChartName, err)
	}
	w.Status.PullSecretDeliveries = mergePullSecretDelivery(w.Status.PullSecretDeliveries, ns, created)
	if len(vals) > 0 {
		helmSpec["values"] = vals
	}

	epoch := r.retryEpochValue(w)

	digest := perHelmOpRenderDigest(ComponentRenderInputs{
		ChartRepo:    c.ChartRepo,
		ChartName:    c.ChartName,
		ChartVersion: c.ChartVersion,
		Namespace:    ns,
		Vendor:       string(c.Vendor),
		RepoURL:      repoInfo.URL,
		Targets:      w.Spec.TargetClusters,
		Values:       vals,
	})

	localTargets, downstreamTargets := splitWorkloadTargets(w)

	for _, pair := range []struct {
		ns      string
		targets []any
	}{
		{"fleet-local", localTargets},
		{"fleet-default", downstreamTargets},
	} {
		if len(pair.targets) == 0 {
			continue
		}

		if repoInfo.ClientSecret != "" {
			if err := r.ensureFleetAuthSecret(ctx, pair.ns, repoInfo.ClientSecretNS, repoInfo.ClientSecret); err != nil {
				log.FromContext(ctx).Error(err, "could not sync auth secret to fleet namespace",
					"namespace", pair.ns, "secret", repoInfo.ClientSecret)
			}
		}

		ho := &unstructured.Unstructured{}
		ho.SetGroupVersionKind(helmOpGVK)
		ho.SetName(bundleName)
		ho.SetNamespace(pair.ns)
		ho.SetLabels(map[string]string{workloadUIDLabel: string(w.UID)})
		_ = unstructured.SetNestedStringMap(ho.Object, map[string]string{
			renderDigestLabel: renderDigestLabelValue(digest),
			workloadUIDLabel:  string(w.UID),
		}, "spec", "labels")
		// defaultNamespace (not namespace): targets the release namespace without
		// forcing every resource into it. Fleet's strict `namespace` field rejects
		// any cluster-scoped resource (ClusterRole, CRD, webhook), which breaks
		// operator/CRD-bearing charts.
		_ = unstructured.SetNestedField(ho.Object, ns, "spec", "defaultNamespace")
		_ = unstructured.SetNestedField(ho.Object, helmSpec, "spec", "helm")
		// forceSyncGeneration lives at the HelmOp spec top level (HelmOpSpec embeds
		// BundleSpec→BundleDeploymentOptions inline), NOT under spec.helm — Fleet's HelmOp
		// schema declares spec.forceSyncGeneration and rejects spec.helm.forceSyncGeneration.
		_ = unstructured.SetNestedField(ho.Object, epoch, "spec", "forceSyncGeneration")
		_ = unstructured.SetNestedSlice(ho.Object, pair.targets, "spec", "targets")
		if repoInfo.ClientSecret != "" {
			_ = unstructured.SetNestedField(ho.Object, repoInfo.ClientSecret, "spec", "helmSecretName")
		}

		if err := r.Patch(ctx, ho, client.Apply,
			client.ForceOwnership,
			client.FieldOwner("aif-operator"),
		); err != nil {
			return "", fmt.Errorf("patch HelmOp %s/%s: %w", pair.ns, bundleName, err)
		}

		// Record baseline after successful Patch: the HelmOp now carries UID, generation,
		// and the current render digest label. Upsert a baseline entry for this HelmOp.
		w.Status.RenderBaselines = upsertRenderBaseline(w.Status.RenderBaselines, aiplatformv1alpha1.RenderBaseline{
			HelmOpUID:           string(ho.GetUID()),
			RenderDigest:        digest,
			RetryEpoch:          epoch,
			HelmOpGeneration:    ho.GetGeneration(),
			AcceptedFingerprint: acceptedConditionFingerprint(ho),
		})
	}
	return digest, nil
}

const (
	defaultAppCollectionHost = "dp.apps.rancher.io"
	defaultSUSERegistryHost  = "registry.suse.com"
	defaultNvidiaHost        = "nvcr.io"
	combinedPullSecretName   = "suse-ai-pull-combined"

	nvidiaImagePullSecretName = "ngc-secret"
	nvidiaAPISecretName       = "ngc-api"
)

// nvidiaAPISecretKeys are the env-var names different NVIDIA chart families
// expect for the same NGC API token. We populate all of them so charts that
// read any one of them work without per-chart tuning:
//   - NGC_API_KEY: original SUSE-AI / NIM convention
//   - NGC_CLI_API_KEY: ngc-cli auth (used by some NIM containers)
//   - NVIDIA_API_KEY: nvidia-blueprints (e.g. nvidia-blueprint-rag)
var nvidiaAPISecretKeys = []string{"NGC_API_KEY", "NGC_CLI_API_KEY", "NVIDIA_API_KEY"}

// ngcAPISecretData builds the ngc-api Opaque secret payload with all
// nvidiaAPISecretKeys mapped to the same token value.
func ngcAPISecretData(token string) map[string][]byte {
	out := make(map[string][]byte, len(nvidiaAPISecretKeys))
	for _, k := range nvidiaAPISecretKeys {
		out[k] = []byte(token)
	}
	return out
}

// secretInjector configures Helm values for a blueprint component so its
// rendered workloads can pull images and access vendor APIs. Each implementation
// owns the namespace-scoped Secret objects it requires and the Helm-values paths
// it writes. A no-op Apply (e.g., missing credentials) is acceptable; Helm will
// surface the resulting ImagePullBackOff downstream.
type secretInjector interface {
	// Apply sets value-path references in vals and returns the names of the
	// Secrets the operator will deliver (used by reconcilePullSecrets/
	// deliverPullSecrets to attach them to ServiceAccounts and ship them to
	// every target cluster).
	//
	// writeLocal gates the local-cluster side effects only: when true, the
	// Secret (and its namespace) are persisted on the operator's own cluster
	// through cc. When false — a downstream-only workload — nothing is written
	// locally; deliverPullSecrets ships the Secret + namespace to the target
	// cluster via a Fleet Bundle instead. The returned names and the vals
	// mutation happen regardless of writeLocal (bug 862).
	Apply(ctx context.Context, cc cluster.Client, targetNamespace string, repoInfo clusterRepoInfo, vals map[string]any, writeLocal bool) (createdSecretNames []string, err error)
}

// suseInjector preserves the historical combined-secret behavior: one
// dockerconfigjson covering every configured registry, written into both
// imagePullSecrets and global.imagePullSecrets.
type suseInjector struct{ r *AIWorkloadReconciler }

func (s *suseInjector) Apply(ctx context.Context, cc cluster.Client, targetNamespace string, repoInfo clusterRepoInfo, vals map[string]any, writeLocal bool) ([]string, error) {
	name, err := s.r.ensureCombinedPullSecret(ctx, cc, targetNamespace, repoInfo, writeLocal)
	if err != nil {
		log.FromContext(ctx).Error(err, "could not create image pull secret", "namespace", targetNamespace)
		return nil, nil
	}
	if name == "" {
		return nil, nil
	}
	pullSecrets := []any{map[string]any{"name": name}}
	vals["imagePullSecrets"] = pullSecrets
	// Merge into the existing global map rather than replacing it — the blueprint
	// may set sibling keys under global (e.g. open-webui's global.tls for the
	// suse-private-ai cert config). Replacing global wholesale dropped those,
	// which made open-webui render an Ingress with an empty TLS host.
	global, _ := vals["global"].(map[string]any)
	if global == nil {
		global = map[string]any{}
	}
	global["imagePullSecrets"] = pullSecrets
	vals["global"] = global
	return []string{name}, nil
}

// nvidiaInjector creates the conventional ngc-secret + ngc-api in the target
// namespace and writes both common pull-secret value paths. NVIDIA charts honor
// either the standard k8s pod-spec list-of-objects shape (imagePullSecrets) or
// the k8s-nim-operator flat-string shape (image.pullSecrets); writing both
// covers the surveyed NIM chart families.
type nvidiaInjector struct{ r *AIWorkloadReconciler }

func (n *nvidiaInjector) Apply(ctx context.Context, cc cluster.Client, targetNamespace string, repoInfo clusterRepoInfo, vals map[string]any, writeLocal bool) ([]string, error) {
	l := log.FromContext(ctx)

	dockerCfg, err := n.r.buildNGCDockerConfig(ctx)
	if err != nil {
		return nil, err
	}
	if dockerCfg == nil {
		l.Info("nvidia injector: credentials not configured, skipping", "namespace", targetNamespace)
		return nil, nil
	}

	var s aiplatformv1alpha1.Settings
	if err := n.r.Get(ctx, types.NamespacedName{Namespace: n.r.OperatorNamespace, Name: operatorSettingsName}, &s); err != nil {
		return nil, nil
	}
	_, token, ok, err := n.r.readRegistryCredentials(ctx, credentials.RegistryNvidia, s.Spec.Nvidia.UserSecretRef, s.Spec.Nvidia.TokenSecretRef)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	// Persist the ngc-secret / ngc-api Secrets onto the operator's own cluster
	// only when the workload targets it. For a downstream-only workload
	// deliverPullSecrets rebuilds and ships them (and their namespace) to the
	// target cluster via a Fleet Bundle; writing them locally would leave an
	// orphan namespace on the management cluster (bug 862). The returned names
	// and the vals mutation below happen regardless so the chart references the
	// secrets and the delivery is recorded.
	if writeLocal {
		if err := n.r.ensureNamespace(ctx, targetNamespace); err != nil {
			return nil, err
		}
		pullSecret := &corev1.Secret{}
		pullSecret.Name = nvidiaImagePullSecretName
		pullSecret.Namespace = targetNamespace
		pullSecret.Type = corev1.SecretTypeDockerConfigJson
		pullSecret.Data = map[string][]byte{corev1.DockerConfigJsonKey: dockerCfg}
		if err := cc.ApplySecret(ctx, pullSecret); err != nil {
			return nil, fmt.Errorf("apply %s/%s: %w", targetNamespace, nvidiaImagePullSecretName, err)
		}

		apiSecret := &corev1.Secret{}
		apiSecret.Name = nvidiaAPISecretName
		apiSecret.Namespace = targetNamespace
		apiSecret.Type = corev1.SecretTypeOpaque
		apiSecret.Data = ngcAPISecretData(token)
		if err := cc.ApplySecret(ctx, apiSecret); err != nil {
			return nil, fmt.Errorf("apply %s/%s: %w", targetNamespace, nvidiaAPISecretName, err)
		}
	}

	injectNvidiaPullSecretRefs(vals)
	// NVIDIA blueprint charts (aiq-aira, nvidia-blueprint-rag, ...) commonly
	// template their own ngc-secret / ngc-api from `imagePullSecret.password` /
	// `ngcApiSecret.password`. Those values default to "" — and with the
	// workload HelmOp's takeOwnership:true the chart adopts the operator's
	// pre-delivered secret and then OVERWRITES its data with the empty
	// template, breaking image pulls. Disable the chart's secret templating
	// so our pre-delivered Secret survives.
	disableChartSecretCreation(vals, "imagePullSecret", nvidiaImagePullSecretName)
	disableChartSecretCreation(vals, "ngcApiSecret", nvidiaAPISecretName)
	return []string{nvidiaImagePullSecretName, nvidiaAPISecretName}, nil
}

// buildNGCDockerConfig reads NVIDIA Settings + credentials from the operator
// namespace and returns the marshaled dockerconfigjson bytes. Returns
// (nil, nil) when credentials are not configured or unreadable — callers
// should treat this as "no NGC secret to deliver this round" and skip.
// Returns (nil, err) only on a hard error like JSON marshaling failure.
func (r *AIWorkloadReconciler) buildNGCDockerConfig(ctx context.Context) ([]byte, error) {
	var s aiplatformv1alpha1.Settings
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: operatorSettingsName}, &s); err != nil {
		return nil, nil
	}
	user, token, ok, err := r.readRegistryCredentials(ctx, credentials.RegistryNvidia, s.Spec.Nvidia.UserSecretRef, s.Spec.Nvidia.TokenSecretRef)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	host := defaultNvidiaHost
	if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.Nvidia != "" {
		host = s.Spec.RegistryEndpoints.Nvidia
	}
	cfg, err := json.Marshal(map[string]any{
		"auths": map[string]any{host: dockerAuthEntry(user, token)},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal ngc dockerconfigjson: %w", err)
	}
	return cfg, nil
}

// localCC returns a cluster.Client bound to the operator's own cluster.
// Use this at call sites that write Secrets that should live on the
// operator's cluster (i.e., the local-only path). Task 2.x will introduce
// per-target-cluster client selection for the cross-cluster delivery path.
func (r *AIWorkloadReconciler) localCC() cluster.Client {
	return cluster.NewLocalClient(r.Client, r.Scheme)
}

// injectorFor returns the secretInjector for a component vendor. Unknown or
// empty vendors fall back to the SUSE profile defensively; the CRD default
// fills the field in practice.
func (r *AIWorkloadReconciler) injectorFor(vendor aiplatformv1alpha1.ComponentVendor) secretInjector {
	switch vendor {
	case aiplatformv1alpha1.ComponentVendorNvidia:
		return &nvidiaInjector{r: r}
	default:
		return &suseInjector{r: r}
	}
}

// ensureCombinedPullSecret creates (or updates) a single kubernetes.io/dockerconfigjson secret
// in targetNamespace whose "auths" map covers ALL configured registries: the component's own
// chartRepo, ApplicationCollection, and SUSERegistry from Settings. This ensures subchart
// images pulled from a different registry than the parent chart are also authenticated.
// Returns the secret name, or "" if no credentials are available.
func (r *AIWorkloadReconciler) ensureCombinedPullSecret(ctx context.Context, cc cluster.Client, targetNamespace string, repoInfo clusterRepoInfo, writeLocal bool) (string, error) {
	auths := map[string]any{}

	// Component's own chartRepo credentials. The Settings-derived registries
	// are appended below; for the local path this gives the most complete
	// coverage (chart-pull host + every image host the chart may reference).
	// Git repos are skipped: their clientSecret is git-clone auth, not an
	// image-registry credential, and their URL is empty — Host("") would key it
	// under a bogus "" host in the dockerconfigjson.
	if repoInfo.ClientSecret != "" && repoInfo.Kind != repoKindGit {
		if host := registryurl.Host(repoInfo.URL); host != "" {
			src := &corev1.Secret{}
			if err := r.Get(ctx, types.NamespacedName{Namespace: repoInfo.ClientSecretNS, Name: repoInfo.ClientSecret}, src); err == nil {
				if u, p := string(src.Data["username"]), string(src.Data["password"]); u != "" && p != "" {
					auths[host] = dockerAuthEntry(u, p)
				}
			}
		}
	}

	r.addSUSESettingsAuths(ctx, auths)

	if len(auths) == 0 {
		return "", nil
	}

	// Persist the secret onto the operator's own cluster only when the workload
	// targets it. For a downstream-only workload deliverPullSecrets rebuilds and
	// ships the secret (and its namespace) to the target cluster via a Fleet
	// Bundle; writing it locally would leave an orphan namespace on the
	// management cluster (bug 862). The name is still returned so the caller can
	// wire imagePullSecrets into the chart values and record the delivery.
	if !writeLocal {
		return combinedPullSecretName, nil
	}

	dockerCfg, err := json.Marshal(map[string]any{"auths": auths})
	if err != nil {
		return "", err
	}

	// The target namespace may not exist yet — a component pinned to a fixed
	// namespace is often new, and Fleet only creates it later when the HelmOp
	// reconciles. The secret Patch below would fail (NotFound) against a missing
	// namespace, leaving the chart without imagePullSecrets, so ensure it first.
	if err := r.ensureNamespace(ctx, targetNamespace); err != nil {
		return "", err
	}

	dst := &corev1.Secret{}
	dst.Name = combinedPullSecretName
	dst.Namespace = targetNamespace
	dst.Type = corev1.SecretTypeDockerConfigJson
	dst.Data = map[string][]byte{corev1.DockerConfigJsonKey: dockerCfg}
	if err := cc.ApplySecret(ctx, dst); err != nil {
		return "", err
	}
	return combinedPullSecretName, nil
}

// addSUSESettingsAuths populates an "auths" map with entries for every
// Settings-derived registry that has credentials configured: SUSE App
// Collection, SUSE Registry, and NVIDIA NGC. Hosts honor
// Settings.spec.registryEndpoints overrides for air-gap mirroring. Missing
// or partially-configured credential refs are silently skipped (matches the
// existing per-injector lenient policy).
//
// Used by:
//   - ensureCombinedPullSecret: appended after the component's own chart-repo
//     credentials for the local-cluster write path.
//   - buildSUSECombinedDockerConfig: sole source for the downstream-delivery
//     path, where there is no component-specific chartRepo context.
func (r *AIWorkloadReconciler) addSUSESettingsAuths(ctx context.Context, auths map[string]any) {
	var s aiplatformv1alpha1.Settings
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: operatorSettingsName}, &s); err != nil {
		return
	}

	appHost := defaultAppCollectionHost
	if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.ApplicationCollection != "" {
		appHost = registryurl.Host(s.Spec.RegistryEndpoints.ApplicationCollection)
	}
	if u, p, ok, err := r.readRegistryCredentials(ctx, credentials.RegistryApplicationCollection, s.Spec.ApplicationCollection.UserSecretRef, s.Spec.ApplicationCollection.TokenSecretRef); err == nil && ok {
		auths[appHost] = dockerAuthEntry(u, p)
	}

	suseHost := defaultSUSERegistryHost
	if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.SUSERegistry != "" {
		suseHost = registryurl.Host(s.Spec.RegistryEndpoints.SUSERegistry)
	}
	if u, p, ok, err := r.readRegistryCredentials(ctx, credentials.RegistrySUSERegistry, s.Spec.SUSERegistry.UserSecretRef, s.Spec.SUSERegistry.TokenSecretRef); err == nil && ok {
		auths[suseHost] = dockerAuthEntry(u, p)
	}

	// NVIDIA images come from nvcr.io (connected); registryEndpoints.nvidia is the chart-repo
	// OCI URL, not an image host, and air-gap redirection is a node-level concern.
	if u, p, ok, err := r.readRegistryCredentials(ctx, credentials.RegistryNvidia, s.Spec.Nvidia.UserSecretRef, s.Spec.Nvidia.TokenSecretRef); err == nil && ok {
		auths[defaultNvidiaHost] = dockerAuthEntry(u, p)
	}
}

// readRegistryCredentials resolves explicit Settings refs or well-known operator
// secrets and returns decoded username/token values.
func (r *AIWorkloadReconciler) readRegistryCredentials(
	ctx context.Context,
	registry credentials.Registry,
	explicitUser, explicitToken *aiplatformv1alpha1.SecretKeyRef,
) (user, token string, ok bool, err error) {
	userRef, tokenRef := credentials.EffectiveRefs(ctx, r.Client, r.OperatorNamespace, explicitUser, explicitToken, registry)
	return credentials.ReadPair(ctx, r.Client, r.OperatorNamespace, userRef, tokenRef)
}

// buildSUSECombinedDockerConfig returns the marshaled dockerconfigjson bytes
// for the suseInjector's combined pull secret, sourced entirely from Settings
// (no component-specific chartRepo context). Returns (nil, nil) when no
// Settings credentials are configured — callers should treat this as
// "nothing to deliver this round" and skip silently. Returns (nil, err)
// only on a hard error like JSON marshaling failure.
//
// This is the downstream-delivery sibling of ensureCombinedPullSecret: the
// suseInjector writes the secret locally during reconcile (with chart-repo
// auth merged in), then deliverPullSecrets needs to ship an equivalent
// payload to each target downstream cluster — minus the per-component
// chart-repo entry, since the pull-secret authenticates IMAGE pulls (not
// chart pulls) and the chart-repo host is not an image registry.
func (r *AIWorkloadReconciler) buildSUSECombinedDockerConfig(ctx context.Context) ([]byte, error) {
	auths := map[string]any{}
	r.addSUSESettingsAuths(ctx, auths)
	if len(auths) == 0 {
		return nil, nil
	}
	cfg, err := json.Marshal(map[string]any{"auths": auths})
	if err != nil {
		return nil, fmt.Errorf("marshal suse combined dockerconfigjson: %w", err)
	}
	return cfg, nil
}

// ensureNamespace makes sure the namespace exists. It uses Server-Side Apply
// (a write that bypasses the client cache) rather than a cached Get: the
// operator is not granted list/watch on namespaces, so a cached read would
// force controller-runtime to start a Namespace informer that fails to sync.
// This mirrors how the API layer ensures the workload namespace.
func (r *AIWorkloadReconciler) ensureNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{}
	ns.APIVersion = "v1"
	ns.Kind = "Namespace"
	ns.Name = name
	return r.Patch(ctx, ns, client.Apply, client.ForceOwnership, client.FieldOwner("aif-operator"))
}

// readSettingsSecretKey reads a single key from a Settings secret ref in the operator namespace.
func (r *AIWorkloadReconciler) readSettingsSecretKey(ctx context.Context, ref *aiplatformv1alpha1.SecretKeyRef) (string, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: ref.Name}, &secret); err != nil {
		return "", err
	}
	val, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %q", ref.Key, ref.Name)
	}
	return string(val), nil
}

const (
	// 53 = 63 (K8s DNS-1123 label max) − 10 bytes Helm reserves for generated
	// suffixes. Fleet validates spec.helm.releaseName against this.
	helmReleaseNameMax = 53 // Helm/Fleet reject release names longer than this.
	helmHashLen        = 6  // base36 suffix; 36^6 ≈ 2.2e9 distinct values, ample for collision avoidance.
)

// capReleaseName mirrors the dashboard's release-name capping: Helm/Fleet reject
// release names longer than 53 bytes, while the bundle (object) name may be up to
// 63. Append a short hash when truncating to avoid collisions on a shared prefix.
// The result is always a valid DNS-1123 label (no leading/trailing '-'), even
// for pathological inputs.
//
// This need NOT match the dashboard's TS capReleaseName byte-for-byte: a single
// install's releaseName is produced by exactly one side, and the operator looks
// workloads up by bundle (object) name, never by releaseName.
func capReleaseName(name string) string {
	if len(name) <= helmReleaseNameMax {
		return name
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	suffix := strconv.FormatUint(uint64(h.Sum32()), 36)
	// base36(uint32) is 1–7 chars; cap to helmHashLen. The length guard is
	// required: slicing a shorter suffix (e.g. "5") to [:helmHashLen] would panic.
	if len(suffix) > helmHashLen {
		suffix = suffix[:helmHashLen]
	}
	head := strings.Trim(name[:helmReleaseNameMax-len(suffix)-1], "-")
	if head == "" {
		return suffix
	}
	return head + "-" + suffix
}

// dockerAuthEntry builds the auth object for a single registry in a dockerconfigjson auths map.
func dockerAuthEntry(username, password string) map[string]any {
	return map[string]any{
		"auth":     base64.StdEncoding.EncodeToString([]byte(username + ":" + password)),
		"username": username,
		"password": password,
	}
}

// ensureFleetAuthSecret copies a basic-auth secret from srcNS into the given
// fleet workspace namespace so HelmOp can authenticate to the OCI chart registry.
func (r *AIWorkloadReconciler) ensureFleetAuthSecret(ctx context.Context, fleetNS, srcNS, secretName string) error {
	src := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: srcNS, Name: secretName}, src); err != nil {
		if errors.IsNotFound(err) {
			return nil // credentials not configured yet — HelmOp will fail with auth error until they are
		}
		return err
	}

	dst := &corev1.Secret{}
	dst.APIVersion = "v1"
	dst.Kind = "Secret"
	dst.Name = secretName
	dst.Namespace = fleetNS
	dst.Type = src.Type
	dst.Data = src.Data
	return r.Patch(ctx, dst, client.Apply, client.ForceOwnership, client.FieldOwner("aif-operator"))
}

// ensureBlueprintGitFile publishes a git file for one blueprint component.
func (r *AIWorkloadReconciler) ensureBlueprintGitFile(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	c aiplatformv1alpha1.BlueprintComponent,
	bundleName string,
) (string, error) {
	repoInfo, err := r.resolveClusterRepo(ctx, c.ChartRepo)
	if err != nil {
		return "", fmt.Errorf("resolve repo %q: %w", c.ChartRepo, err)
	}

	// Git-backed ClusterRepos: publish an embedded-chart Fleet Bundle git file
	// rather than a HelmOp (which cannot pull from git).
	if repoInfo.Kind == repoKindGit {
		return r.ensureBlueprintGitChartBundle(ctx, w, c, bundleName, repoInfo, true)
	}

	isOCI := strings.HasPrefix(repoInfo.URL, "oci://")
	helmSpec := map[string]any{
		"version": c.ChartVersion,
		// releaseName uses the chart name (not the full bundleName) so chart
		// sub-resources templated as `{{ .Release.Name }}-foo` fit under the
		// 63-char DNS-label limit. bundleName already includes the workload
		// name and component slug for uniqueness in fleet-default, so on long
		// blueprints the bundleName-derived release name burned all the chart's
		// remaining headroom — e.g. nvidia-blueprint-rag's `-etcd-headless`
		// (14 chars) tipped a 52-char release past 63 and Kubernetes rejected
		// the Service. Helm release names are unique per (cluster, namespace),
		// and Blueprint components are addressed by chart name, so the chart
		// name alone is the right level of granularity here. A component may
		// override this default via its ReleaseName (componentReleaseName).
		"releaseName": capReleaseName(componentReleaseName(c)),
		// Disable Fleet's ${ } value templating: we resolve all values ourselves,
		// and upstream charts legitimately use ${ } (e.g. OTel ${env:MY_POD_IP}),
		// which Fleet would otherwise mis-parse as a template function.
		"disablePreProcess": true,
		// See ensureBlueprintHelmOp for the takeOwnership rationale — same
		// "adopt operator-delivered pull secrets" need on the GitOps path.
		"takeOwnership": true,
	}
	if !isOCI {
		helmSpec["repo"] = repoInfo.URL
		helmSpec["chart"] = c.ChartName
	} else {
		helmSpec["repo"] = repoInfo.URL + "/" + c.ChartName
	}

	// Load the blueprint component's own values BEFORE injecting pull secrets —
	// mirrors ensureBlueprintHelmOp. Omitting this dropped every component value
	// (including open-webui's global.tls) from the GitOps git file, so the chart
	// rendered with defaults and the open-webui Ingress got an empty TLS host.
	vals := map[string]any{}
	if c.Values != nil {
		_ = json.Unmarshal(c.Values.Raw, &vals)
	}
	ns := componentNamespace(w, c)
	created, err := r.injectorFor(c.Vendor).Apply(ctx, r.localCC(), ns, repoInfo, vals, targetsLocalCluster(w))
	if err != nil {
		return "", fmt.Errorf("inject secrets for %s: %w", c.ChartName, err)
	}
	w.Status.PullSecretDeliveries = mergePullSecretDelivery(w.Status.PullSecretDeliveries, ns, created)
	if len(vals) > 0 {
		helmSpec["values"] = vals
	}

	epoch := r.retryEpochValue(w)

	digest := perHelmOpRenderDigest(ComponentRenderInputs{
		ChartRepo:    c.ChartRepo,
		ChartName:    c.ChartName,
		ChartVersion: c.ChartVersion,
		Namespace:    ns,
		Vendor:       string(c.Vendor),
		RepoURL:      repoInfo.URL,
		Targets:      w.Spec.TargetClusters,
		Values:       vals,
	})

	localTargets, downstreamTargets := splitWorkloadTargets(w)
	targets := append(append([]any{}, localTargets...), downstreamTargets...)
	fleetNS := gitOpsFleetNamespace(w)

	helmOpSpec := map[string]any{
		// defaultNamespace (not namespace): targets the release namespace without
		// forcing every resource into it. Fleet's strict `namespace` field rejects
		// any cluster-scoped resource (ClusterRole, CRD, webhook), which breaks
		// operator/CRD-bearing charts.
		"defaultNamespace": ns,
		"helm":             helmSpec,
		"targets":          targets,
		// forceSyncGeneration lives at the HelmOp spec top level (not under spec.helm) —
		// Fleet's HelmOp schema declares spec.forceSyncGeneration.
		"forceSyncGeneration": epoch,
		"labels": map[string]any{
			renderDigestLabel: renderDigestLabelValue(digest),
			workloadUIDLabel:  string(w.UID),
		},
	}
	if repoInfo.ClientSecret != "" {
		helmOpSpec["helmSecretName"] = repoInfo.ClientSecret
	}

	helmOpObj := map[string]any{
		"apiVersion": "fleet.cattle.io/v1alpha1",
		"kind":       "HelmOp",
		"metadata": map[string]any{
			"name":      bundleName,
			"namespace": fleetNS,
			"labels": map[string]any{
				workloadUIDLabel: string(w.UID),
			},
		},
		"spec": helmOpSpec,
	}

	yamlBytes, err := json.MarshalIndent(helmOpObj, "", "  ")
	if err != nil {
		return "", err
	}
	content := string(yamlBytes)

	newHash := gitManifestHash(content)
	if w.Annotations[gitFileHashAnnotation(bundleName)] == newHash {
		return digest, nil // unchanged — no republish
	}
	if err := r.publishBlueprintGitFile(ctx, w, bundleName, content); err != nil {
		return "", err
	}
	metav1.SetMetaDataAnnotation(&w.ObjectMeta, gitFileHashAnnotation(bundleName), newHash)
	if err := r.Update(ctx, w); err != nil {
		return "", err
	}
	return digest, nil
}

func (r *AIWorkloadReconciler) publishBlueprintGitFile(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, bundleName, content string) error {
	var s aiplatformv1alpha1.Settings
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: r.OperatorNamespace,
		Name:      operatorSettingsName,
	}, &s); err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	gc, err := igit.NewFromSettings(ctx, &s, r.OperatorNamespace, &controllerSecretReader{r.Client})
	if err != nil {
		return fmt.Errorf("init git client: %w", err)
	}
	filePath := "workloads/" + bundleName + ".yaml"
	_, err = gc.WriteFile(ctx, filePath, content, "chore: deploy blueprint component "+bundleName)
	return err
}

// aggregateClusterStatuses derives ClusterStatuses from the component matrix by aggregating
// cells per clusterId to the WORST phase (reuses worstClusterPhase).
func aggregateClusterStatuses(cells []aiplatformv1alpha1.AIWorkloadComponentStatus) []aiplatformv1alpha1.AIWorkloadClusterStatus {
	clusterPhases := make(map[string]aiplatformv1alpha1.AIWorkloadClusterPhase)
	clusterMessages := make(map[string]string)

	for _, c := range cells {
		existing, seen := clusterPhases[c.ClusterID]
		if !seen {
			clusterPhases[c.ClusterID] = c.Phase
			if c.Phase == aiplatformv1alpha1.AIWorkloadClusterPhaseFailed {
				clusterMessages[c.ClusterID] = c.Message
			}
		} else {
			worst := worstClusterPhase(existing, c.Phase)
			clusterPhases[c.ClusterID] = worst
			if worst == aiplatformv1alpha1.AIWorkloadClusterPhaseFailed && c.Message != "" {
				clusterMessages[c.ClusterID] = c.Message
			}
		}
	}

	statuses := make([]aiplatformv1alpha1.AIWorkloadClusterStatus, 0, len(clusterPhases))
	for id, phase := range clusterPhases {
		statuses = append(statuses, aiplatformv1alpha1.AIWorkloadClusterStatus{
			ClusterID: id,
			Phase:     phase,
			Message:   clusterMessages[id],
		})
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ClusterID < statuses[j].ClusterID })
	return statuses
}

// equalClusterStatuses compares two slices of cluster statuses for equality.
func equalClusterStatuses(a, b []aiplatformv1alpha1.AIWorkloadClusterStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// worstClusterPhase returns the worse of two cluster phases: Failed > Pending > Running.
func worstClusterPhase(a, b aiplatformv1alpha1.AIWorkloadClusterPhase) aiplatformv1alpha1.AIWorkloadClusterPhase {
	rank := func(p aiplatformv1alpha1.AIWorkloadClusterPhase) int {
		switch p {
		case aiplatformv1alpha1.AIWorkloadClusterPhaseFailed:
			return 2
		case aiplatformv1alpha1.AIWorkloadClusterPhasePending:
			return 1
		default:
			return 0
		}
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

// resolveClusterRepo looks up a Rancher ClusterRepo by name and returns its URL and
// optional clientSecret name (stored in cattle-system by Rancher's catalog system).
func (r *AIWorkloadReconciler) resolveClusterRepo(ctx context.Context, repoName string) (clusterRepoInfo, error) {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(clusterRepoGVK)
	if err := r.Get(ctx, types.NamespacedName{Name: repoName}, cr); err != nil {
		if errors.IsNotFound(err) {
			return clusterRepoInfo{}, fmt.Errorf("%w: get ClusterRepo %q: %v", errClusterRepoNotReady, repoName, err)
		}
		return clusterRepoInfo{}, fmt.Errorf("get ClusterRepo %q: %w", repoName, err)
	}
	// spec.clientSecret is an object {name, namespace}, not a plain string.
	clientSecretName, _, _ := unstructured.NestedString(cr.Object, "spec", "clientSecret", "name")
	clientSecretNS, _, _ := unstructured.NestedString(cr.Object, "spec", "clientSecret", "namespace")
	if clientSecretNS == "" {
		clientSecretNS = "cattle-system"
	}
	info := clusterRepoInfo{ClientSecret: clientSecretName, ClientSecretNS: clientSecretNS}

	url, _, _ := unstructured.NestedString(cr.Object, "spec", "url")
	if url == "" {
		url, _, _ = unstructured.NestedString(cr.Object, "spec", "ociRepo")
	}
	if url != "" {
		info.URL = url
		info.Kind = repoKindHTTP
		if strings.HasPrefix(url, "oci://") {
			info.Kind = repoKindOCI
		}
		return info, nil
	}

	// Git-backed ClusterRepos (spec.gitRepo + spec.gitBranch) have no url/ociRepo.
	// Rancher clones and indexes them; the operator resolves the chart via the
	// Rancher catalog API and republishes it as a self-contained Fleet Bundle.
	gitRepo, _, _ := unstructured.NestedString(cr.Object, "spec", "gitRepo")
	if gitRepo != "" {
		info.Kind = repoKindGit
		info.GitRepo = gitRepo
		info.GitBranch, _, _ = unstructured.NestedString(cr.Object, "spec", "gitBranch")
		// Rancher records the revision it cloned and indexed. A git-backed repo
		// tracks a branch, so the same chart version can change underneath us;
		// this is the only input that tells us it did.
		info.Commit, _, _ = unstructured.NestedString(cr.Object, "status", "commit")
		return info, nil
	}

	return clusterRepoInfo{}, fmt.Errorf("%w: ClusterRepo %q has no url, ociRepo, or gitRepo in spec", errClusterRepoNotReady, repoName)
}

func bpCRName(familyName, version string) string {
	v := version
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	return naming.Slugify(familyName) + "-" + strings.ReplaceAll(v, ".", "-")
}

// injectNvidiaPullSecretRefs writes the ngc-secret reference into both common
// pull-secret value paths used by NVIDIA charts. Merge rules:
//   - path absent → create with [ngc-secret]
//   - path present and ngc-secret already listed → leave unchanged
//   - path present with other entries → prepend ngc-secret
//   - path present with an unexpected shape → leave untouched (author intent)
func injectNvidiaPullSecretRefs(vals map[string]any) {
	// Top-level k8s pod-spec shape: list of objects with {"name": ...}.
	// Covers Helm charts that respect the standard pod-spec convention at
	// the chart root.
	switch existing := vals["imagePullSecrets"].(type) {
	case nil:
		vals["imagePullSecrets"] = []any{map[string]any{"name": nvidiaImagePullSecretName}}
	case []any:
		if !containsObjectNamed(existing, nvidiaImagePullSecretName) {
			vals["imagePullSecrets"] = append([]any{map[string]any{"name": nvidiaImagePullSecretName}}, existing...)
		}
	}

	// NIM workload chart shape: image.pullSecrets is a flat string list
	// nested under the chart's "image" map. Conservative: only create the
	// parent map if values["image"] is absent or already a map; if it's
	// something unexpected (string, list, etc.), leave it alone to honor
	// the chart author's intent.
	injectFlatPullSecretList(vals, "image", "pullSecrets")

	// k8s-nim-operator chart shape: operator.image.pullSecrets is a flat
	// string list nested two levels deep (operator -> image -> pullSecrets).
	// Same conservative shape policy as image.pullSecrets above.
	injectNestedFlatPullSecretList(vals, "operator", "image", "pullSecrets")
}

// injectFlatPullSecretList adds nvidiaImagePullSecretName to a flat string
// list at vals[topKey][listKey], creating the parent map if absent. If the
// parent at vals[topKey] exists but isn't a map, the function returns without
// changes (preserves author intent for unexpected shapes).
func injectFlatPullSecretList(vals map[string]any, topKey, listKey string) {
	topRaw, present := vals[topKey]
	if !present {
		vals[topKey] = map[string]any{listKey: []any{nvidiaImagePullSecretName}}
		return
	}
	top, ok := topRaw.(map[string]any)
	if !ok {
		return
	}
	switch existing := top[listKey].(type) {
	case nil:
		top[listKey] = []any{nvidiaImagePullSecretName}
	case []any:
		if !containsString(existing, nvidiaImagePullSecretName) {
			top[listKey] = append([]any{nvidiaImagePullSecretName}, existing...)
		}
	}
}

// injectNestedFlatPullSecretList walks vals[topKey][midKey][listKey],
// creating intermediate maps as needed. If any intermediate value exists but
// isn't a map, the function returns without changes (preserves author intent).
func injectNestedFlatPullSecretList(vals map[string]any, topKey, midKey, listKey string) {
	topRaw, present := vals[topKey]
	if !present {
		vals[topKey] = map[string]any{midKey: map[string]any{listKey: []any{nvidiaImagePullSecretName}}}
		return
	}
	top, ok := topRaw.(map[string]any)
	if !ok {
		return
	}
	midRaw, midPresent := top[midKey]
	if !midPresent {
		top[midKey] = map[string]any{listKey: []any{nvidiaImagePullSecretName}}
		return
	}
	mid, ok := midRaw.(map[string]any)
	if !ok {
		return
	}
	switch existing := mid[listKey].(type) {
	case nil:
		mid[listKey] = []any{nvidiaImagePullSecretName}
	case []any:
		if !containsString(existing, nvidiaImagePullSecretName) {
			mid[listKey] = append([]any{nvidiaImagePullSecretName}, existing...)
		}
	}
}

func containsObjectNamed(list []any, name string) bool {
	for _, item := range list {
		if obj, ok := item.(map[string]any); ok && obj["name"] == name {
			return true
		}
	}
	return false
}

func containsString(list []any, s string) bool {
	for _, item := range list {
		if v, ok := item.(string); ok && v == s {
			return true
		}
	}
	return false
}

// disableChartSecretCreation sets vals[key] = {create: false, name: <name>}
// to instruct charts that conditionally template a Secret (NVIDIA convention:
// {{- if .Values.<key>.create -}}) to skip rendering it, while still telling
// the chart which existing Secret name to wire into pod specs. The operator's
// pre-delivered Secret then survives the install/upgrade unmangled.
//
// Merge rules:
//   - vals[key] absent or wrong shape → replace with {create:false, name}
//   - vals[key] is a map → set create=false; only set name if absent (honor
//     any explicit override from the user's values)
func disableChartSecretCreation(vals map[string]any, key, name string) {
	existing, ok := vals[key].(map[string]any)
	if !ok {
		vals[key] = map[string]any{"create": false, "name": name}
		return
	}
	existing["create"] = false
	if _, hasName := existing["name"]; !hasName {
		existing["name"] = name
	}
}

// componentNamespace returns the namespace a blueprint component deploys into:
// the component's own TargetNamespace when set, else the workload's TargetNamespace.
func componentNamespace(w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent) string {
	if c.TargetNamespace != "" {
		return c.TargetNamespace
	}
	return w.Spec.TargetNamespace
}

// componentReleaseName returns the Helm release name for a blueprint component:
// the component's own ReleaseName when set, else the chart name (the historical
// default). Callers pass the result through capReleaseName, so an over-long
// override is still truncated to a valid release name.
func componentReleaseName(c aiplatformv1alpha1.BlueprintComponent) string {
	if c.ReleaseName != "" {
		return c.ReleaseName
	}
	return c.ChartName
}

// buildComponentMatrix returns one sorted (component, cluster) cell per desired HelmOp key,
// render-gated on the parent Bundle. A missing BundleDeployment for an expected cluster yields
// a Pending cell (never absent).
func (r *AIWorkloadReconciler) buildComponentMatrix(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	keys []HelmOpKey,
	expectedDigests map[string]string,
) ([]aiplatformv1alpha1.AIWorkloadComponentStatus, error) {
	cells := []aiplatformv1alpha1.AIWorkloadComponentStatus{}
	epoch := r.retryEpochValue(w)
	for _, k := range keys {
		b, err := r.getBundle(ctx, k.Namespace, k.Name)
		if err != nil {
			return nil, err
		}
		current := bundleRenderCurrent(b, expectedDigests[k.Namespace+"/"+k.Name])

		bdList := &unstructured.UnstructuredList{}
		bdList.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeploymentList",
		})
		// Scope by BOTH bundle-name and bundle-namespace: a mixed local+downstream workload has
		// the same bundle name in fleet-local and fleet-default, so name alone would match the
		// other parent's BundleDeployments — producing duplicate (component,cluster) cells and
		// cross-parent render gating.
		if err := r.List(ctx, bdList, client.MatchingLabels{
			"fleet.cattle.io/bundle-name":      k.Name,
			"fleet.cattle.io/bundle-namespace": k.Namespace,
		}); err != nil {
			return nil, err
		}

		// Track which expected cluster IDs have existing BundleDeployments.
		seenClusters := make(map[string]bool)
		for i := range bdList.Items {
			bd := &bdList.Items[i]
			clusterID, _, _ := unstructured.NestedString(bd.Object, "metadata", "labels", "fleet.cattle.io/cluster")
			if clusterID == "" {
				continue
			}
			seenClusters[clusterID] = true
			phase := matrixCellPhase(bd, current)
			msg := ""
			if phase == aiplatformv1alpha1.AIWorkloadClusterPhaseFailed {
				msg, _, _ = unstructured.NestedString(bd.Object, "status", "display", "message")
			}
			rev, _, _ := unstructured.NestedString(bd.Object, "status", "appliedDeploymentID")
			cells = append(cells, aiplatformv1alpha1.AIWorkloadComponentStatus{
				ComponentName: k.ComponentChartName, ClusterID: clusterID, Phase: phase,
				Revision: rev, Message: truncateMessage(msg),
			})
		}

		// Determine expected cluster IDs for this key based on namespace and TargetClusters.
		expectedClusters := []string{}
		if k.Namespace == "fleet-local" {
			// fleet-local → expected = the "local" entry if present in TargetClusters
			for _, id := range w.Spec.TargetClusters {
				if id == "local" {
					expectedClusters = append(expectedClusters, id)
					break
				}
			}
		} else if k.Namespace == "fleet-default" {
			// fleet-default → expected = the non-"local" entries of TargetClusters
			for _, id := range w.Spec.TargetClusters {
				if id != "local" {
					expectedClusters = append(expectedClusters, id)
				}
			}
		}

		// For each expected cluster not seen, check if Accepted=False is terminal.
		// If Bundle doesn't exist AND HelmOp has terminal Accepted=False, emit Failed.
		for _, expectedID := range expectedClusters {
			if !seenClusters[expectedID] {
				cellPhase := aiplatformv1alpha1.AIWorkloadClusterPhasePending
				if b == nil {
					// No Bundle exists: check if HelmOp has terminal Accepted=False.
					ho, err := r.getHelmOpIn(ctx, k.Namespace, k.Name)
					if err != nil {
						return nil, err
					}
					if ho != nil {
						// Find baseline for this HelmOp UID.
						var baseline *aiplatformv1alpha1.RenderBaseline
						for i := range w.Status.RenderBaselines {
							if w.Status.RenderBaselines[i].HelmOpUID == string(ho.GetUID()) {
								baseline = &w.Status.RenderBaselines[i]
								break
							}
						}
						digest := expectedDigests[k.Namespace+"/"+k.Name]
						if acceptedFalseTerminal(ho, baseline, digest, epoch, ho.GetGeneration()) {
							cellPhase = aiplatformv1alpha1.AIWorkloadClusterPhaseFailed
						}
					}
				}
				cells = append(cells, aiplatformv1alpha1.AIWorkloadComponentStatus{
					ComponentName: k.ComponentChartName,
					ClusterID:     expectedID,
					Phase:         cellPhase,
				})
			}
		}
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].ComponentName != cells[j].ComponentName {
			return cells[i].ComponentName < cells[j].ComponentName
		}
		return cells[i].ClusterID < cells[j].ClusterID
	})
	return cells, nil
}

// truncateMessage caps a Fleet message to 1 KiB for status storage.
func truncateMessage(s string) string {
	const max = 1024
	if len(s) > max {
		return s[:max]
	}
	return s
}

// phaseFromCells derives the top-level phase from matrix cells (reuses derivePhase semantics).
func phaseFromCells(cells []aiplatformv1alpha1.AIWorkloadComponentStatus) aiplatformv1alpha1.AIWorkloadPhase {
	statuses := make([]aiplatformv1alpha1.AIWorkloadClusterStatus, len(cells))
	for i, c := range cells {
		statuses[i] = aiplatformv1alpha1.AIWorkloadClusterStatus{ClusterID: c.ClusterID, Phase: c.Phase}
	}
	return derivePhase(statuses)
}

// equalComponentStatuses compares two slices of component statuses for equality.
func equalComponentStatuses(a, b []aiplatformv1alpha1.AIWorkloadComponentStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// cleanupStaleHelmOps removes HelmOps/Bundles (or git files) no longer desired, discovered by
// the workload-uid owner label so a mid-cleanup crash self-heals. (namespace,name) identities
// throughout. Backfills the label onto still-known FleetBundleNames HelmOps before diffing.
func (r *AIWorkloadReconciler) cleanupStaleHelmOps(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, desired []HelmOpKey) error {
	desiredSet := map[string]bool{}
	for _, k := range desired {
		desiredSet[k.Namespace+"/"+k.Name] = true
	}

	// Backfill the owner label onto pre-existing HelmOps named in FleetBundleNames.
	for _, name := range w.Spec.FleetBundleNames {
		for _, ns := range fleetNamespaces {
			ho, err := r.getHelmOpIn(ctx, ns, name)
			if err != nil {
				return err
			}
			if ho == nil {
				continue
			}
			if ho.GetLabels()[workloadUIDLabel] == "" {
				lbls := ho.GetLabels()
				if lbls == nil {
					lbls = map[string]string{}
				}
				lbls[workloadUIDLabel] = string(w.UID)
				ho.SetLabels(lbls)
				if err := r.Update(ctx, ho); err != nil {
					return err
				}
			}
		}
	}

	// Discover actual HelmOps by owner label across both fleet namespaces.
	for _, ns := range fleetNamespaces {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "HelmOpList"})
		if err := r.List(ctx, list, client.InNamespace(ns), client.MatchingLabels{workloadUIDLabel: string(w.UID)}); err != nil {
			return err
		}
		for i := range list.Items {
			name := list.Items[i].GetName()
			if desiredSet[ns+"/"+name] {
				continue
			}
			switch w.Spec.DeployStrategy {
			case aiplatformv1alpha1.AIWorkloadDeployGitOps:
				if err := r.deleteGitFileByName(ctx, w, name); err != nil {
					return err
				}
			default:
				// Delete only this (ns, name) — NOT the same name in the other fleet namespace,
				// which for a mixed local+downstream workload is a still-desired deployment.
				if err := r.deleteHelmOpIn(ctx, ns, name); err != nil {
					return err
				}
				if err := r.deleteBundleIn(ctx, ns, name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// getHelmOpIn fetches a HelmOp in a specific namespace, returning (nil,nil) when absent.
func (r *AIWorkloadReconciler) getHelmOpIn(ctx context.Context, ns, name string) (*unstructured.Unstructured, error) {
	ho := &unstructured.Unstructured{}
	ho.SetGroupVersionKind(helmOpGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, ho); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return ho, nil
}

// collectDesiredHelmOpUIDs fetches the UID of each desired HelmOp key that currently exists,
// returning a set (map[string]bool) for use with pruneRenderBaselines.
func (r *AIWorkloadReconciler) collectDesiredHelmOpUIDs(ctx context.Context, keys []HelmOpKey) map[string]bool {
	uids := make(map[string]bool)
	for _, k := range keys {
		ho, err := r.getHelmOpIn(ctx, k.Namespace, k.Name)
		if err != nil || ho == nil {
			continue
		}
		uids[string(ho.GetUID())] = true
	}
	return uids
}
