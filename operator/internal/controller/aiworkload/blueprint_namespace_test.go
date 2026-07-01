package aiworkload

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// TestComponentNamespace covers the resolver added in ai-factory: a blueprint
// component can pin itself to a fixed namespace via BlueprintComponent.TargetNamespace,
// falling back to the workload's TargetNamespace when unset. This is what
// ensureBlueprintHelmOp uses for `ns` so per-component installs don't all land
// in the same install-time namespace.
func TestComponentNamespace(t *testing.T) {
	w := &aiplatformv1alpha1.AIWorkload{}
	w.Spec.TargetNamespace = "install-ns"

	t.Run("falls back to workload namespace when component unset", func(t *testing.T) {
		c := aiplatformv1alpha1.BlueprintComponent{ChartName: "a"}
		if got := componentNamespace(w, c); got != "install-ns" {
			t.Errorf("expected install-ns, got %q", got)
		}
	})

	t.Run("uses component namespace when set", func(t *testing.T) {
		c := aiplatformv1alpha1.BlueprintComponent{ChartName: "a", TargetNamespace: "fixed-ns"}
		if got := componentNamespace(w, c); got != "fixed-ns" {
			t.Errorf("expected fixed-ns, got %q", got)
		}
	})
}

// TestEnsureNamespace covers the SSA-based namespace-create helper added in
// ai-factory. The operator is not granted list/watch on namespaces, so a
// cached Get would force a Namespace informer that fails to sync. The helper
// must therefore bypass the cache via server-side apply.
func TestEnsureNamespace(t *testing.T) {
	scheme := kruntime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	t.Run("creates namespace when missing", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &AIWorkloadReconciler{Client: c}
		if err := r.ensureNamespace(context.Background(), "fixed-ns"); err != nil {
			t.Fatalf("ensureNamespace: %v", err)
		}
		ns := &corev1.Namespace{}
		if err := r.Get(context.Background(), types.NamespacedName{Name: "fixed-ns"}, ns); err != nil {
			t.Fatalf("expected namespace to be created, got: %v", err)
		}
	})

	t.Run("idempotent when namespace already exists", func(t *testing.T) {
		existing := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "install-ns"}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
		r := &AIWorkloadReconciler{Client: c}
		if err := r.ensureNamespace(context.Background(), "install-ns"); err != nil {
			t.Fatalf("ensureNamespace on existing ns should not error: %v", err)
		}
	})
}

// TestEnsureBlueprintHelmOp_UsesDefaultNamespace verifies the HelmOp created
// by ensureBlueprintHelmOp uses spec.defaultNamespace (Fleet's DEFAULTER) rather
// than spec.namespace (Fleet's FORCER that rejects cluster-scoped resources).
//
// Fleet's BundleDeploymentOptions:
//   - namespace        — assigns ALL resources to this ns AND fails the
//     deployment if any cluster-scoped resource exists.
//   - defaultNamespace — default for resources that don't specify a ns;
//     does NOT enforce/lock-down, so cluster-scoped resources render fine.
//
// We need the DEFAULTER so charts with ClusterRole / CRD / ClusterRoleBinding
// (k8s-nim-operator, NVIDIA GPU operator, most operator charts) can deploy.
func TestEnsureBlueprintHelmOp_UsesDefaultNamespace(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "nim-app"

	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	// Register HelmOp + ClusterRepo as Unstructured so the fake client can store them.
	scheme.AddKnownTypeWithName(helmOpGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "HelmOpList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(clusterRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})

	// ClusterRepo the BlueprintComponent's ChartRepo points at.
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("nvidia")
	_ = unstructured.SetNestedField(repo.Object, "https://helm.ngc.nvidia.com/nvidia", "spec", "url")

	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: opNS},
		Spec:       aiplatformv1alpha1.SettingsSpec{}, // no NVIDIA creds → injector is a no-op
	}

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(settings).
		WithObjects(repo).
		Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName:     "wl",
			TargetNamespace: targetNS,
			TargetClusters:  []string{"local"},
			DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint: &aiplatformv1alpha1.BlueprintSource{
					Name: "bp", Version: "0.0.1",
				},
			},
		},
	}
	comp := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo:    "nvidia",
		ChartName:    "k8s-nim-operator",
		ChartVersion: "3.1.1",
		Vendor:       aiplatformv1alpha1.ComponentVendorNvidia,
	}

	if err := r.ensureBlueprintHelmOp(context.Background(), w, comp, "bp-comp"); err != nil {
		t.Fatalf("ensureBlueprintHelmOp: %v", err)
	}

	// local-only TargetClusters → HelmOp lands in fleet-local.
	var ho unstructured.Unstructured
	ho.SetGroupVersionKind(helmOpGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "bp-comp"}, &ho); err != nil {
		t.Fatalf("HelmOp not found in fleet-local: %v", err)
	}

	// Critical: spec.defaultNamespace == TargetNamespace, spec.namespace UNSET.
	defaultNS, _, _ := unstructured.NestedString(ho.Object, "spec", "defaultNamespace")
	if defaultNS != targetNS {
		t.Errorf("spec.defaultNamespace: got %q want %q", defaultNS, targetNS)
	}
	forceNS, found, _ := unstructured.NestedString(ho.Object, "spec", "namespace")
	if found && forceNS != "" {
		t.Errorf("spec.namespace must NOT be set (FORCER rejects cluster-scoped); got %q", forceNS)
	}

	// takeOwnership=true: required so the chart's helm install can adopt
	// operator-delivered ngc-secret/ngc-api/suse-ai-pull-combined that the
	// pull-secret bundle already stamped with a different release name.
	// Without this, NVIDIA NIM-family charts (whose templates default to
	// `imagePullSecret.create: true`) abort at install with
	// "Secret … cannot be imported into the current release".
	takeOwnership, found, err := unstructured.NestedBool(ho.Object, "spec", "helm", "takeOwnership")
	if err != nil || !found || !takeOwnership {
		t.Errorf("spec.helm.takeOwnership: found=%v err=%v value=%v (want true)", found, err, takeOwnership)
	}
}

