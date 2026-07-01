package cluster_test

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

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
	if labels["ai-factory.suse.com/owner-name"] != "wl-x" {
		t.Errorf("owner-name label: got %q want wl-x", labels["ai-factory.suse.com/owner-name"])
	}
	if labels["ai-factory.suse.com/owner-namespace"] != "default" {
		t.Errorf("owner-namespace label: got %q want default", labels["ai-factory.suse.com/owner-namespace"])
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
	if !strings.Contains(contents[0], "helm.sh/resource-policy: keep") {
		t.Errorf("resources[0] Namespace must survive Helm uninstall, got:\n%s", contents[0])
	}

	// takeOwnership: lets the consolidated Bundle adopt resources
	// pre-annotated by an older per-secret bundle (upgrade compatibility).
	takeOwnership, found, err := unstructured.NestedBool(bundle.Object, "spec", "helm", "takeOwnership")
	if err != nil || !found || !takeOwnership {
		t.Errorf("spec.helm.takeOwnership: found=%v err=%v value=%v (want true)", found, err, takeOwnership)
	}
	releaseName, found, err := unstructured.NestedString(bundle.Object, "spec", "helm", "releaseName")
	if err != nil || !found || releaseName != bundleName {
		t.Errorf("spec.helm.releaseName: found=%v err=%v value=%q (want %q)", found, err, releaseName, bundleName)
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
	// RoleBinding, the one-shot Job, and the recurring CronJob. The args must
	// reference both secret names in the DESIRED list the shell script unions
	// with each SA's existing imagePullSecrets — the JSON is built per-SA from
	// the union, see sa_merge.go.
	saMerge := contents[3]
	for _, needle := range []string{
		"kind: ServiceAccount", "name: ai-pullsecret-merger",
		"kind: Role", "kind: RoleBinding",
		"kind: Job", "ai-pullsecret-merge-",
		// Recurring reconciliation so SAs/Pods created after the one-shot Job
		// ran still converge.
		"kind: CronJob", "name: ai-pullsecret-merge-cron", "schedule:",
		"ngc-api", "ngc-secret",
		// Owner-scope label selector — only chart-managed SAs/Pods are touched.
		"app.kubernetes.io/managed-by=Helm",
		// The bounce step needs pods RBAC; the SA patch needs serviceaccounts RBAC.
		`resources: ["serviceaccounts"]`,
		`resources: ["pods"]`,
		// The DESIRED literal must be on a single line; a multi-line
		// rendering inside the YAML `|` block-scalar would break Fleet's
		// post-render with "could not find expected ':'".
		"DESIRED='ngc-api ngc-secret'",
	} {
		if !strings.Contains(saMerge, needle) {
			t.Errorf("SA-merge manifest missing %q, got:\n%s", needle, saMerge)
		}
	}
	if strings.Contains(saMerge, "ttlSecondsAfterFinished") {
		t.Errorf("SA-merge Job must remain present for Fleet drift detection, got:\n%s", saMerge)
	}
	// DESIRED= must not contain a literal newline (would break the YAML
	// block-scalar). Anchor the assertion with strings.Index, not contains,
	// so we explicitly check what follows the opening quote.
	if i := strings.Index(saMerge, "DESIRED='"); i >= 0 {
		tail := saMerge[i+len("DESIRED='"):]
		if end := strings.Index(tail, "'"); end < 0 {
			t.Errorf("DESIRED literal has no closing quote — multi-line value broke the parse?\nfragment:\n%s", tail[:min(200, len(tail))])
		} else if strings.Contains(tail[:end], "\n") {
			t.Errorf("DESIRED literal spans multiple YAML lines — would break Fleet post-render. value=%q", tail[:end])
		}
	}

	// Whole-document YAML sanity: every document must parse without error.
	// This is the regression check for the line-42 ErrApplied bug seen
	// when DesiredNames was joined with '\n' instead of ' '.
	for i, doc := range strings.Split(saMerge, "\n---\n") {
		var out map[string]any
		if err := yaml.Unmarshal([]byte(doc), &out); err != nil {
			t.Errorf("SA-merge YAML document %d failed to parse: %v\n--- doc ---\n%s", i, err, doc)
		}
	}

	// The merge script is rendered once and spliced into both the Job and the
	// CronJob pod specs at different YAML depths via the `indent` template func.
	// Pull the script back out of each runner's parsed block scalar and assert
	// it is intact — this guards the indent splice (a wrong indent would make
	// the block scalar drop/mangle lines or fail to parse).
	for _, tc := range []struct {
		name string
		path func(map[string]any) []any
	}{
		{"Job", func(m map[string]any) []any {
			return digContainers(m, "spec", "template", "spec")
		}},
		{"CronJob", func(m map[string]any) []any {
			return digContainers(m, "spec", "jobTemplate", "spec", "template", "spec")
		}},
	} {
		var runner map[string]any
		for _, doc := range strings.Split(saMerge, "\n---\n") {
			var out map[string]any
			if err := yaml.Unmarshal([]byte(doc), &out); err != nil {
				continue
			}
			if out["kind"] == tc.name {
				runner = out
			}
		}
		if runner == nil {
			t.Errorf("SA-merge: no %s document found", tc.name)
			continue
		}
		containers := tc.path(runner)
		if len(containers) != 1 {
			t.Errorf("%s: want 1 container, got %d", tc.name, len(containers))
			continue
		}
		args, _ := containers[0].(map[string]any)["args"].([]any)
		if len(args) != 1 {
			t.Errorf("%s: want 1 arg, got %v", tc.name, args)
			continue
		}
		script, _ := args[0].(string)
		for _, frag := range []string{
			"set -eu",
			"DESIRED='ngc-api ngc-secret'",
			"kubectl -n \"$NS\" patch sa",
			// the Pod-bounce step must survive the splice intact
			"delete pod",
			"ImagePullBackOff",
			// the bounce must be gated on an SA actually being patched this
			// run, so a stable namespace never churns Pods (Pending<->Running).
			"PATCHED=0",
			`if [ "$PATCHED" = 1 ]; then`,
			// The namespace "default" SA must be in scope so subchart pods that
			// run under it (e.g. litellm's postgresql) get the combined creds.
			`printf 'default %s'`,
		} {
			if !strings.Contains(script, frag) {
				t.Errorf("%s script missing %q after block-scalar round-trip, got:\n%s", tc.name, frag, script)
			}
		}
	}
}

func TestBundleClient_ExplicitReleaseNameMatchesFleetCapping(t *testing.T) {
	t.Run("reported 55-byte bundle keeps existing Fleet release identity", func(t *testing.T) {
		bundle := applyTestPullSecretBundle(t, cluster.BundleClientOptions{
			ClusterID:      "c-58kz8",
			FleetWorkspace: "fleet-default",
			OwnerName:      "aiq-aira-c-58kz8",
			OwnerNamespace: "aiq-aira-system",
			Namespace:      "aiq-aira-system",
		})

		const wantBundle = "ai-pullsecrets-aiq-aira-c-58kz8-c-58kz8-aiq-aira-system"
		const wantRelease = "ai-pullsecrets-aiq-aira-c-58kz8-c-58kz8-aiq-air-f3763"
		if bundle.GetName() != wantBundle {
			t.Fatalf("bundle name: got %q want %q", bundle.GetName(), wantBundle)
		}
		got, found, err := unstructured.NestedString(bundle.Object, "spec", "helm", "releaseName")
		if err != nil || !found || got != wantRelease {
			t.Fatalf("releaseName: found=%v err=%v got=%q want=%q", found, err, got, wantRelease)
		}
	})

	for _, wantBundleLen := range []int{53, 54, 55} {
		t.Run(strconv.Itoa(wantBundleLen)+"-byte boundary", func(t *testing.T) {
			// `ai-pullsecrets-<owner>-c` contributes 17 bytes outside owner.
			owner := strings.Repeat("a", wantBundleLen-17)
			bundle := applyTestPullSecretBundle(t, cluster.BundleClientOptions{
				ClusterID:      "c",
				FleetWorkspace: "fleet-default",
				OwnerName:      owner,
				OwnerNamespace: "default",
			})
			if len(bundle.GetName()) != wantBundleLen {
				t.Fatalf("test setup: bundle length got %d want %d (%q)", len(bundle.GetName()), wantBundleLen, bundle.GetName())
			}

			releaseName, found, err := unstructured.NestedString(bundle.Object, "spec", "helm", "releaseName")
			if err != nil || !found {
				t.Fatalf("releaseName missing: found=%v err=%v", found, err)
			}
			if len(releaseName) > 53 {
				t.Errorf("releaseName length got %d, want <=53 (%q)", len(releaseName), releaseName)
			}
			if wantBundleLen == 53 && releaseName != bundle.GetName() {
				t.Errorf("53-byte name should pass through: got %q want %q", releaseName, bundle.GetName())
			}
			if wantBundleLen > 53 && releaseName == bundle.GetName() {
				t.Errorf("%d-byte name was not capped: %q", wantBundleLen, releaseName)
			}
			if !regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(releaseName) {
				t.Errorf("releaseName is not a DNS label: %q", releaseName)
			}
		})
	}

	t.Run("long shared prefixes remain distinct", func(t *testing.T) {
		prefix := strings.Repeat("a", 37)
		a := applyTestPullSecretBundle(t, cluster.BundleClientOptions{
			ClusterID: "c", FleetWorkspace: "fleet-default", OwnerName: prefix + "x", OwnerNamespace: "default",
		})
		b := applyTestPullSecretBundle(t, cluster.BundleClientOptions{
			ClusterID: "c", FleetWorkspace: "fleet-default", OwnerName: prefix + "y", OwnerNamespace: "default",
		})
		aRelease, _, _ := unstructured.NestedString(a.Object, "spec", "helm", "releaseName")
		bRelease, _, _ := unstructured.NestedString(b.Object, "spec", "helm", "releaseName")
		if aRelease == bRelease {
			t.Errorf("distinct long names collided at %q", aRelease)
		}
	})
}

func applyTestPullSecretBundle(t *testing.T, opts cluster.BundleClientOptions) unstructured.Unstructured {
	t.Helper()
	scheme := newBundleTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	bc := cluster.NewBundleClient(c, scheme, opts)
	secrets := []*corev1.Secret{{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-secret", Namespace: "target-ns"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
	}}
	if err := bc.ApplyPullSecretBundle(context.Background(), secrets); err != nil {
		t.Fatalf("ApplyPullSecretBundle: %v", err)
	}

	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles, client.InNamespace(opts.FleetWorkspace)); err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	if len(bundles.Items) != 1 {
		t.Fatalf("bundle count: got %d want 1", len(bundles.Items))
	}
	return bundles.Items[0]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// digContainers walks a nested map path (e.g. spec.template.spec) and returns
// the containers slice at the end of it. Returns nil if any segment is missing.
func digContainers(m map[string]any, path ...string) []any {
	cur := m
	for _, p := range path {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	containers, _ := cur["containers"].([]any)
	return containers
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
