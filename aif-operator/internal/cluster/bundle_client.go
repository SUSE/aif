package cluster

import (
	"context"
	"crypto/md5" //nolint:gosec // Fleet uses MD5 only to derive deterministic Helm release-name suffixes.
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// bundleGVK is the Fleet Bundle CR GVK we emit.
var bundleGVK = schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}

// BundleClient delivers a set of pull-secrets to a single downstream cluster
// as one consolidated Fleet Bundle (Namespace + Secrets + an SA-merge Job).
// One bundle per (owner, cluster) replaces the prior one-bundle-per-secret
// design — that one tripped Helm's adoption check when peer bundles shared
// a Namespace, and made downstream SA injection awkward (which Job goes in
// which bundle?). With one bundle per cluster, ownership is unambiguous.
type BundleClient interface {
	// ApplyPullSecretBundle writes a single Fleet Bundle to the target
	// cluster carrying the Namespace + every Secret in secrets + a Job that
	// merges the secret names into every ServiceAccount in the namespace.
	// Idempotent: re-applying with the same input updates the bundle in
	// place. All secrets must share the same .Namespace; ApplyPullSecretBundle
	// returns an error otherwise.
	ApplyPullSecretBundle(ctx context.Context, secrets []*corev1.Secret) error
}

// BundleClientOptions configures a bundleClient instance.
type BundleClientOptions struct {
	// ClusterID is the Rancher cluster ID Fleet targets (e.g. "c-abc123").
	// Use "local" for the operator's own cluster (the bundleClient would
	// then deliver via fleet-local rather than fleet-default).
	ClusterID string
	// FleetWorkspace is the Fleet workspace namespace the Bundle lives in.
	// Conventionally "fleet-default" for downstream clusters, "fleet-local"
	// for the operator's own cluster.
	FleetWorkspace string
	// OwnerName is the AIWorkload name owning this Bundle. Used in Bundle
	// naming and labels so the finalizer can delete it by label selector.
	OwnerName string
	// OwnerNamespace is the AIWorkload namespace owning this Bundle.
	OwnerNamespace string
	// Namespace is the target namespace this Bundle's secrets are written
	// into on the downstream cluster. Threaded through to the Bundle's
	// metadata.name so two ApplyPullSecretBundle calls for the same
	// (owner, cluster) but different namespaces emit two distinct Bundles
	// — blueprints whose components fan across multiple namespaces need
	// one Bundle per namespace, otherwise the second call clobbers the
	// first in fleet-default. When empty, the bundle name omits the
	// namespace segment (back-compat: matches the pre-multi-namespace
	// naming convention for App workloads and single-namespace blueprints).
	Namespace string
	// SAMergeImage is the container image used by the Job that patches
	// every ServiceAccount's imagePullSecrets in the target namespace.
	// Must contain `kubectl` on its PATH; the Job script uses only POSIX
	// shell builtins, `sort`, `tr`, and `kubectl` (no `jq`, `awk`, `sed`
	// or `grep`) so a minimal kubectl image is sufficient. Defaults to
	// the SUSE kubectl image when empty.
	SAMergeImage string
}

// NewBundleClient returns a BundleClient that emits one consolidated Fleet
// Bundle per ApplyPullSecretBundle call. Bundle name is
// "ai-pullsecrets-<OwnerName>-<ClusterID>"; owner labels
// (ai-platform.suse.com/owner-name and /owner-namespace) tie this Bundle
// to its AIWorkload for finalizer cleanup via label selector.
func NewBundleClient(c client.Client, scheme *runtime.Scheme, opts BundleClientOptions) BundleClient {
	if opts.SAMergeImage == "" {
		// Same image the extension-cleanup-job chart uses; available in
		// air-gapped environments via the SUSE Registry mirror.
		opts.SAMergeImage = "registry.suse.com/suse/kubectl:1.35"
	}
	return &bundleClient{c: c, scheme: scheme, opts: opts}
}

