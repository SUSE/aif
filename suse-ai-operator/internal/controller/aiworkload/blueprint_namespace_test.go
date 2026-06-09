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

	aiplatformv1alpha1 "github.com/SUSE/suse-ai-operator/api/v1alpha1"
)

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

