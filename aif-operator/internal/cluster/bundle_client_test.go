package cluster_test

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/SUSE/aif-operator/internal/cluster"
)

var bundleGVK = schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
var bundleListGVK = schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"}

// TestBundleClient_EmitsConsolidatedBundle covers the happy path for a
// multi-secret bundle: name, owner labels, target, and the full resource
// list (namespace + every secret + SA-merge manifests).
func TestBundleClient_EmitsConsolidatedBundle(t *testing.T) {
	scheme := newBundleTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	bc := cluster.NewBundleClient(c, scheme, cluster.BundleClientOptions{
		ClusterID:      "c-abc123",
		FleetWorkspace: "fleet-default",
		OwnerName:      "wl-x",
		OwnerNamespace: "default",
	})

	pull := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-secret", Namespace: "target-ns"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
	}
	api := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-api", Namespace: "target-ns"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"NGC_API_KEY": []byte("nvapi-test")},
	}
	if err := bc.ApplyPullSecretBundle(context.Background(), []*corev1.Secret{pull, api}); err != nil {
		t.Fatalf("ApplyPullSecretBundle: %v", err)
	}

	// Bundle name: one per (owner, cluster), no secret-name suffix.
	bundleName := "ai-pullsecrets-wl-x-c-abc123"
	var bundle unstructured.Unstructured
	bundle.SetGroupVersionKind(bundleGVK)
	if err := c.Get(context.Background(),
		client.ObjectKey{Namespace: "fleet-default", Name: bundleName}, &bundle); err != nil {
		t.Fatalf("get bundle %s/%s: %v", "fleet-default", bundleName, err)
	}

	labels := bundle.GetLabels()
	if labels["ai-platform.suse.com/owner-name"] != "wl-x" {
		t.Errorf("owner-name label: got %q want wl-x", labels["ai-platform.suse.com/owner-name"])
	}
	if labels["ai-platform.suse.com/owner-namespace"] != "default" {
		t.Errorf("owner-namespace label: got %q want default", labels["ai-platform.suse.com/owner-namespace"])
	}

	targets, found, err := unstructured.NestedSlice(bundle.Object, "spec", "targets")
	if err != nil || !found || len(targets) == 0 {
		t.Fatalf("spec.targets missing: found=%v err=%v", found, err)
	}
	t0, _ := targets[0].(map[string]any)
	if name, _ := t0["clusterName"].(string); name != "c-abc123" {
		t.Errorf("target clusterName: got %v want c-abc123", name)
	}

	// Resources layout: [Namespace, pull Secret, api Secret, SA-merge bundle].
	resources, found, err := unstructured.NestedSlice(bundle.Object, "spec", "resources")
	if err != nil || !found || len(resources) != 4 {
		t.Fatalf("spec.resources count: found=%v err=%v len=%d (want 4: namespace + 2 secrets + sa-merge)", found, err, len(resources))
	}

	contents := make([]string, len(resources))
	for i, r := range resources {
		rm, _ := r.(map[string]any)
		contents[i], _ = rm["content"].(string)
		if name, _ := rm["name"].(string); name == "" {
			t.Errorf("resources[%d].name is empty", i)
		}
	}

	// resources[0] = Namespace
	if !strings.Contains(contents[0], "kind: Namespace") || !strings.Contains(contents[0], "name: target-ns") {
		t.Errorf("resources[0] should be Namespace target-ns, got:\n%s", contents[0])
	}

	// takeOwnership: lets the consolidated Bundle adopt resources
	// pre-annotated by an older per-secret bundle (upgrade compatibility).
	takeOwnership, found, err := unstructured.NestedBool(bundle.Object, "spec", "helm", "takeOwnership")
	if err != nil || !found || !takeOwnership {
		t.Errorf("spec.helm.takeOwnership: found=%v err=%v value=%v (want true)", found, err, takeOwnership)
	}
	// resources[1] = pull secret
	if !strings.Contains(contents[1], "ngc-secret") || !strings.Contains(contents[1], "kubernetes.io/dockerconfigjson") {
		t.Errorf("resources[1] should be ngc-secret of dockerconfigjson type, got:\n%s", contents[1])
	}
	// resources[2] = api secret
	if !strings.Contains(contents[2], "ngc-api") || !strings.Contains(contents[2], "NGC_API_KEY") {
		t.Errorf("resources[2] should be ngc-api Opaque with NGC_API_KEY, got:\n%s", contents[2])
	}
	// resources[3] = SA-merge: must include the merger ServiceAccount, Role,
	// RoleBinding, and Job. The Job's args must reference both secret names
	// in jq's "wanted" array.
	saMerge := contents[3]
	for _, needle := range []string{
		"kind: ServiceAccount", "name: ai-pullsecret-merger",
		"kind: Role", "kind: RoleBinding",
		"kind: Job", "ai-pullsecret-merge-",
		`"ngc-api"`, `"ngc-secret"`,
	} {
		if !strings.Contains(saMerge, needle) {
			t.Errorf("SA-merge manifest missing %q, got:\n%s", needle, saMerge)
		}
	}
}

