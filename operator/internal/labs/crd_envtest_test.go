package labs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// TestCRDsInstallAndValidate installs every chart CRD into a real API server
// (envtest) and checks the Lab/LabCatalog CEL rules. It catches CRDs the API
// server rejects, e.g. a CEL rule over budget, which makes the chart's CRD hook
// Job fail. Skipped when the envtest binaries are not installed
// (make setup-envtest, or set KUBEBUILDER_ASSETS).
func TestCRDsInstallAndValidate(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		dirs, _ := filepath.Glob(filepath.Join("..", "..", "bin", "k8s", "*"))
		if len(dirs) == 0 {
			t.Skip("envtest binaries not found; run make setup-envtest")
		}
		assets = dirs[0]
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "charts", "aif-operator", "crds")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: assets,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start / CRD install: %v", err)
	}
	defer func() { _ = env.Stop() }()
	s := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(s)
	c, err := client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	src := &v1alpha1.ExtensionSource{Kind: v1alpha1.ExtensionSourceKindHelm, Helm: &v1alpha1.HelmSource{ChartURL: "oci://r/chart/aif-lab-x", Version: "1"}}
	ext := &v1alpha1.ExtensionConfig{Name: "aif-lab-x", Version: "1"}
	cases := []struct {
		name string
		obj  client.Object
		ok   bool
	}{
		{"builtin catalog, empty url", &v1alpha1.LabCatalog{ObjectMeta: metav1.ObjectMeta{Name: "builtin"}}, true},
		{"external catalog, https", &v1alpha1.LabCatalog{ObjectMeta: metav1.ObjectMeta{Name: "partner"}, Spec: v1alpha1.LabCatalogSpec{URL: "https://example.com/c.json"}}, true},
		{"external catalog, empty url", &v1alpha1.LabCatalog{ObjectMeta: metav1.ObjectMeta{Name: "nourl"}}, false},
		{"external catalog, http url", &v1alpha1.LabCatalog{ObjectMeta: metav1.ObjectMeta{Name: "plain"}, Spec: v1alpha1.LabCatalogSpec{URL: "http://example.com/c.json"}}, false},
		{"lab with catalogRef", &v1alpha1.Lab{ObjectMeta: metav1.ObjectMeta{Name: "a"}, Spec: v1alpha1.LabSpec{Enabled: true, CatalogRef: &v1alpha1.LabCatalogRef{Catalog: "builtin", Entry: "token-factory"}}}, true},
		{"lab inline", &v1alpha1.Lab{ObjectMeta: metav1.ObjectMeta{Name: "b"}, Spec: v1alpha1.LabSpec{Source: src, Extension: ext}}, true},
		{"lab with both", &v1alpha1.Lab{ObjectMeta: metav1.ObjectMeta{Name: "c"}, Spec: v1alpha1.LabSpec{CatalogRef: &v1alpha1.LabCatalogRef{Catalog: "builtin", Entry: "x"}, Source: src, Extension: ext}}, false},
		{"lab with neither", &v1alpha1.Lab{ObjectMeta: metav1.ObjectMeta{Name: "d"}}, false},
		{"lab source only", &v1alpha1.Lab{ObjectMeta: metav1.ObjectMeta{Name: "e"}, Spec: v1alpha1.LabSpec{Source: src}}, false},
	}
	for _, tc := range cases {
		err := c.Create(ctx, tc.obj)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}
