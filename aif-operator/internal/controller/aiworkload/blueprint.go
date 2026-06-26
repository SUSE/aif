package aiworkload

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	igit "github.com/SUSE/aif-operator/internal/git"
	"github.com/SUSE/aif-operator/internal/registryurl"
)

var clusterRepoGVK = schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"}
var nonAlphanumBPRE = regexp.MustCompile(`[^a-z0-9]+`)

type clusterRepoInfo struct {
	URL            string
	ClientSecret   string // name of the basic-auth secret; empty if unauthenticated
	ClientSecretNS string // namespace of the basic-auth secret (typically cattle-system)
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
			w.Status.Phase = guardPhaseTransition(aiplatformv1alpha1.AIWorkloadPhaseFailed, w.Status.Phase, w.CreationTimestamp.Time)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Step 2: populate FleetBundleNames from components on first reconcile.
	if len(w.Spec.FleetBundleNames) == 0 {
		names := make([]string, 0, len(bp.Spec.Components))
		for _, c := range bp.Spec.Components {
			name := truncateName(w.Name+"-"+slugifyBP(c.ChartName), 63)
			names = append(names, name)
		}
		w.Spec.FleetBundleNames = names
		if err := r.Update(ctx, w); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Step 3: ensure HelmOps or git files exist for each component bundle.
	switch w.Spec.DeployStrategy {
	case aiplatformv1alpha1.AIWorkloadDeployFleetBundle:
		for i, c := range bp.Spec.Components {
			if i >= len(w.Spec.FleetBundleNames) {
				break
			}
			if err := r.ensureBlueprintHelmOp(ctx, w, c, w.Spec.FleetBundleNames[i]); err != nil {
				return ctrl.Result{}, err
			}
		}
	case aiplatformv1alpha1.AIWorkloadDeployGitOps:
		for i, c := range bp.Spec.Components {
			if i >= len(w.Spec.FleetBundleNames) {
				break
			}
			if err := r.ensureBlueprintGitFile(ctx, w, c, w.Spec.FleetBundleNames[i]); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// Step 4: aggregate status across all component bundles.
	return ctrl.Result{}, r.mirrorBlueprintStatus(ctx, w)
}

// ensureBlueprintHelmOp creates (or patches) the HelmOp for one blueprint component.
func (r *AIWorkloadReconciler) ensureBlueprintHelmOp(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	c aiplatformv1alpha1.BlueprintComponent,
	bundleName string,
) error {
	repoInfo, err := r.resolveClusterRepo(ctx, c.ChartRepo)
	if err != nil {
		return fmt.Errorf("resolve repo %q: %w", c.ChartRepo, err)
	}

	isOCI := strings.HasPrefix(repoInfo.URL, "oci://")
	helmSpec := map[string]any{
		"version":     c.ChartVersion,
		"releaseName": capReleaseName(bundleName),
		// Disable Fleet's ${ } value templating: we resolve all values ourselves,
		// and upstream charts legitimately use ${ } (e.g. OTel ${env:MY_POD_IP}),
		// which Fleet would otherwise mis-parse as a template function.
		"disablePreProcess": true,
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
	ns := componentNamespace(w, c)
	pullSecretName, err := r.ensureCombinedPullSecret(ctx, ns, repoInfo)
	if err != nil {
		log.FromContext(ctx).Error(err, "could not create image pull secret", "namespace", ns)
	}
	if pullSecretName != "" {
		pullSecrets := []any{map[string]any{"name": pullSecretName}}
		vals["imagePullSecrets"] = pullSecrets
		vals["global"] = map[string]any{"imagePullSecrets": pullSecrets}
	}
	if len(vals) > 0 {
		helmSpec["values"] = vals
	}

	localTargets := make([]any, 0)
	downstreamTargets := make([]any, 0)
	for _, id := range w.Spec.TargetClusters {
		if id == "local" {
			localTargets = append(localTargets, map[string]any{"clusterName": "local"})
		} else {
			downstreamTargets = append(downstreamTargets, map[string]any{
				"clusterSelector": map[string]any{
					"matchLabels": map[string]any{"management.cattle.io/cluster-name": id},
				},
			})
		}
	}

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
		// defaultNamespace (not namespace): targets the release namespace without
		// forcing every resource into it. Fleet's strict `namespace` field rejects
		// any cluster-scoped resource (ClusterRole, CRD, webhook), which breaks
		// operator/CRD-bearing charts.
		_ = unstructured.SetNestedField(ho.Object, ns, "spec", "defaultNamespace")
		_ = unstructured.SetNestedField(ho.Object, helmSpec, "spec", "helm")
		_ = unstructured.SetNestedSlice(ho.Object, pair.targets, "spec", "targets")
		if repoInfo.ClientSecret != "" {
			_ = unstructured.SetNestedField(ho.Object, repoInfo.ClientSecret, "spec", "helmSecretName")
		}

		if err := r.Patch(ctx, ho, client.Apply,
			client.ForceOwnership,
			client.FieldOwner("aif-operator"),
		); err != nil {
			return fmt.Errorf("patch HelmOp %s/%s: %w", pair.ns, bundleName, err)
		}
	}
	return nil
}

const (
	defaultAppCollectionHost = "dp.apps.rancher.io"
	defaultSUSERegistryHost  = "registry.suse.com"
	defaultNvidiaHost        = "nvcr.io"
	combinedPullSecretName   = "suse-ai-pull-combined"
)

// ensureCombinedPullSecret creates (or updates) a single kubernetes.io/dockerconfigjson secret
// in targetNamespace whose "auths" map covers ALL configured registries: the component's own
// chartRepo, ApplicationCollection, and SUSERegistry from Settings. This ensures subchart
// images pulled from a different registry than the parent chart are also authenticated.
// Returns the secret name, or "" if no credentials are available.
func (r *AIWorkloadReconciler) ensureCombinedPullSecret(ctx context.Context, targetNamespace string, repoInfo clusterRepoInfo) (string, error) {
	auths := map[string]any{}

	// Component's own chartRepo credentials.
	if repoInfo.ClientSecret != "" {
		src := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: repoInfo.ClientSecretNS, Name: repoInfo.ClientSecret}, src); err == nil {
			if u, p := string(src.Data["username"]), string(src.Data["password"]); u != "" && p != "" {
				auths[registryurl.Host(repoInfo.URL)] = dockerAuthEntry(u, p)
			}
		}
	}

	// All registry credentials configured in Settings.
	var s aiplatformv1alpha1.Settings
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: operatorSettingsName}, &s); err == nil {
		appHost := defaultAppCollectionHost
		if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.ApplicationCollection != "" {
			appHost = registryurl.Host(s.Spec.RegistryEndpoints.ApplicationCollection)
		}
		if s.Spec.ApplicationCollection.UserSecretRef != nil && s.Spec.ApplicationCollection.TokenSecretRef != nil {
			u, err1 := r.readSettingsSecretKey(ctx, s.Spec.ApplicationCollection.UserSecretRef)
			p, err2 := r.readSettingsSecretKey(ctx, s.Spec.ApplicationCollection.TokenSecretRef)
			if err1 == nil && err2 == nil && u != "" && p != "" {
				auths[appHost] = dockerAuthEntry(u, p)
			}
		}

		suseHost := defaultSUSERegistryHost
		if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.SUSERegistry != "" {
			suseHost = registryurl.Host(s.Spec.RegistryEndpoints.SUSERegistry)
		}
		if s.Spec.SUSERegistry.UserSecretRef != nil && s.Spec.SUSERegistry.TokenSecretRef != nil {
			u, err1 := r.readSettingsSecretKey(ctx, s.Spec.SUSERegistry.UserSecretRef)
			p, err2 := r.readSettingsSecretKey(ctx, s.Spec.SUSERegistry.TokenSecretRef)
			if err1 == nil && err2 == nil && u != "" && p != "" {
				auths[suseHost] = dockerAuthEntry(u, p)
			}
		}

		// NVIDIA images come from nvcr.io (connected); registryEndpoints.nvidia is the chart-repo
		// OCI URL, not an image host, and air-gap redirection is a node-level concern.
		if s.Spec.Nvidia.UserSecretRef != nil && s.Spec.Nvidia.TokenSecretRef != nil {
			u, err1 := r.readSettingsSecretKey(ctx, s.Spec.Nvidia.UserSecretRef)
			p, err2 := r.readSettingsSecretKey(ctx, s.Spec.Nvidia.TokenSecretRef)
			if err1 == nil && err2 == nil && u != "" && p != "" {
				auths[defaultNvidiaHost] = dockerAuthEntry(u, p)
			}
		}
	}

	if len(auths) == 0 {
		return "", nil
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
	dst.APIVersion = "v1"
	dst.Kind = "Secret"
	dst.Name = combinedPullSecretName
	dst.Namespace = targetNamespace
	dst.Type = corev1.SecretTypeDockerConfigJson
	dst.Data = map[string][]byte{corev1.DockerConfigJsonKey: dockerCfg}
	if err := r.Patch(ctx, dst, client.Apply, client.ForceOwnership, client.FieldOwner("aif-operator")); err != nil {
		return "", err
	}
	return combinedPullSecretName, nil
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
) error {
	ho, err := r.getHelmOp(ctx, bundleName)
	if err != nil {
		return err
	}
	if ho != nil {
		return nil // already published
	}

	repoInfo, err := r.resolveClusterRepo(ctx, c.ChartRepo)
	if err != nil {
		return fmt.Errorf("resolve repo %q: %w", c.ChartRepo, err)
	}

	isOCI := strings.HasPrefix(repoInfo.URL, "oci://")
	helmSpec := map[string]any{
		"version":     c.ChartVersion,
		"releaseName": capReleaseName(bundleName),
		// Disable Fleet's ${ } value templating: we resolve all values ourselves,
		// and upstream charts legitimately use ${ } (e.g. OTel ${env:MY_POD_IP}),
		// which Fleet would otherwise mis-parse as a template function.
		"disablePreProcess": true,
	}
	if !isOCI {
		helmSpec["repo"] = repoInfo.URL
		helmSpec["chart"] = c.ChartName
	} else {
		helmSpec["repo"] = repoInfo.URL + "/" + c.ChartName
	}

	vals := map[string]any{}
	ns := componentNamespace(w, c)
	pullSecretName, err := r.ensureCombinedPullSecret(ctx, ns, repoInfo)
	if err != nil {
		log.FromContext(ctx).Error(err, "could not create image pull secret", "namespace", ns)
	}
	if pullSecretName != "" {
		pullSecrets := []any{map[string]any{"name": pullSecretName}}
		vals["imagePullSecrets"] = pullSecrets
		vals["global"] = map[string]any{"imagePullSecrets": pullSecrets}
	}
	if len(vals) > 0 {
		helmSpec["values"] = vals
	}

	targets := make([]any, 0)
	isLocalOnly := true
	for _, id := range w.Spec.TargetClusters {
		if id == "local" {
			targets = append(targets, map[string]any{"clusterName": "local"})
		} else {
			isLocalOnly = false
			targets = append(targets, map[string]any{
				"clusterSelector": map[string]any{
					"matchLabels": map[string]any{"management.cattle.io/cluster-name": id},
				},
			})
		}
	}
	if len(w.Spec.TargetClusters) == 0 {
		isLocalOnly = false
	}

	fleetNS := "fleet-default"
	if isLocalOnly {
		fleetNS = "fleet-local"
	}

	helmOpSpec := map[string]any{
		// defaultNamespace (not namespace): targets the release namespace without
		// forcing every resource into it. Fleet's strict `namespace` field rejects
		// any cluster-scoped resource (ClusterRole, CRD, webhook), which breaks
		// operator/CRD-bearing charts.
		"defaultNamespace": ns,
		"helm":             helmSpec,
		"targets":          targets,
	}
	if repoInfo.ClientSecret != "" {
		helmOpSpec["helmSecretName"] = repoInfo.ClientSecret
	}

	helmOpObj := map[string]any{
		"apiVersion": "fleet.cattle.io/v1alpha1",
		"kind":       "HelmOp",
		"metadata":   map[string]any{"name": bundleName, "namespace": fleetNS},
		"spec":       helmOpSpec,
	}

	yamlBytes, err := json.MarshalIndent(helmOpObj, "", "  ")
	if err != nil {
		return err
	}

	return r.publishBlueprintGitFile(ctx, w, bundleName, string(yamlBytes))
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

// mirrorBlueprintStatus aggregates BundleDeployment statuses across all component bundles.
// Per-cluster phase is the worst phase across all components for that cluster.
func (r *AIWorkloadReconciler) mirrorBlueprintStatus(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) error {
	clusterPhases := make(map[string]aiplatformv1alpha1.AIWorkloadClusterPhase)
	clusterMessages := make(map[string]string)

	for _, bundleName := range w.Spec.FleetBundleNames {
		bdList := &unstructured.UnstructuredList{}
		bdList.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeploymentList",
		})
		if err := r.List(ctx, bdList, client.MatchingLabels{
			"fleet.cattle.io/bundle-name": bundleName,
		}); err != nil {
			return err
		}
		for _, bd := range bdList.Items {
			clusterID, _, _ := unstructured.NestedString(bd.Object, "metadata", "labels", "fleet.cattle.io/cluster")
			if clusterID == "" {
				continue
			}
			state, _, _ := unstructured.NestedString(bd.Object, "status", "display", "state")
			message, _, _ := unstructured.NestedString(bd.Object, "status", "display", "message")
			phase := fleetStateToClusterPhase(state)
			existing, seen := clusterPhases[clusterID]
			if !seen {
				clusterPhases[clusterID] = phase
				if phase != aiplatformv1alpha1.AIWorkloadClusterPhaseRunning {
					clusterMessages[clusterID] = message
				}
			} else {
				worst := worstClusterPhase(existing, phase)
				clusterPhases[clusterID] = worst
				if worst != existing && message != "" {
					clusterMessages[clusterID] = message
				}
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
	w.Status.ClusterStatuses = statuses
	w.Status.Phase = guardPhaseTransition(derivePhase(statuses), w.Status.Phase, w.CreationTimestamp.Time)
	return nil
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
		return clusterRepoInfo{}, fmt.Errorf("get ClusterRepo %q: %w", repoName, err)
	}
	url, _, _ := unstructured.NestedString(cr.Object, "spec", "url")
	if url == "" {
		url, _, _ = unstructured.NestedString(cr.Object, "spec", "ociRepo")
	}
	if url == "" {
		return clusterRepoInfo{}, fmt.Errorf("ClusterRepo %q has no url or ociRepo in spec", repoName)
	}
	// spec.clientSecret is an object {name, namespace}, not a plain string.
	clientSecretName, _, _ := unstructured.NestedString(cr.Object, "spec", "clientSecret", "name")
	clientSecretNS, _, _ := unstructured.NestedString(cr.Object, "spec", "clientSecret", "namespace")
	if clientSecretNS == "" {
		clientSecretNS = "cattle-system"
	}
	return clusterRepoInfo{URL: url, ClientSecret: clientSecretName, ClientSecretNS: clientSecretNS}, nil
}

func bpCRName(familyName, version string) string {
	v := version
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	return slugifyBP(familyName) + "-" + strings.ReplaceAll(v, ".", "-")
}

func slugifyBP(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumBPRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func truncateName(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// componentNamespace returns the namespace a blueprint component deploys into:
// the component's own TargetNamespace when set, else the workload's TargetNamespace.
func componentNamespace(w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent) string {
	if c.TargetNamespace != "" {
		return c.TargetNamespace
	}
	return w.Spec.TargetNamespace
}