// TestBundleClient_Idempotent confirms re-applying the same input does not
// proliferate bundles — one (owner, cluster) → exactly one Bundle.
func TestBundleClient_Idempotent(t *testing.T) {
	scheme := newBundleTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	bc := cluster.NewBundleClient(c, scheme, cluster.BundleClientOptions{
		ClusterID: "c-abc", FleetWorkspace: "fleet-default", OwnerName: "wl", OwnerNamespace: "default",
	})
	secrets := []*corev1.Secret{{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-secret", Namespace: "target-ns"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
	}}
	for i := 0; i < 3; i++ {
		if err := bc.ApplyPullSecretBundle(context.Background(), secrets); err != nil {
			t.Fatalf("ApplyPullSecretBundle #%d: %v", i, err)
		}
	}

	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles, client.InNamespace("fleet-default")); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bundles.Items) != 1 {
		t.Errorf("expected exactly 1 bundle after 3 applies, got %d", len(bundles.Items))
	}
}

// TestBundleClient_EmptySecrets_NoOp covers the early-return path when the
// caller has nothing to deliver.
func TestBundleClient_EmptySecrets_NoOp(t *testing.T) {
	scheme := newBundleTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	bc := cluster.NewBundleClient(c, scheme, cluster.BundleClientOptions{
		ClusterID: "c-abc", FleetWorkspace: "fleet-default", OwnerName: "wl", OwnerNamespace: "default",
	})
	if err := bc.ApplyPullSecretBundle(context.Background(), nil); err != nil {
		t.Fatalf("ApplyPullSecretBundle(nil): %v", err)
	}
	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles, client.InNamespace("fleet-default")); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bundles.Items) != 0 {
		t.Errorf("expected 0 bundles for empty input, got %d", len(bundles.Items))
	}
}

// TestBundleClient_MixedNamespaces_Errors guards the invariant that one
// Bundle ships one Namespace; the operator's call sites always pass secrets
// from the same target namespace, but a future refactor that violates this
// should fail loudly rather than silently dropping work.
func TestBundleClient_MixedNamespaces_Errors(t *testing.T) {
	scheme := newBundleTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	bc := cluster.NewBundleClient(c, scheme, cluster.BundleClientOptions{
		ClusterID: "c-abc", FleetWorkspace: "fleet-default", OwnerName: "wl", OwnerNamespace: "default",
	})
	secrets := []*corev1.Secret{
		{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns-a"}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"k": []byte("v")}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns-b"}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"k": []byte("v")}},
	}
	err := bc.ApplyPullSecretBundle(context.Background(), secrets)
	if err == nil {
		t.Fatalf("expected error for mixed namespaces, got nil")
	}
	if !strings.Contains(err.Error(), "mixed namespaces") {
		t.Errorf("expected error to mention mixed namespaces, got: %v", err)
	}
}

// newBundleTestScheme builds a runtime.Scheme that knows about corev1 (for
// the Secret) and registers the Fleet Bundle GVK so the fake client accepts
// unstructured objects of that kind.
func newBundleTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	// Register Bundle as unstructured so the fake client can roundtrip it.
	s.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})
	return s
}
