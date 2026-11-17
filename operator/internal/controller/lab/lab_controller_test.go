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

func ref(version string) *v1alpha1.LabCatalogRef {
	return &v1alpha1.LabCatalogRef{Catalog: "partner", Entry: "demo", Version: version}
}

func reconcileAgain(t *testing.T, c client.Client) v1alpha1.Lab {
	t.Helper()
	r := &LabReconciler{Client: c, Scheme: c.Scheme()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "l"}}); err != nil {
		t.Fatal(err)
	}
	var l v1alpha1.Lab
	if err := c.Get(context.Background(), client.ObjectKey{Name: "l"}, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func childVersion(t *testing.T, c client.Client) string {
	t.Helper()
	var child v1alpha1.InstallAIExtension
	if err := c.Get(context.Background(), client.ObjectKey{Name: "lab-l"}, &child); err != nil {
		t.Fatal(err)
	}
	return child.Spec.Source.Helm.Version
}

func bumpCatalog(t *testing.T, c client.Client, version, chartURL string) {
	t.Helper()
	var cat v1alpha1.LabCatalog
	if err := c.Get(context.Background(), client.ObjectKey{Name: "partner"}, &cat); err != nil {
		t.Fatal(err)
	}
	cat.Status.Entries[0].Source.Helm.Version = version
	cat.Status.Entries[0].Source.Helm.ChartURL = chartURL
	cat.Status.Entries[0].Extension.Version = version
	if err := c.Status().Update(context.Background(), &cat); err != nil {
		t.Fatal(err)
	}
}

func setRefVersion(t *testing.T, c client.Client, version string) {
	t.Helper()
	var l v1alpha1.Lab
	if err := c.Get(context.Background(), client.ObjectKey{Name: "l"}, &l); err != nil {
		t.Fatal(err)
	}
	l.Spec.CatalogRef.Version = version
	if err := c.Update(context.Background(), &l); err != nil {
		t.Fatal(err)
	}
}

// A catalog change never changes what runs until the admin accepts the version.
func TestCatalogUpdateNeedsAcceptance(t *testing.T) {
	c, l := run(t, catalog("1"), labWith(v1alpha1.LabSpec{CatalogRef: ref("1")}))
	if childVersion(t, c) != "1" || l.Status.UpdateAvailable != "" {
		t.Fatalf("status: %+v", l.Status)
	}

	bumpCatalog(t, c, "2", "oci://x/chart/aif-lab-demo")
	l = reconcileAgain(t, c)
	if childVersion(t, c) != "1" || l.Status.UpdateAvailable != "2" || l.Status.ResolvedVersion != "1" {
		t.Fatalf("not pinned: child=%s status=%+v", childVersion(t, c), l.Status)
	}

	setRefVersion(t, c, "2")
	l = reconcileAgain(t, c)
	if childVersion(t, c) != "2" || l.Status.UpdateAvailable != "" || l.Status.ResolvedVersion != "2" {
		t.Fatalf("not upgraded: child=%s status=%+v", childVersion(t, c), l.Status)
	}

	// same version, different chart: never applied silently
	bumpCatalog(t, c, "2", "oci://x/chart/aif-lab-other")
	l = reconcileAgain(t, c)
	var child v1alpha1.InstallAIExtension
	_ = c.Get(context.Background(), client.ObjectKey{Name: "lab-l"}, &child)
	if child.Spec.Source.Helm.ChartURL != "oci://x/chart/aif-lab-demo" || !strings.Contains(l.Status.Message, "without a new version") {
		t.Fatalf("chart swapped: %+v status=%+v", child.Spec.Source.Helm, l.Status)
	}
}

func TestAcceptedVersionNotOffered(t *testing.T) {
	c, l := run(t, catalog("2"), labWith(v1alpha1.LabSpec{CatalogRef: ref("1")}))
	if l.Status.Phase != v1alpha1.LabPhaseFailed || l.Status.UpdateAvailable != "2" {
		t.Fatalf("status: %+v", l.Status)
	}
	var list v1alpha1.InstallAIExtensionList
	if err := c.List(context.Background(), &list); err != nil || len(list.Items) != 0 {
		t.Fatalf("child created: %v", list.Items)
	}
}

func foreignChild() *v1alpha1.InstallAIExtension {
	return &v1alpha1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "lab-l"},
		Spec: v1alpha1.InstallAIExtensionSpec{Source: helm("oci://x/chart/aif-lab-mine", "9"),
			Extension: v1alpha1.ExtensionConfig{Name: "aif-lab-mine", Version: "9"}},
	}
}

func TestForeignChildNeverAdoptedOrDeleted(t *testing.T) {
	c, l := run(t, catalog("1"), foreignChild(), labWith(v1alpha1.LabSpec{CatalogRef: ref("")}))
	if l.Status.Phase != v1alpha1.LabPhaseFailed || !strings.Contains(l.Status.Message, "not owned") || childVersion(t, c) != "9" {
		t.Fatalf("status: %+v", l.Status)
	}

	disabled := labWith(v1alpha1.LabSpec{CatalogRef: ref("")})
	disabled.Spec.Enabled = false
	c, _ = run(t, catalog("1"), foreignChild(), disabled)
	if childVersion(t, c) != "9" {
		t.Fatal("foreign child deleted")
	}
}

func TestNameConflictWithOtherExtension(t *testing.T) {
	other := foreignChild()
	other.Name = "someone-else"
	other.Spec.Extension.Name = "aif-lab-demo" // same UIPlugin as the catalog entry
	c, l := run(t, catalog("1"), other, labWith(v1alpha1.LabSpec{CatalogRef: ref("")}))
	if l.Status.Phase != v1alpha1.LabPhaseFailed || !strings.Contains(l.Status.Message, "someone-else") {
		t.Fatalf("status: %+v", l.Status)
	}
	var child v1alpha1.InstallAIExtension
	if err := c.Get(context.Background(), client.ObjectKey{Name: "lab-l"}, &child); err == nil {
		t.Fatal("child created despite conflict")
	}
}
