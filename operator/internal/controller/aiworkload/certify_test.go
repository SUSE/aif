package aiworkload

import (
	"context"
	"testing"
	"time"

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

// settledBD is a BundleDeployment of bundle ns/name on one cluster, carrying the
// render digest label Fleet copies from the Bundle, whose spec targets
// deploymentID and whose agent last applied applied in state.
func settledBD(ns, name, cluster, digest, deploymentID, applied, state string) *unstructured.Unstructured {
	bd := &unstructured.Unstructured{Object: map[string]any{}}
	bd.SetGroupVersionKind(bundleDeploymentGVK)
	bd.SetNamespace("cluster-" + ns + "-" + cluster)
	bd.SetName(name)
	bd.SetLabels(map[string]string{
		"fleet.cattle.io/bundle-name":      name,
		"fleet.cattle.io/bundle-namespace": ns,
		renderDigestLabel:                  renderDigestLabelValue(digest),
	})
	_ = unstructured.SetNestedField(bd.Object, deploymentID, "spec", "deploymentID")
	_ = unstructured.SetNestedField(bd.Object, deploymentID, "spec", "stagedDeploymentID")
	_ = unstructured.SetNestedField(bd.Object, applied, "status", "appliedDeploymentID")
	_ = unstructured.SetNestedField(bd.Object, state, "status", "display", "state")
	return bd
}

func TestBundleDeploymentsApplied(t *testing.T) {
	ready := *settledBD("fleet-local", "b", "a", "d", "s-new", "s-new", "Ready")
	modified := *settledBD("fleet-local", "b", "a", "d", "s-new", "s-new", "Modified")
	lagging := *settledBD("fleet-local", "b", "a", "d", "s-new", "s-old", "Ready")
	unpromoted := *settledBD("fleet-local", "b", "a", "d", "s-old", "s-old", "Ready")
	_ = unstructured.SetNestedField(unpromoted.Object, "s-new", "spec", "stagedDeploymentID")
	notReady := *settledBD("fleet-local", "b", "a", "d", "s-new", "s-new", "NotReady")
	// Fleet has not yet updated it: its IDs agree with each other, but it is the previous render's.
	previousRender := *settledBD("fleet-local", "b", "a", "old", "s-old", "s-old", "Ready")
	deleting := *settledBD("fleet-local", "b", "c", "d", "s-old", "s-old", "WaitApplied")
	now := metav1.Now()
	deleting.SetDeletionTimestamp(&now)
	for _, tc := range []struct {
		name          string
		bds           []unstructured.Unstructured
		clusters      int
		allowModified bool
		want          bool
	}{
		{"ready", []unstructured.Unstructured{ready}, 1, false, true},
		{"applied previous deployment", []unstructured.Unstructured{lagging}, 1, true, false},
		{"previous render, untouched by Fleet", []unstructured.Unstructured{previousRender}, 1, true, false},
		{"staged not promoted", []unstructured.Unstructured{unpromoted}, 1, true, false},
		{"missing BundleDeployment", nil, 1, true, false},
		{"fewer than clusters", []unstructured.Unstructured{ready}, 2, true, false},
		{"modified certifies nothing", []unstructured.Unstructured{modified}, 1, false, false},
		{"modified settles a dependency", []unstructured.Unstructured{modified}, 1, true, true},
		{"not ready", []unstructured.Unstructured{notReady}, 1, true, false},
		{"deleting BundleDeployment ignored", []unstructured.Unstructured{ready, deleting}, 1, false, true},
		{"zero clusters", nil, 0, false, true},
	} {
		if got := bundleDeploymentsApplied(tc.bds, "d", tc.clusters, tc.allowModified); got != tc.want {
			t.Errorf("%s: bundleDeploymentsApplied = %v, want %v", tc.name, got, tc.want)
		}
	}
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
	for _, gvk := range []schema.GroupVersionKind{bundleGVK, bundleDeploymentGVK, helmOpGVK, fleetClusterGVK} {
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
	b.SetLabels(map[string]string{renderDigestLabel: renderDigestLabelValue("sha256:d")})
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

	// Bundle summary alone: Fleet's first status write after a spec change
	// reports old deployments, so certification waits for the BundleDeployment.
	if err := r.certifyDeployedSource(ctx, w, keys, digests); err != nil {
		t.Fatalf("certifyDeployedSource failed with ready bundle: %v", err)
	}
	if w.Status.DeployedSource != nil {
		t.Fatalf("expected DeployedSource nil without an applied BundleDeployment, got %+v", w.Status.DeployedSource)
	}
	if err := cl.Create(ctx, settledBD("fleet-local", keys[0].Name, "local", "sha256:d", "s-1", "s-0", "Ready")); err != nil {
		t.Fatal(err)
	}
	if err := r.certifyDeployedSource(ctx, w, keys, digests); err != nil {
		t.Fatal(err)
	}
	if w.Status.DeployedSource != nil {
		t.Fatalf("expected DeployedSource nil while the BundleDeployment applied the previous deployment, got %+v", w.Status.DeployedSource)
	}
	putBundleDeployment(t, r, settledBD("fleet-local", keys[0].Name, "local", "sha256:d", "s-1", "s-1", "Ready"))
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

func TestCertifyDeployedSource_RefreshesCertifiedAtForActiveVersionOp(t *testing.T) {
	ctx := context.Background()
	keys := []HelmOpKey{{Namespace: "fleet-local", Name: blueprintBundleName("w", "open-webui"), ExpectedClusters: 1}}
	digests := map[string]string{"fleet-local/" + keys[0].Name: "sha256:d"}
	certifiedAt := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
	cases := []struct {
		name        string
		opType      string
		requestedAt time.Time
		wantRefresh bool
	}{
		{"no operation keeps CertifiedAt", "", time.Time{}, false},
		{"rollback requested after certification refreshes", aiplatformv1alpha1.OperationTypeRollback, time.Now().Add(-time.Minute), true},
		{"upgrade requested after certification refreshes", aiplatformv1alpha1.OperationTypeUpgrade, time.Now().Add(-time.Minute), true},
		{"rollback requested before certification keeps CertifiedAt", aiplatformv1alpha1.OperationTypeRollback, certifiedAt.Add(-time.Minute), false},
		{"retry keeps CertifiedAt", aiplatformv1alpha1.OperationTypeRetry, time.Now().Add(-time.Minute), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := wlWithBlueprint("1.0.0")
			if tc.opType != "" {
				inFlightVersionOp(w, tc.opType, "1.0.0", "", tc.requestedAt)
			}
			b := bundle(renderDigestLabelValue("sha256:d"), 1, 1, 0, 1, 1)
			b.SetName(keys[0].Name)
			b.SetNamespace("fleet-local")
			cl := fakeClient(w, b, settledBD("fleet-local", keys[0].Name, "local", "sha256:d", "s-1", "s-1", "Ready"))
			r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme()}
			if err := r.certifyDeployedSource(ctx, w, keys, digests); err != nil {
				t.Fatal(err)
			}
			w.Status.DeployedSource.CertifiedAt = certifiedAt

			if err := r.certifyDeployedSource(ctx, w, keys, digests); err != nil {
				t.Fatal(err)
			}
			refreshed := !w.Status.DeployedSource.CertifiedAt.Equal(&certifiedAt)
			if refreshed != tc.wantRefresh {
				t.Fatalf("CertifiedAt refreshed=%v, want %v", refreshed, tc.wantRefresh)
			}
			if refreshed && w.Status.DeployedSource.CertifiedAt.Time.Before(tc.requestedAt) {
				t.Fatalf("refreshed CertifiedAt %v must not predate the request %v", w.Status.DeployedSource.CertifiedAt, tc.requestedAt)
			}
		})
	}
}
