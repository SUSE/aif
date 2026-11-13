package labcatalog

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func newR(t *testing.T, fetch func(context.Context, string) ([]byte, error), objs ...client.Object) (*LabCatalogReconciler, client.Client) {
	t.Helper()
	sch := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(sch).WithStatusSubresource(&v1alpha1.LabCatalog{}).WithObjects(objs...).Build()
	return &LabCatalogReconciler{Client: c, Scheme: sch, Fetch: fetch}, c
}

func reconcile(t *testing.T, r *LabCatalogReconciler, name string) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: name}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func get(t *testing.T, c client.Client, name string) v1alpha1.LabCatalog {
	t.Helper()
	var cat v1alpha1.LabCatalog
	if err := c.Get(context.Background(), client.ObjectKey{Name: name}, &cat); err != nil {
		t.Fatal(err)
	}
	return cat
}

func TestBuiltinRecreatedAndFilled(t *testing.T) {
	r, c := newR(t, nil)
	reconcile(t, r, "builtin") // not found -> created
	reconcile(t, r, "builtin")
	cat := get(t, c, "builtin")
	if cat.Status.Phase != v1alpha1.LabCatalogPhaseReady || len(cat.Status.Entries) == 0 {
		t.Fatalf("status: %+v", cat.Status)
	}
}

const doc = `[
 {"name":"demo","source":{"kind":"Helm","helm":{"chartURL":"oci://x/chart/aif-lab-demo","version":"1","auth":{}}},"extension":{"name":"aif-lab-demo","version":"1"}},
 {"name":"hijack","source":{"kind":"Helm","helm":{"chartURL":"oci://x/chart/aif-ui","version":"1"}},"extension":{"name":"aif-ui","version":"1"}}
]`

func TestExternalFetchKeepsEntriesOnError(t *testing.T) {
	cat := &v1alpha1.LabCatalog{ObjectMeta: metav1.ObjectMeta{Name: "partner"}, Spec: v1alpha1.LabCatalogSpec{URL: "https://x/c.json"}}
	fail := false
	r, c := newR(t, func(context.Context, string) ([]byte, error) {
		if fail {
			return nil, errors.New("boom")
		}
		return []byte(doc), nil
	}, cat)

	if res := reconcile(t, r, "partner"); res.RequeueAfter != RefreshInterval {
		t.Errorf("requeue=%v", res.RequeueAfter)
	}
	got := get(t, c, "partner")
	if got.Status.Phase != v1alpha1.LabCatalogPhaseReady || len(got.Status.Entries) != 1 || got.Status.Skipped != 1 {
		t.Fatalf("status: %+v", got.Status)
	}
	if got.Status.Message == "" {
		t.Error("want message with skip reason")
	}

	fail = true
	reconcile(t, r, "partner")
	got = get(t, c, "partner")
	if got.Status.Phase != v1alpha1.LabCatalogPhaseError || len(got.Status.Entries) != 1 {
		t.Fatalf("entries must be kept on error: %+v", got.Status)
	}
}
