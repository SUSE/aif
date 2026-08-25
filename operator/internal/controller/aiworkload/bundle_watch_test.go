package aiworkload

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func ctxTODO() context.Context { return context.TODO() }

func fakeClientWithWorkload(t *testing.T, name, ns, bundleName string) ctrlclient.Client {
	t.Helper()
	_ = aiplatformv1alpha1.AddToScheme(scheme.Scheme)
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{FleetBundleNames: []string{bundleName}},
	}
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(w).Build()
}

func TestBundleToAIWorkloads_ByBundleName(t *testing.T) {
	// A Bundle whose name matches a workload's FleetBundleNames maps to that workload.
	r := &AIWorkloadReconciler{Client: fakeClientWithWorkload(t, "wl-a", "default", "wl-a-open-webui")}
	b := &unstructured.Unstructured{Object: map[string]any{}}
	b.SetGroupVersionKind(bundleGVK)
	b.SetName("wl-a-open-webui")
	b.SetNamespace("fleet-local")
	reqs := r.bundleToAIWorkloads(ctxTODO(), ctrlclient.Object(b))
	if len(reqs) != 1 || reqs[0].Name != "wl-a" {
		t.Fatalf("want 1 request for wl-a, got %+v", reqs)
	}
}

func TestBundleToAIWorkloads_ByWorkloadUIDLabel(t *testing.T) {
	// A Bundle whose workload-uid label matches a workload's UID maps to that workload,
	// even if the bundle name does not match any FleetBundleNames (the primary label-based path).
	_ = aiplatformv1alpha1.AddToScheme(scheme.Scheme)
	testUID := "uid-xyz-12345"
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-b", Namespace: "default", UID: types.UID(testUID)},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{FleetBundleNames: []string{}},
	}
	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(w).Build()
	r := &AIWorkloadReconciler{Client: client}

	// Create a Bundle with workload-uid label pointing to wl-b, but a name that doesn't match any FleetBundleNames.
	b := &unstructured.Unstructured{Object: map[string]any{}}
	b.SetGroupVersionKind(bundleGVK)
	b.SetName("unrelated-bundle")
	b.SetNamespace("fleet-local")
	b.SetLabels(map[string]string{"ai-factory.suse.com/workload-uid": testUID})

	reqs := r.bundleToAIWorkloads(ctxTODO(), ctrlclient.Object(b))
	if len(reqs) != 1 || reqs[0].Name != "wl-b" {
		t.Fatalf("want 1 request for wl-b (label-based match), got %+v", reqs)
	}
}
