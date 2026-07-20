package aiworkload

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func bd(deploymentID, applied, state string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{}}
	o.SetGroupVersionKind(bundleDeploymentGVK)
	_ = unstructured.SetNestedField(o.Object, deploymentID, "spec", "deploymentID")
	_ = unstructured.SetNestedField(o.Object, applied, "status", "appliedDeploymentID")
	_ = unstructured.SetNestedField(o.Object, state, "status", "display", "state")
	return o
}

func TestMatrixCellPhase(t *testing.T) {
	R := aiplatformv1alpha1.AIWorkloadClusterPhaseRunning
	F := aiplatformv1alpha1.AIWorkloadClusterPhaseFailed
	P := aiplatformv1alpha1.AIWorkloadClusterPhasePending

	// Render-gated: not current → Pending regardless of ready state.
	if got := matrixCellPhase(bd("s1", "s1", "Ready"), false); got != P {
		t.Fatalf("bundle not render-current must be Pending, got %s", got)
	}
	// Stale ErrApplied (applied != desired) → Pending, NOT Failed.
	if got := matrixCellPhase(bd("s2", "s1", "ErrApplied"), true); got != P {
		t.Fatalf("stale deploymentID must be Pending, got %s", got)
	}
	// Current + Ready / Modified → Running.
	if got := matrixCellPhase(bd("s1", "s1", "Ready"), true); got != R {
		t.Fatalf("Ready+current must be Running, got %s", got)
	}
	if got := matrixCellPhase(bd("s1", "s1", "Modified"), true); got != R {
		t.Fatalf("Modified+current must be Running, got %s", got)
	}
	// Current + ErrApplied → Failed.
	if got := matrixCellPhase(bd("s1", "s1", "ErrApplied"), true); got != F {
		t.Fatalf("ErrApplied+current must be Failed, got %s", got)
	}
	// Current + transient → Pending.
	if got := matrixCellPhase(bd("s1", "s1", "WaitApplied"), true); got != P {
		t.Fatalf("transient must be Pending, got %s", got)
	}
}

