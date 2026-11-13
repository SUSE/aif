package lab

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func helm(url, ver string) v1alpha1.ExtensionSource {
	return v1alpha1.ExtensionSource{Kind: v1alpha1.ExtensionSourceKindHelm, Helm: &v1alpha1.HelmSource{ChartURL: url, Version: ver}}
}

func run(t *testing.T, objs ...client.Object) (client.Client, v1alpha1.Lab) {
	t.Helper()
	sch := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(sch).
		WithStatusSubresource(&v1alpha1.Lab{}, &v1alpha1.LabCatalog{}).WithObjects(objs...).Build()
	r := &LabReconciler{Client: c, Scheme: sch}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "l"}}); err != nil {
		t.Fatal(err)
	}
	var l v1alpha1.Lab
	if err := c.Get(context.Background(), client.ObjectKey{Name: "l"}, &l); err != nil {
		t.Fatal(err)
	}
	return c, l
}

func catalog(version string) *v1alpha1.LabCatalog {
	return &v1alpha1.LabCatalog{
		ObjectMeta: metav1.ObjectMeta{Name: "partner"},
		Status: v1alpha1.LabCatalogStatus{Entries: []v1alpha1.LabCatalogEntry{{
			Name: "demo", Source: helm("oci://x/chart/aif-lab-demo", version),
			Extension: v1alpha1.ExtensionConfig{Name: "aif-lab-demo", Version: version},
		}}},
	}
}

func labWith(spec v1alpha1.LabSpec) *v1alpha1.Lab {
	spec.Enabled = true
	return &v1alpha1.Lab{ObjectMeta: metav1.ObjectMeta{Name: "l"}, Spec: spec}
}

func TestCatalogRefResolved(t *testing.T) {
	c, l := run(t, catalog("1.2.0"), labWith(v1alpha1.LabSpec{CatalogRef: &v1alpha1.LabCatalogRef{Catalog: "partner", Entry: "demo"}}))
	if l.Status.ResolvedChartURL != "oci://x/chart/aif-lab-demo" || l.Status.ResolvedVersion != "1.2.0" {
		t.Fatalf("status: %+v", l.Status)
	}
	var child v1alpha1.InstallAIExtension
	if err := c.Get(context.Background(), client.ObjectKey{Name: "lab-l"}, &child); err != nil {
		t.Fatal(err)
	}
	if child.Spec.Source.Helm.Version != "1.2.0" || child.Spec.Extension.Name != "aif-lab-demo" {
		t.Fatalf("child: %+v", child.Spec)
	}
}

func TestCatalogRefMissing(t *testing.T) {
	_, l := run(t, labWith(v1alpha1.LabSpec{CatalogRef: &v1alpha1.LabCatalogRef{Catalog: "nope", Entry: "demo"}}))
	if l.Status.Phase != v1alpha1.LabPhaseFailed || !strings.Contains(l.Status.Message, "not found") {
		t.Fatalf("status: %+v", l.Status)
	}
	_, l = run(t, catalog("1"), labWith(v1alpha1.LabSpec{CatalogRef: &v1alpha1.LabCatalogRef{Catalog: "partner", Entry: "zzz"}}))
	if l.Status.Phase != v1alpha1.LabPhaseFailed || !strings.Contains(l.Status.Message, "zzz") {
		t.Fatalf("status: %+v", l.Status)
	}
}

func TestInlineAndNameValidation(t *testing.T) {
	src := helm("oci://x/chart/aif-lab-demo", "1")
	_, l := run(t, labWith(v1alpha1.LabSpec{Source: &src, Extension: &v1alpha1.ExtensionConfig{Name: "aif-lab-demo", Version: "1"}}))
	if l.Status.Phase != v1alpha1.LabPhasePending || l.Status.ResolvedVersion != "1" {
		t.Fatalf("status: %+v", l.Status)
	}
	bad := helm("oci://x/chart/aif-ui", "1")
	_, l = run(t, labWith(v1alpha1.LabSpec{Source: &bad, Extension: &v1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1"}}))
	if l.Status.Phase != v1alpha1.LabPhaseFailed {
		t.Fatalf("status: %+v", l.Status)
	}
	// a catalog entry that slipped through must still fail ValidateNames
	cat := catalog("1")
	cat.Status.Entries[0].Extension.Name = "aif-ui"
	_, l = run(t, cat, labWith(v1alpha1.LabSpec{CatalogRef: &v1alpha1.LabCatalogRef{Catalog: "partner", Entry: "demo"}}))
	if l.Status.Phase != v1alpha1.LabPhaseFailed {
		t.Fatalf("status: %+v", l.Status)
	}
}
