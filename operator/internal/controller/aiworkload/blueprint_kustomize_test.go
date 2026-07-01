package aiworkload

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// TestEnsureBlueprintKustomize verifies that a Blueprint with a Kustomize
// component produces a GitRepo with the expected spec.repo, spec.paths,
// spec.revision, and spec.targets.
func TestEnsureBlueprintKustomize(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "kustomize-app"

	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	// Register GitRepo as Unstructured so the fake client can store it.
	scheme.AddKnownTypeWithName(gitRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepoList",
	}, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName:     "wl",
			TargetNamespace: targetNS,
			TargetClusters:  []string{"local"},
			DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
		},
	}
	comp := aiplatformv1alpha1.BlueprintComponent{
		Name: "podinfo",
		Type: aiplatformv1alpha1.ComponentContentTypeKustomize,
		Kustomize: &aiplatformv1alpha1.KustomizeSource{
			Repo:     "https://github.com/stefanprodan/podinfo",
			Path:     "kustomize",
			Revision: "main",
		},
	}

	if err := r.ensureBlueprintKustomize(context.Background(), w, comp, "wl-podinfo"); err != nil {
		t.Fatalf("ensureBlueprintKustomize: %v", err)
	}

	// local-only TargetClusters → GitRepo lands in fleet-local.
	var gr unstructured.Unstructured
	gr.SetGroupVersionKind(gitRepoGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "wl-podinfo"}, &gr); err != nil {
		t.Fatalf("GitRepo not found in fleet-local: %v", err)
	}

	// Verify spec.repo
	repo, _, _ := unstructured.NestedString(gr.Object, "spec", "repo")
	if repo != "https://github.com/stefanprodan/podinfo" {
		t.Errorf("spec.repo: got %q want %q", repo, "https://github.com/stefanprodan/podinfo")
	}

	// Verify spec.paths
	paths, _, _ := unstructured.NestedStringSlice(gr.Object, "spec", "paths")
	if len(paths) != 1 || paths[0] != "kustomize" {
		t.Errorf("spec.paths: got %v want [kustomize]", paths)
	}

	// Verify spec.revision
	revision, _, _ := unstructured.NestedString(gr.Object, "spec", "revision")
	if revision != "main" {
		t.Errorf("spec.revision: got %q want %q", revision, "main")
	}

	// Verify spec.targetNamespace
	ns, _, _ := unstructured.NestedString(gr.Object, "spec", "targetNamespace")
	if ns != targetNS {
		t.Errorf("spec.targetNamespace: got %q want %q", ns, targetNS)
	}

	// Verify spec.targets has the local cluster target
	targets, _, _ := unstructured.NestedSlice(gr.Object, "spec", "targets")
	if len(targets) != 1 {
		t.Fatalf("spec.targets: expected 1 target, got %d", len(targets))
	}
	if target, ok := targets[0].(map[string]any); ok {
		if clusterName, _ := target["clusterName"].(string); clusterName != "local" {
			t.Errorf("spec.targets[0].clusterName: got %q want %q", clusterName, "local")
		}
	} else {
		t.Errorf("spec.targets[0] is not a map")
	}
}

