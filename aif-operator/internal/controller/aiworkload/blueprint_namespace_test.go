package aiworkload

import (
	"context"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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

func newRepoFakeClient(t *testing.T) *AIWorkloadReconciler {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("suse-ai")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	return &AIWorkloadReconciler{Client: c, OperatorNamespace: "aif-operator"}
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