// TestHelmOpNamespace_PrefersDefaultNamespace verifies the GitOps sync-back
// read path resolves the effective HelmOp namespace from spec.defaultNamespace
// (Phase 5+ writes) and falls back to spec.namespace for HelmOps last written
// by older operator versions.
//
// Without this preference, sync-back reads an empty string for any HelmOp
// written by a Phase-5-or-later operator and silently disables the
// namespace-update branch (and feeds an empty namespace into helmOpHash's
// change-detection).
func TestHelmOpNamespace_PrefersDefaultNamespace(t *testing.T) {
	cases := []struct {
		name             string
		defaultNamespace string
		namespace        string
		want             string
	}{
		{
			name:             "new format only (Phase 5+)",
			defaultNamespace: "nim-app",
			want:             "nim-app",
		},
		{
			name:      "legacy format only (pre-Phase-5)",
			namespace: "legacy-ns",
			want:      "legacy-ns",
		},
		{
			name:             "both set — prefer defaultNamespace",
			defaultNamespace: "new-ns",
			namespace:        "old-ns",
			want:             "new-ns",
		},
		{
			name: "neither set",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ho := &unstructured.Unstructured{}
			ho.SetGroupVersionKind(helmOpGVK)
			if tc.defaultNamespace != "" {
				_ = unstructured.SetNestedField(ho.Object, tc.defaultNamespace, "spec", "defaultNamespace")
			}
			if tc.namespace != "" {
				_ = unstructured.SetNestedField(ho.Object, tc.namespace, "spec", "namespace")
			}
			if got := helmOpNamespace(ho); got != tc.want {
				t.Errorf("helmOpNamespace: got %q want %q", got, tc.want)
			}
		})
	}
}

// --- Shared helpers + integration test from ai-factory's namespace-resolution work ---

// newRepoFakeClient builds the lightweight reconciler used by the
// namespace-resolution test below. The fake client only knows about the
// "suse-ai" ClusterRepo (a generic HTTP repo with no auth — sufficient for
// resolveClusterRepo to succeed) and registers the scheme types the test
// actually exercises.
func newRepoFakeClient(t *testing.T) *AIWorkloadReconciler {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	scheme.AddKnownTypeWithName(helmOpGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "HelmOpList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(clusterRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("suse-ai")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	return &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}
}

func helmOpDefaultNamespace(t *testing.T, r *AIWorkloadReconciler, name string) string {
	t.Helper()
	ho := &unstructured.Unstructured{}
	ho.SetGroupVersionKind(helmOpGVK)
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: name}, ho); err != nil {
		t.Fatalf("get HelmOp %s: %v", name, err)
	}
	ns, _, _ := unstructured.NestedString(ho.Object, "spec", "defaultNamespace")
	return ns
}

func TestEnsureBlueprintHelmOp_NamespaceResolution(t *testing.T) {
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "aif-operator"},
	}
	w.Spec.TargetNamespace = "install-ns"
	w.Spec.TargetClusters = []string{"local"}

	t.Run("component override wins", func(t *testing.T) {
		r := newRepoFakeClient(t)
		c := aiplatformv1alpha1.BlueprintComponent{ChartRepo: "suse-ai", ChartName: "pinned", ChartVersion: "1.0.0", TargetNamespace: "fixed-ns"}
		if err := r.ensureBlueprintHelmOp(context.Background(), w, c, "wl-pinned"); err != nil {
			t.Fatalf("ensureBlueprintHelmOp: %v", err)
		}
		if got := helmOpDefaultNamespace(t, r, "wl-pinned"); got != "fixed-ns" {
			t.Errorf("expected defaultNamespace fixed-ns, got %q", got)
		}
	})

	t.Run("falls back to install namespace", func(t *testing.T) {
		r := newRepoFakeClient(t)
		c := aiplatformv1alpha1.BlueprintComponent{ChartRepo: "suse-ai", ChartName: "plain", ChartVersion: "1.0.0"}
		if err := r.ensureBlueprintHelmOp(context.Background(), w, c, "wl-plain"); err != nil {
			t.Fatalf("ensureBlueprintHelmOp: %v", err)
		}
		if got := helmOpDefaultNamespace(t, r, "wl-plain"); got != "install-ns" {
			t.Errorf("expected defaultNamespace install-ns, got %q", got)
		}
	})
}