type bundleClient struct {
	c client.Client
	// scheme kept for symmetry with localClient; not currently used.
	scheme *runtime.Scheme
	opts   BundleClientOptions
}

const (
	// saMergeServiceAccount is the SA the SA-merge Job runs as on the
	// downstream cluster. The corresponding Role grants get/list/patch on
	// serviceaccounts in the target namespace only.
	saMergeServiceAccount = "ai-pullsecret-merger"
	// saMergeJobName template — actual name carries a hash to force a new
	// Job pod on every Bundle reapply (Job spec is immutable after create).
	saMergeJobNamePrefix = "ai-pullsecret-merge"
)

func (b *bundleClient) ApplyPullSecretBundle(ctx context.Context, secrets []*corev1.Secret) error {
	if len(secrets) == 0 {
		return nil
	}

	// All secrets must target the same namespace — the Bundle ships ONE
	// Namespace manifest and ONE SA-merge Job scoped to that namespace.
	ns := secrets[0].Namespace
	if ns == "" {
		return fmt.Errorf("ApplyPullSecretBundle: first secret has empty namespace")
	}
	secretNames := make([]string, 0, len(secrets))
	for _, sec := range secrets {
		if sec.Namespace != ns {
			return fmt.Errorf("ApplyPullSecretBundle: mixed namespaces (%s vs %s); a Bundle ships one namespace", ns, sec.Namespace)
		}
		secretNames = append(secretNames, sec.Name)
	}

	resources := make([]any, 0, 3+len(secrets))

	// 1) Namespace — Fleet wraps each Bundle as its own Helm release on the
	//    target cluster and applies manifests verbatim with no implicit
	//    namespace creation, so the namespace must ship in the Bundle.
	nsObj := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
			// The pull-secret release must never delete the workload namespace
			// when Fleet uninstalls or replaces the release. Secrets and the
			// merger RBAC remain release-owned; only the shared namespace is kept.
			Annotations: map[string]string{"helm.sh/resource-policy": "keep"},
		},
	}
	nsYAML, err := yaml.Marshal(nsObj)
	if err != nil {
		return fmt.Errorf("marshal namespace %s: %w", ns, err)
	}
	resources = append(resources, map[string]any{
		"name":    fmt.Sprintf("%s-namespace.yaml", ns),
		"content": string(nsYAML),
	})

	// 2) Each Secret.
	for _, sec := range secrets {
		out := sec.DeepCopy()
		out.APIVersion = "v1"
		out.Kind = "Secret"
		secYAML, err := yaml.Marshal(out)
		if err != nil {
			return fmt.Errorf("marshal secret %s/%s: %w", out.Namespace, out.Name, err)
		}
		resources = append(resources, map[string]any{
			"name":    fmt.Sprintf("%s-%s.yaml", out.Namespace, out.Name),
			"content": string(secYAML),
		})
	}

	// 3) SA-merge: ServiceAccount + Role + RoleBinding + Job. The Job
	//    enumerates every SA in the namespace and patches imagePullSecrets
	//    to include each of secretNames, preserving any pre-existing
	//    entries. Mirrors mergeImagePullSecrets() on the local cluster.
	saMergeYAML, err := buildSAMergeResources(ns, secretNames, b.opts.SAMergeImage)
	if err != nil {
		return fmt.Errorf("build SA-merge resources for ns %s: %w", ns, err)
	}
	resources = append(resources, map[string]any{
		"name":    fmt.Sprintf("%s-sa-merge.yaml", ns),
		"content": saMergeYAML,
	})

	bundleName := pullSecretBundleName(b.opts.OwnerName, b.opts.ClusterID, b.opts.Namespace)
	bundle := &unstructured.Unstructured{}
	bundle.SetGroupVersionKind(bundleGVK)
	bundle.SetName(bundleName)
	bundle.SetNamespace(b.opts.FleetWorkspace)
	bundle.SetLabels(map[string]string{
		"ai-platform.suse.com/owner-name":      b.opts.OwnerName,
		"ai-platform.suse.com/owner-namespace": b.opts.OwnerNamespace,
	})

	spec := map[string]any{
		"resources": resources,
		"targets": []any{
			map[string]any{
				"clusterName": b.opts.ClusterID,
			},
		},
		// takeOwnership lets this Bundle's Helm release adopt a Namespace
		// (or any other resource) that already exists with foreign Helm
		// ownership annotations. The new one-bundle-per-(owner, cluster)
		// design doesn't create cross-bundle namespace conflicts on its
		// own, but takeOwnership smooths upgrades from the prior
		// one-bundle-per-secret design: the old bundles annotated the
		// namespace as theirs, and without this flag the consolidated
		// bundle would refuse to install on existing clusters.
		"helm": map[string]any{
			// Fleet derives Helm release names from BundleDeployment names and
			// caps them at 53 characters. Its garbage collector compares against
			// the uncapped BundleDeployment name unless releaseName is explicit,
			// which can make it uninstall a healthy long-named release as
			// "unknown". Use Fleet's exact capping algorithm so existing releases
			// are adopted safely when this field first appears.
			"releaseName":   pullSecretReleaseName(bundleName),
			"takeOwnership": true,
		},
	}
	if err := unstructured.SetNestedField(bundle.Object, spec, "spec"); err != nil {
		return fmt.Errorf("set bundle spec: %w", err)
	}

	if err := b.c.Patch(ctx, bundle, client.Apply, client.ForceOwnership, client.FieldOwner("suse-ai-operator")); err != nil {
		return fmt.Errorf("apply bundle %s/%s: %w", bundle.GetNamespace(), bundle.GetName(), err)
	}
	return nil
}

