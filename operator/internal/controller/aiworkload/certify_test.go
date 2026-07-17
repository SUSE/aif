package aiworkload

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func bundle(digest string, generation, observed, unavailable, ready, desired int64) *unstructured.Unstructured {
	b := &unstructured.Unstructured{Object: map[string]any{}}
	b.SetGroupVersionKind(bundleGVK)
	b.SetName("b")
	b.SetGeneration(generation)
	if digest != "" {
		b.SetLabels(map[string]string{renderDigestLabel: digest})
	}
	_ = unstructured.SetNestedField(b.Object, observed, "status", "observedGeneration")
	_ = unstructured.SetNestedField(b.Object, unavailable, "status", "unavailable")
	_ = unstructured.SetNestedField(b.Object, ready, "status", "summary", "ready")
	_ = unstructured.SetNestedField(b.Object, desired, "status", "summary", "desiredReady")
	return b
}

func TestBundleRenderCurrent(t *testing.T) {
	if !bundleRenderCurrent(bundle("d", 3, 3, 0, 1, 1), "d") {
		t.Fatal("should be render-current: digest matches, observed==generation")
	}
	if bundleRenderCurrent(bundle("old", 3, 3, 0, 1, 1), "d") {
		t.Fatal("stale digest label must not be render-current")
	}
	if bundleRenderCurrent(bundle("d", 3, 2, 0, 1, 1), "d") {
		t.Fatal("status.observedGeneration lagging generation must not be render-current")
	}
	if bundleRenderCurrent(nil, "d") {
		t.Fatal("nil bundle must not be render-current")
	}
}

func TestBundleCertified(t *testing.T) {
	if !bundleCertified(bundle("d", 3, 3, 0, 2, 2), "d", 2) {
		t.Fatal("fully rolled out + render-current + expected==desired should certify")
	}
	if bundleCertified(bundle("d", 3, 3, 1, 2, 2), "d", 2) {
		t.Fatal("unavailable>0 must not certify")
	}
	if bundleCertified(bundle("d", 3, 3, 0, 1, 2), "d", 2) {
		t.Fatal("ready<desired must not certify")
	}
	if bundleCertified(bundle("d", 3, 3, 0, 1, 1), "d", 2) {
		t.Fatal("desiredReady != expectedClusters must not certify (missing cluster)")
	}
	if bundleCertified(bundle("d", 3, 3, 0, 0, 0), "d", 0) {
		t.Fatal("expectedClusters==0 must never certify (vacuous)")
	}
}

func fakeClient(objs ...client.Object) client.Client {
	s := runtime.NewScheme()
	_ = aiplatformv1alpha1.AddToScheme(s)
	for _, gvk := range []schema.GroupVersionKind{bundleGVK, bundleDeploymentGVK, helmOpGVK} {
		s.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		lg := gvk
		lg.Kind += "List"
		s.AddKnownTypeWithName(lg, &unstructured.UnstructuredList{})
	}
	return fake.NewClientBuilder().WithScheme(s).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).
		WithObjects(objs...).Build()
}

func TestCertifyDeployedSource(t *testing.T) {
	ctx := context.Background()

	// Create a workload
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "cert-wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName:     "cert-wl",
			TargetNamespace: "ai",
			DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
			TargetClusters:  []string{"local"},
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: "1.0.0"},
			},
		},
	}

	cl := fakeClient(w)
	r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme()}

	keys := []HelmOpKey{{Namespace: "fleet-local", Name: blueprintBundleName("cert-wl", "open-webui"), ExpectedClusters: 1}}
	digests := map[string]string{"fleet-local/" + keys[0].Name: "sha256:d"}

	// Test 1: No Bundle exists → not certified, deployedSource should be nil
	if err := r.certifyDeployedSource(ctx, w, keys, digests); err != nil {
		t.Fatalf("certifyDeployedSource failed with no bundle: %v", err)
	}
	if w.Status.DeployedSource != nil {
		t.Errorf("expected DeployedSource to be nil when no bundle exists, got %+v", w.Status.DeployedSource)
	}

	// Test 2: Create a NOT-yet-rolled-out Bundle → still not certified
	b := &unstructured.Unstructured{Object: map[string]any{}}
	b.SetGroupVersionKind(bundleGVK)
	b.SetName(keys[0].Name)
	b.SetNamespace("fleet-local")
	b.SetLabels(map[string]string{renderDigestLabel: "sha256:d"})
	b.SetGeneration(1)

	// Set status fields to simulate a NOT fully rolled out bundle
	_ = unstructured.SetNestedField(b.Object, int64(1), "status", "observedGeneration")
	_ = unstructured.SetNestedField(b.Object, int64(1), "status", "unavailable")
	_ = unstructured.SetNestedField(b.Object, int64(0), "status", "summary", "ready")
	_ = unstructured.SetNestedField(b.Object, int64(1), "status", "summary", "desiredReady")

	// Create the bundle with status already set
	if err := cl.Create(ctx, b); err != nil {
		t.Fatalf("failed to create bundle: %v", err)
	}

	if err := r.certifyDeployedSource(ctx, w, keys, digests); err != nil {
		t.Fatalf("certifyDeployedSource failed with not-ready bundle: %v", err)
	}
	if w.Status.DeployedSource != nil {
		t.Errorf("expected DeployedSource to be nil when bundle not ready, got %+v", w.Status.DeployedSource)
	}

	// Test 3: Now fully rolled out → certified
	// Fetch the bundle and update it to be fully rolled out
	fetched := &unstructured.Unstructured{}
	fetched.SetGroupVersionKind(bundleGVK)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(b), fetched); err != nil {
		t.Fatalf("failed to fetch bundle again: %v", err)
	}

	gen := fetched.GetGeneration()
	_ = unstructured.SetNestedField(fetched.Object, gen, "status", "observedGeneration")
	_ = unstructured.SetNestedField(fetched.Object, int64(0), "status", "unavailable")
	_ = unstructured.SetNestedField(fetched.Object, int64(1), "status", "summary", "ready")
	_ = unstructured.SetNestedField(fetched.Object, int64(1), "status", "summary", "desiredReady")

	if err := cl.Update(ctx, fetched); err != nil {
		t.Fatalf("failed to update bundle to ready: %v", err)
	}

	if err := r.certifyDeployedSource(ctx, w, keys, digests); err != nil {
		t.Fatalf("certifyDeployedSource failed with ready bundle: %v", err)
	}
	if w.Status.DeployedSource == nil {
		t.Fatal("expected DeployedSource to be set when bundle ready, got nil")
	}
	if w.Status.DeployedSource.Version != "1.0.0" {
		t.Errorf("expected Version 1.0.0, got %s", w.Status.DeployedSource.Version)
	}

	firstCertifiedAt := w.Status.DeployedSource.CertifiedAt

	// Test 4: Idempotent: unchanged inputs do not rewrite certifiedAt
	if err := r.certifyDeployedSource(ctx, w, keys, digests); err != nil {
		t.Fatalf("certifyDeployedSource failed on second call: %v", err)
	}
	if !w.Status.DeployedSource.CertifiedAt.Equal(&firstCertifiedAt) {
		t.Errorf("expected CertifiedAt to be unchanged (idempotent), was %v, now %v", firstCertifiedAt, w.Status.DeployedSource.CertifiedAt)
	}
}