func TestBuildComponentMatrix(t *testing.T) {
	ctx := context.Background()

	// Test: a stale ErrApplied BundleDeployment (parent Bundle carries an OLD render-digest,
	// not render-current) yields a Pending matrix cell (not Failed).
	name := blueprintBundleName("mx", "c")
	keys := []HelmOpKey{{Namespace: "fleet-local", Name: name, ExpectedClusters: 1}}
	digests := map[string]string{"fleet-local/" + name: "sha256:new"}

	// Bundle carries an OLD digest (render not current).
	b := &unstructured.Unstructured{Object: map[string]any{}}
	b.SetGroupVersionKind(bundleGVK)
	b.SetName(name)
	b.SetNamespace("fleet-local")
	b.SetLabels(map[string]string{renderDigestLabel: renderDigestLabelValue("sha256:old")})

	// A BundleDeployment reporting ErrApplied on the old content.
	bdObj := &unstructured.Unstructured{Object: map[string]any{}}
	bdObj.SetGroupVersionKind(bundleDeploymentGVK)
	bdObj.SetName(name + "-local")
	bdObj.SetNamespace("fleet-local")
	bdObj.SetLabels(map[string]string{"fleet.cattle.io/bundle-name": name, "fleet.cattle.io/bundle-namespace": "fleet-local", "fleet.cattle.io/cluster": "local"})
	_ = unstructured.SetNestedField(bdObj.Object, "s1", "spec", "deploymentID")

	cl := fakeClient(b, bdObj)
	// The fake client sets generation=1 on create by default.
	// Fetch the bundle and set status.observedGeneration consistently.
	fetched := &unstructured.Unstructured{}
	fetched.SetGroupVersionKind(bundleGVK)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(b), fetched); err != nil {
		t.Fatalf("failed to fetch bundle: %v", err)
	}
	gen := fetched.GetGeneration()
	_ = unstructured.SetNestedField(fetched.Object, gen, "status", "observedGeneration")
	if err := cl.Update(ctx, fetched); err != nil {
		t.Fatalf("failed to update bundle status: %v", err)
	}

	// Fetch the BundleDeployment and set its status to ErrApplied.
	fetchedBD := &unstructured.Unstructured{}
	fetchedBD.SetGroupVersionKind(bundleDeploymentGVK)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(bdObj), fetchedBD); err != nil {
		t.Fatalf("failed to fetch BundleDeployment: %v", err)
	}
	_ = unstructured.SetNestedField(fetchedBD.Object, "s1", "status", "appliedDeploymentID")
	_ = unstructured.SetNestedField(fetchedBD.Object, "ErrApplied", "status", "display", "state")
	if err := cl.Update(ctx, fetchedBD); err != nil {
		t.Fatalf("failed to update BundleDeployment status: %v", err)
	}

	r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme()}
	w := &aiplatformv1alpha1.AIWorkload{}

	cells, err := r.buildComponentMatrix(ctx, w, keys, digests)
	if err != nil {
		t.Fatalf("buildComponentMatrix failed: %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}
	if cells[0].Phase != aiplatformv1alpha1.AIWorkloadClusterPhasePending {
		t.Errorf("expected phase Pending (stale ErrApplied must not fail), got %v", cells[0].Phase)
	}
}

func TestBuildComponentMatrix_MissingBDIsPending(t *testing.T) {
	ctx := context.Background()

	// Test: a key whose expected cluster has NO BundleDeployment yields a Pending cell for that cluster.
	name := blueprintBundleName("missing-bd", "chart-a")
	keys := []HelmOpKey{{Namespace: "fleet-default", Name: name, ExpectedClusters: 2}}
	digests := map[string]string{"fleet-default/" + name: "sha256:current"}

	// Bundle with current digest.
	b := &unstructured.Unstructured{Object: map[string]any{}}
	b.SetGroupVersionKind(bundleGVK)
	b.SetName(name)
	b.SetNamespace("fleet-default")
	b.SetLabels(map[string]string{renderDigestLabel: renderDigestLabelValue("sha256:current")})

	// Create a workload targeting two downstream clusters: "c-alpha" and "c-beta".
	w := &aiplatformv1alpha1.AIWorkload{
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetClusters: []string{"c-alpha", "c-beta"},
		},
	}

	// Only create a BundleDeployment for "c-alpha"; "c-beta" has none (not yet deployed).
	bdObj := &unstructured.Unstructured{Object: map[string]any{}}
	bdObj.SetGroupVersionKind(bundleDeploymentGVK)
	bdObj.SetName(name + "-c-alpha")
	bdObj.SetNamespace("fleet-default")
	bdObj.SetLabels(map[string]string{"fleet.cattle.io/bundle-name": name, "fleet.cattle.io/bundle-namespace": "fleet-default", "fleet.cattle.io/cluster": "c-alpha"})
	_ = unstructured.SetNestedField(bdObj.Object, "s1", "spec", "deploymentID")

	cl := fakeClient(b, bdObj, w)

	// Set bundle status.observedGeneration.
	fetched := &unstructured.Unstructured{}
	fetched.SetGroupVersionKind(bundleGVK)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(b), fetched); err != nil {
		t.Fatalf("failed to fetch bundle: %v", err)
	}
	gen := fetched.GetGeneration()
	_ = unstructured.SetNestedField(fetched.Object, gen, "status", "observedGeneration")
	if err := cl.Update(ctx, fetched); err != nil {
		t.Fatalf("failed to update bundle status: %v", err)
	}

	// Set BundleDeployment status to Ready.
	fetchedBD := &unstructured.Unstructured{}
	fetchedBD.SetGroupVersionKind(bundleDeploymentGVK)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(bdObj), fetchedBD); err != nil {
		t.Fatalf("failed to fetch BundleDeployment: %v", err)
	}
	_ = unstructured.SetNestedField(fetchedBD.Object, "s1", "status", "appliedDeploymentID")
	_ = unstructured.SetNestedField(fetchedBD.Object, "Ready", "status", "display", "state")
	if err := cl.Update(ctx, fetchedBD); err != nil {
		t.Fatalf("failed to update BundleDeployment status: %v", err)
	}

	r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme()}

	cells, err := r.buildComponentMatrix(ctx, w, keys, digests)
	if err != nil {
		t.Fatalf("buildComponentMatrix failed: %v", err)
	}

	// Expect 2 cells: one for c-alpha (from existing BD), one for c-beta (missing → Pending).
	if len(cells) != 2 {
		t.Fatalf("expected 2 cells (c-alpha + c-beta), got %d", len(cells))
	}

	// Verify that both clusters are present and sorted.
	if cells[0].ClusterID != "c-alpha" || cells[1].ClusterID != "c-beta" {
		t.Errorf("expected cells for c-alpha and c-beta in order, got %v and %v", cells[0].ClusterID, cells[1].ClusterID)
	}

	// c-alpha has a BD → should be Running (Ready + current).
	if cells[0].Phase != aiplatformv1alpha1.AIWorkloadClusterPhaseRunning {
		t.Errorf("c-alpha (existing BD) should be Running, got %v", cells[0].Phase)
	}

	// c-beta has NO BD → should be Pending.
	if cells[1].Phase != aiplatformv1alpha1.AIWorkloadClusterPhasePending {
		t.Errorf("c-beta (missing BD) should be Pending, got %v", cells[1].Phase)
	}
}