const (
	pullSecretHelmReleaseNameMax = 53
	pullSecretHelmHashLen        = 5
)

// pullSecretReleaseName mirrors Fleet's names.HelmReleaseName/Limit behavior
// for the DNS-label-safe Bundle names produced by pullSecretBundleName.
// Matching Fleet's MD5-derived five-character suffix is migration-critical:
// an already-installed implicit release must get the same name after the
// operator starts setting spec.helm.releaseName explicitly.
func pullSecretReleaseName(name string) string {
	if len(name) <= pullSecretHelmReleaseNameMax {
		return name
	}

	digest := md5.Sum([]byte(name))
	suffix := hex.EncodeToString(digest[:])[:pullSecretHelmHashLen]
	headLen := pullSecretHelmReleaseNameMax - pullSecretHelmHashLen - 1
	separator := "-"
	if name[headLen-1] == '-' {
		separator = ""
	}
	return name[:headLen] + separator + suffix
}

// pullSecretBundleName builds the Bundle's metadata.name as
// `ai-pullsecrets-<owner>-<cluster>` (back-compat, when namespace is empty)
// or `ai-pullsecrets-<owner>-<cluster>-<namespace>` (multi-namespace case).
// The result is always a valid DNS-1123 label of ≤63 chars; long inputs are
// truncated with a deterministic FNV-1a/base36 suffix so distinct namespaces
// that share a long prefix don't collide on the same truncated name.
func pullSecretBundleName(owner, clusterID, namespace string) string {
	base := fmt.Sprintf("ai-pullsecrets-%s-%s", owner, clusterID)
	if namespace != "" {
		base = base + "-" + namespace
	}
	if len(base) <= 63 {
		return base
	}
	// 63 - 1 (separator) - 6 (hash) = 56 chars of head text.
	const max = 63
	const hashLen = 6
	h := fnv.New32a()
	_, _ = h.Write([]byte(base))
	suffix := strconv.FormatUint(uint64(h.Sum32()), 36)
	if len(suffix) > hashLen {
		suffix = suffix[:hashLen]
	}
	head := strings.TrimRight(base[:max-len(suffix)-1], "-")
	if head == "" {
		return suffix
	}
	return head + "-" + suffix
}
