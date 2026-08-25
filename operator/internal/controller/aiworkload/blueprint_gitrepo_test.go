package aiworkload

import (
	"context"
	stderrors "errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
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
	holder := rancher.NewHolder()
	holder.Set(fakeCatalog{tgz: tgz})
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme, CatalogClient: holder}

	w := &aiplatformv1alpha1.AIWorkload{}
	w.Name = "wl"
	w.Spec.TargetClusters = []string{"local"}

	if _, err := r.ensureBlueprintHelmOp(context.Background(), w, gitComponent(), "wl-agent"); err != nil {
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

// countingCatalog records how many times the chart was actually downloaded.
type countingCatalog struct {
	tgz   []byte
	calls int
}

func (f *countingCatalog) FetchChart(ctx context.Context, repo, chart, version string) ([]byte, error) {
	f.calls++
	return f.tgz, nil
}

// refetchHarness wires a git-backed ClusterRepo (optionally carrying
// status.commit) to a counting catalog, and returns a reconcile function so a
// test can drive ensureBlueprintHelmOp repeatedly against the same state.
type refetchHarness struct {
	r       *AIWorkloadReconciler
	catalog *countingCatalog
	repo    *unstructured.Unstructured
	cl      crclient.Client
}

func newRefetchHarness(t *testing.T, commit string) *refetchHarness {
	t.Helper()
	scheme := gitRepoTestScheme()
	repo := repoObj("rancher-charts", map[string]any{
		"gitRepo": "https://git.rancher.io/charts", "gitBranch": "release-v2.14",
	})
	if commit != "" {
		_ = unstructured.SetNestedField(repo.Object, commit, "status", "commit")
	}
	tgz := makeChartTgz(t, map[string]string{
		"rancher-ai-agent/Chart.yaml":        "apiVersion: v2\nname: rancher-ai-agent\nversion: 109.0.1\n",
		"rancher-ai-agent/templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n",
	})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	catalog := &countingCatalog{tgz: tgz}
	holder := rancher.NewHolder()
	holder.Set(catalog)
	return &refetchHarness{
		r:       &AIWorkloadReconciler{Client: cl, Scheme: scheme, CatalogClient: holder},
		catalog: catalog,
		repo:    repo,
		cl:      cl,
	}
}

func (h *refetchHarness) reconcile(t *testing.T, c aiplatformv1alpha1.BlueprintComponent) {
	t.Helper()
	w := &aiplatformv1alpha1.AIWorkload{}
	w.Name = "wl"
	w.Spec.TargetClusters = []string{"local"}
	if _, err := h.r.ensureBlueprintHelmOp(context.Background(), w, c, "wl-agent"); err != nil {
		t.Fatalf("ensureBlueprintHelmOp: %v", err)
	}
}

// setCommit rewrites the ClusterRepo's status.commit, simulating Rancher
// re-cloning a branch that moved.
func (h *refetchHarness) setCommit(t *testing.T, commit string) {
	t.Helper()
	_ = unstructured.SetNestedField(h.repo.Object, commit, "status", "commit")
	if err := h.cl.Update(context.Background(), h.repo); err != nil {
		t.Fatalf("update ClusterRepo status.commit: %v", err)
	}
}

// The fingerprint short circuit is what stops a healthy git-backed workload from
// re-downloading its chart from Rancher on every BundleDeployment status churn
// (commit 8ead8bf, "stop the refetch loop"). A regression there is completely
// silent: no assertion on Bundle content changes, and the only symptom is load
// on Rancher plus a workload that reconciles forever. So count the downloads.
func TestEnsureBlueprintHelmOp_GitRepoRefetchSuppression(t *testing.T) {
	withValues := func(raw string) aiplatformv1alpha1.BlueprintComponent {
		c := gitComponent()
		c.Values = &apixv1.JSON{Raw: []byte(raw)}
		return c
	}

	t.Run("unchanged component is not re-fetched", func(t *testing.T) {
		h := newRefetchHarness(t, "commit-aaa")
		h.reconcile(t, gitComponent())
		h.reconcile(t, gitComponent())
		if h.catalog.calls != 1 {
			t.Fatalf("FetchChart called %d times, want 1 — the fingerprint short circuit did not suppress the second fetch", h.catalog.calls)
		}
	})

	t.Run("changed values force a re-fetch", func(t *testing.T) {
		h := newRefetchHarness(t, "commit-aaa")
		h.reconcile(t, withValues(`{"replicaCount":1}`))
		h.reconcile(t, withValues(`{"replicaCount":2}`))
		if h.catalog.calls != 2 {
			t.Fatalf("FetchChart called %d times, want 2 — a values change must rebuild the Bundle", h.catalog.calls)
		}
	})

	// Section 1's regression guard at the reconcile level, and the case that
	// matters most for this branch: a git-backed repo tracks a branch, so the
	// same chart version can change underneath us. Only status.commit reveals it.
	t.Run("changed repo commit forces a re-fetch", func(t *testing.T) {
		h := newRefetchHarness(t, "commit-aaa")
		h.reconcile(t, gitComponent())
		h.setCommit(t, "commit-bbb")
		h.reconcile(t, gitComponent())
		if h.catalog.calls != 2 {
			t.Fatalf("FetchChart called %d times, want 2 — a chart re-pushed at the same version under a moved branch was never re-fetched", h.catalog.calls)
		}
	})
}

func TestEnsureBlueprintHelmOp_GitRepoNoCatalogClient(t *testing.T) {
	scheme := gitRepoTestScheme()
	repo := repoObj("rancher-charts", map[string]any{"gitRepo": "https://git.rancher.io/charts"})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme} // CatalogClient nil

	w := &aiplatformv1alpha1.AIWorkload{}
	w.Name = "wl"
	w.Spec.TargetClusters = []string{"local"}

	_, err := r.ensureBlueprintHelmOp(context.Background(), w, gitComponent(), "wl-agent")
	if err == nil {
		t.Fatal("expected error when catalog client is not configured")
	}
	if !stderrors.Is(err, errCatalogClientNotConfigured) {
		t.Fatalf("expected errCatalogClientNotConfigured, got %v", err)
	}
}