// TestEnsureBlueprintKustomize_WithDownstreamCluster verifies that a Kustomize
// component targeting a downstream cluster produces a GitRepo in fleet-default
// with the correct clusterSelector.
func TestEnsureBlueprintKustomize_WithDownstreamCluster(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "kustomize-app"

	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	scheme.AddKnownTypeWithName(gitRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepoList",
	}, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName:     "wl",
			TargetNamespace: targetNS,
			TargetClusters:  []string{"cluster-1"},
			DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
		},
	}
	comp := aiplatformv1alpha1.BlueprintComponent{
		Name: "app",
		Type: aiplatformv1alpha1.ComponentContentTypeKustomize,
		Kustomize: &aiplatformv1alpha1.KustomizeSource{
			Repo: "https://github.com/example/app",
			Path: "deploy/overlays/prod",
		},
	}

	if err := r.ensureBlueprintKustomize(context.Background(), w, comp, "wl-app"); err != nil {
		t.Fatalf("ensureBlueprintKustomize: %v", err)
	}

	// Downstream cluster → GitRepo lands in fleet-default.
	var gr unstructured.Unstructured
	gr.SetGroupVersionKind(gitRepoGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-default", Name: "wl-app"}, &gr); err != nil {
		t.Fatalf("GitRepo not found in fleet-default: %v", err)
	}

	// Verify spec.repo
	repo, _, _ := unstructured.NestedString(gr.Object, "spec", "repo")
	if repo != "https://github.com/example/app" {
		t.Errorf("spec.repo: got %q want %q", repo, "https://github.com/example/app")
	}

	// Verify spec.paths
	paths, _, _ := unstructured.NestedStringSlice(gr.Object, "spec", "paths")
	if len(paths) != 1 || paths[0] != "deploy/overlays/prod" {
		t.Errorf("spec.paths: got %v want [deploy/overlays/prod]", paths)
	}

	// Verify spec.revision is NOT set (optional field)
	revision, found, _ := unstructured.NestedString(gr.Object, "spec", "revision")
	if found && revision != "" {
		t.Errorf("spec.revision: expected empty/unset, got %q", revision)
	}

	// Verify spec.targets has the clusterSelector
	targets, _, _ := unstructured.NestedSlice(gr.Object, "spec", "targets")
	if len(targets) != 1 {
		t.Fatalf("spec.targets: expected 1 target, got %d", len(targets))
	}
	if target, ok := targets[0].(map[string]any); ok {
		if selector, ok := target["clusterSelector"].(map[string]any); ok {
			if labels, ok := selector["matchLabels"].(map[string]any); ok {
				if clusterName, ok := labels["management.cattle.io/cluster-name"].(string); !ok || clusterName != "cluster-1" {
					t.Errorf("matchLabels[management.cattle.io/cluster-name]: got %q want %q", clusterName, "cluster-1")
				}
			} else {
				t.Error("clusterSelector.matchLabels is not a map")
			}
		} else {
			t.Error("target[clusterSelector] is not a map")
		}
	} else {
		t.Error("spec.targets[0] is not a map")
	}
}

// TestHelmAndKustomizeCoexist verifies that a Blueprint can have both Helm and
// Kustomize components, and each produces the correct resource type (HelmOp for
// Helm, GitRepo for Kustomize). This is the regression test ensuring the new
// dispatch logic doesn't break existing Helm behavior.
func TestHelmAndKustomizeCoexist(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "mixed-app"

	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	// Register both HelmOp and GitRepo as Unstructured.
	scheme.AddKnownTypeWithName(helmOpGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "HelmOpList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(gitRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepoList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(clusterRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})

	// ClusterRepo for the Helm component.
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("suse-ai")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName:     "wl",
			TargetNamespace: targetNS,
			TargetClusters:  []string{"local"},
			DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
		},
	}

	// Helm component
	helmComp := aiplatformv1alpha1.BlueprintComponent{
		Name: "helm-app",
		Type: aiplatformv1alpha1.ComponentContentTypeHelm,
		Helm: &aiplatformv1alpha1.BlueprintHelmSource{
			ChartRepo:    "suse-ai",
			ChartName:    "my-chart",
			ChartVersion: "1.0.0",
		},
	}

	// Kustomize component
	kustomizeComp := aiplatformv1alpha1.BlueprintComponent{
		Name: "kustomize-app",
		Type: aiplatformv1alpha1.ComponentContentTypeKustomize,
		Kustomize: &aiplatformv1alpha1.KustomizeSource{
			Repo:     "https://github.com/example/app",
			Path:     "deploy/overlays/prod",
			Revision: "v1.0.0",
		},
	}

	// Deploy Helm component
	if err := r.ensureBlueprintHelmOp(context.Background(), w, helmComp, "wl-helm"); err != nil {
		t.Fatalf("ensureBlueprintHelmOp: %v", err)
	}

	// Deploy Kustomize component
	if err := r.ensureBlueprintKustomize(context.Background(), w, kustomizeComp, "wl-kustomize"); err != nil {
		t.Fatalf("ensureBlueprintKustomize: %v", err)
	}

	// Verify HelmOp exists for Helm component
	var ho unstructured.Unstructured
	ho.SetGroupVersionKind(helmOpGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "wl-helm"}, &ho); err != nil {
		t.Fatalf("HelmOp not found in fleet-local: %v", err)
	}
	chartName, _, _ := unstructured.NestedString(ho.Object, "spec", "helm", "chart")
	if chartName != "my-chart" {
		t.Errorf("HelmOp spec.helm.chart: got %q want %q", chartName, "my-chart")
	}

	// Verify GitRepo exists for Kustomize component
	var gr unstructured.Unstructured
	gr.SetGroupVersionKind(gitRepoGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "wl-kustomize"}, &gr); err != nil {
		t.Fatalf("GitRepo not found in fleet-local: %v", err)
	}
	repo2, _, _ := unstructured.NestedString(gr.Object, "spec", "repo")
	if repo2 != "https://github.com/example/app" {
		t.Errorf("GitRepo spec.repo: got %q want %q", repo2, "https://github.com/example/app")
	}
}

