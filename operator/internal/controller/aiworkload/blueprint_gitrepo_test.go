package aiworkload

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

type fakeCatalog struct {
	tgz []byte
	err error
}

func (f fakeCatalog) FetchChart(ctx context.Context, repo, chart, version string) ([]byte, error) {
	return f.tgz, f.err
}

func gitRepoTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = aiplatformv1alpha1.AddToScheme(scheme)
	scheme.AddKnownTypeWithName(clusterRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(helmOpGVK, &unstructured.Unstructured{})
	return scheme
}

func gitComponent() aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{
		ChartName:       "rancher-ai-agent",
		ChartRepo:       "rancher-charts",
		ChartVersion:    "109.0.1",
		TargetNamespace: "cattle-ai-agent-system",
	}
}

func TestEnsureBlueprintHelmOp_GitRepoEmitsBundle(t *testing.T) {
	scheme := gitRepoTestScheme()
	repo := repoObj("rancher-charts", map[string]any{
		"gitRepo": "https://git.rancher.io/charts", "gitBranch": "release-v2.14",
	})
	tgz := makeChartTgz(t, map[string]string{
		"rancher-ai-agent/Chart.yaml":        "apiVersion: v2\nname: rancher-ai-agent\nversion: 109.0.1\n",
		"rancher-ai-agent/templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n",
	})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme, CatalogClient: fakeCatalog{tgz: tgz}}

	w := &aiplatformv1alpha1.AIWorkload{}
	w.Name = "wl"
	w.Spec.TargetClusters = []string{"local"}

	if err := r.ensureBlueprintHelmOp(context.Background(), w, gitComponent(), "wl-agent"); err != nil {
		t.Fatalf("ensureBlueprintHelmOp: %v", err)
	}

	// A Bundle (not a HelmOp) must be created in fleet-local.
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(bundleGVK)
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "wl-agent"}, b); err != nil {
		t.Fatalf("expected Bundle in fleet-local: %v", err)
	}
	// helm.chart points at the unpacked chart directory, and the chart files are
	// carried as individual bundle resources.
	chart, _, _ := unstructured.NestedString(b.Object, "spec", "helm", "chart")
	if chart != "rancher-ai-agent" {
		t.Fatalf("helm.chart = %q", chart)
	}
	res, _, _ := unstructured.NestedSlice(b.Object, "spec", "resources")
	if _, ok := resourceByName(res, "rancher-ai-agent/Chart.yaml"); !ok {
		t.Fatalf("expected unpacked Chart.yaml resource, got %d resources", len(res))
	}

	// No HelmOp should exist for a git-backed component.
	ho := &unstructured.Unstructured{}
	ho.SetGroupVersionKind(helmOpGVK)
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "wl-agent"}, ho); err == nil {
		t.Fatal("did not expect a HelmOp for a git-backed component")
	}
}

func TestEnsureBlueprintHelmOp_GitRepoNoCatalogClient(t *testing.T) {
	scheme := gitRepoTestScheme()
	repo := repoObj("rancher-charts", map[string]any{"gitRepo": "https://git.rancher.io/charts"})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme} // CatalogClient nil

	w := &aiplatformv1alpha1.AIWorkload{}
	w.Name = "wl"
	w.Spec.TargetClusters = []string{"local"}

	if err := r.ensureBlueprintHelmOp(context.Background(), w, gitComponent(), "wl-agent"); err == nil {
		t.Fatal("expected error when catalog client is not configured")
	}
}