// TestGitRepoDeletedOnTeardown verifies that when an AIWorkload with a Kustomize
// component is deleted, the GitRepo is removed during finalizer cleanup.
func TestGitRepoDeletedOnTeardown(t *testing.T) {
	const opNS = "suse-ai-operator"

	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	scheme.AddKnownTypeWithName(gitRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepoList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(helmOpGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "HelmOpList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList",
	}, &unstructured.UnstructuredList{})

	// Pre-create a GitRepo that the cleanup must delete.
	gr := &unstructured.Unstructured{}
	gr.SetGroupVersionKind(gitRepoGVK)
	gr.SetName("wl-podinfo")
	gr.SetNamespace("fleet-local")
	_ = unstructured.SetNestedField(gr.Object, "https://github.com/stefanprodan/podinfo", "spec", "repo")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gr).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	ctx := context.Background()

	// Verify GitRepo exists before deletion
	var grBefore unstructured.Unstructured
	grBefore.SetGroupVersionKind(gitRepoGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "fleet-local", Name: "wl-podinfo"}, &grBefore); err != nil {
		t.Fatalf("GitRepo should exist before deletion: %v", err)
	}

	// Call deleteGitRepo
	if err := r.deleteGitRepo(ctx, "wl-podinfo"); err != nil {
		t.Fatalf("deleteGitRepo: %v", err)
	}

	// Verify GitRepo is deleted
	var grAfter unstructured.Unstructured
	grAfter.SetGroupVersionKind(gitRepoGVK)
	err := c.Get(ctx, types.NamespacedName{Namespace: "fleet-local", Name: "wl-podinfo"}, &grAfter)
	if err == nil {
		t.Error("GitRepo should be deleted but still exists")
	}
	// NotFound is expected; other errors are real failures
	if err != nil && !errors.IsNotFound(err) {
		t.Errorf("unexpected error after deletion: %v", err)
	}
}

// TestGitRepoStatusAggregation verifies that BundleDeployments labeled with
// fleet.cattle.io/repo-name (GitRepo-generated) are aggregated into workload status.
func TestGitRepoStatusAggregation(t *testing.T) {
	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeployment",
	}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeploymentList",
	}, &unstructured.UnstructuredList{})

	// Create a GitRepo-generated BundleDeployment (labeled with repo-name).
	bd := &unstructured.Unstructured{}
	bd.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeployment",
	})
	bd.SetName("wl-podinfo-cluster1")
	bd.SetNamespace("fleet-default")
	bd.SetLabels(map[string]string{
		"fleet.cattle.io/repo-name": "wl-podinfo",
		"fleet.cattle.io/cluster":   "cluster1",
	})
	_ = unstructured.SetNestedField(bd.Object, "Ready", "status", "display", "state")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bd).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			FleetBundleNames: []string{"wl-podinfo"},
		},
	}

	if err := r.mirrorBlueprintStatus(context.Background(), w); err != nil {
		t.Fatalf("mirrorBlueprintStatus: %v", err)
	}

	// Verify cluster status was aggregated.
	if len(w.Status.ClusterStatuses) != 1 {
		t.Fatalf("expected 1 cluster status, got %d", len(w.Status.ClusterStatuses))
	}
	cs := w.Status.ClusterStatuses[0]
	if cs.ClusterID != "cluster1" {
		t.Errorf("ClusterID: got %q want %q", cs.ClusterID, "cluster1")
	}
	if cs.Phase != aiplatformv1alpha1.AIWorkloadClusterPhaseRunning {
		t.Errorf("Phase: got %q want %q", cs.Phase, aiplatformv1alpha1.AIWorkloadClusterPhaseRunning)
	}
}
