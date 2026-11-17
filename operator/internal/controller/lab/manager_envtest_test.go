package lab

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/controller/labcatalog"
)

// TestControllersInManager runs the Lab and LabCatalog controllers in a real
// manager against envtest, covering what the fake client can't: watch events
// (status writes must not re-trigger fetches), the duplicate InstallAIExtension
// watch, real CRD validation of the status writes, and recovery from a conflict.
// Skipped when the envtest binaries are not installed.
func TestControllersInManager(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		dirs, _ := filepath.Glob(filepath.Join("..", "..", "..", "bin", "k8s", "*"))
		if len(dirs) == 0 {
			t.Skip("envtest binaries not found; run make setup-envtest")
		}
		assets = dirs[0]
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "charts", "aif-operator", "crds")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: assets,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = env.Stop() }()

	sch := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(sch)
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: sch, Metrics: metricsserver.Options{BindAddress: "0"}})
	if err != nil {
		t.Fatal(err)
	}

	var fetches atomic.Int32
	var version atomic.Value
	version.Store("1")
	fetch := func(context.Context, string) ([]byte, error) {
		fetches.Add(1)
		time.Sleep(1100 * time.Millisecond) // slow host: lastFetched (1s precision) always changes
		v := version.Load().(string)
		return []byte(`[{"name":"demo","source":{"kind":"Helm","helm":{"chartURL":"oci://r.io/chart/aif-lab-demo","version":"` + v +
			`"}},"extension":{"name":"aif-lab-demo","version":"` + v + `"}},
			{"name":"bad","source":{"kind":"Helm"},"extension":{"name":"aif-lab-bad","version":"1"}}]`), nil
	}
	if err := (&labcatalog.LabCatalogReconciler{Client: mgr.GetClient(), Scheme: sch, Fetch: fetch,
		AllowedRegistryHosts: []string{"r.io"}}).SetupWithManager(mgr); err != nil {
		t.Fatal(err)
	}
	if err := (&LabReconciler{Client: mgr.GetClient(), Scheme: sch}).SetupWithManager(mgr); err != nil {
		t.Fatalf("Lab controller setup (Owns + Watches on InstallAIExtension): %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = mgr.Start(ctx) }()

	c, err := client.New(cfg, client.Options{Scheme: sch})
	if err != nil {
		t.Fatal(err)
	}
	eventually := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for: %s", what)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// H3 + H4: one fetch, status accepted by the API server despite a bad entry.
	if err := c.Create(ctx, &v1alpha1.LabCatalog{ObjectMeta: metav1.ObjectMeta{Name: "partner"},
		Spec: v1alpha1.LabCatalogSpec{URL: "https://example.com/c.json"}}); err != nil {
		t.Fatal(err)
	}
	var cat v1alpha1.LabCatalog
	eventually("catalog Ready", func() bool {
		_ = c.Get(ctx, client.ObjectKey{Name: "partner"}, &cat)
		return cat.Status.Phase == v1alpha1.LabCatalogPhaseReady
	})
	if len(cat.Status.Entries) != 1 || cat.Status.Skipped != 1 {
		t.Fatalf("entries=%d skipped=%d msg=%q", len(cat.Status.Entries), cat.Status.Skipped, cat.Status.Message)
	}
	time.Sleep(5 * time.Second)
	if n := fetches.Load(); n != 1 {
		t.Fatalf("H3: %d fetches in ~6s, want 1 (status write re-triggered the fetch)", n)
	}

	// Lab install, real status write with catalogRef.version.
	if err := c.Create(ctx, &v1alpha1.Lab{ObjectMeta: metav1.ObjectMeta{Name: "l"}, Spec: v1alpha1.LabSpec{Enabled: true,
		CatalogRef: &v1alpha1.LabCatalogRef{Catalog: "partner", Entry: "demo", Version: "1"}}}); err != nil {
		t.Fatal(err)
	}
	var child v1alpha1.InstallAIExtension
	eventually("child created", func() bool { return c.Get(ctx, client.ObjectKey{Name: "lab-l"}, &child) == nil })
	var lab v1alpha1.Lab
	eventually("Lab Pending with resolved version", func() bool {
		_ = c.Get(ctx, client.ObjectKey{Name: "l"}, &lab)
		return lab.Status.Phase == v1alpha1.LabPhasePending && lab.Status.ResolvedVersion == "1"
	})

	// Catalog offers v2 (spec change triggers a refetch): the child stays on v1.
	version.Store("2")
	cat.Spec.DisplayName = "bump"
	if err := c.Update(ctx, &cat); err != nil {
		t.Fatal(err)
	}
	eventually("updateAvailable=2", func() bool {
		_ = c.Get(ctx, client.ObjectKey{Name: "l"}, &lab)
		return lab.Status.UpdateAvailable == "2"
	})
	_ = c.Get(ctx, client.ObjectKey{Name: "lab-l"}, &child)
	if child.Spec.Source.Helm.Version != "1" {
		t.Fatalf("child upgraded without acceptance: %s", child.Spec.Source.Helm.Version)
	}
	lab.Spec.CatalogRef.Version = "2"
	if err := c.Update(ctx, &lab); err != nil {
		t.Fatal(err)
	}
	eventually("child upgraded to 2 after acceptance", func() bool {
		_ = c.Get(ctx, client.ObjectKey{Name: "lab-l"}, &child)
		return child.Spec.Source.Helm.Version == "2"
	})

	// M2 recovery: a second Lab conflicts with lab-l; deleting lab "l" frees the name.
	if err := c.Create(ctx, &v1alpha1.Lab{ObjectMeta: metav1.ObjectMeta{Name: "l2"}, Spec: v1alpha1.LabSpec{Enabled: true,
		CatalogRef: &v1alpha1.LabCatalogRef{Catalog: "partner", Entry: "demo"}}}); err != nil {
		t.Fatal(err)
	}
	var l2 v1alpha1.Lab
	eventually("l2 Failed on conflict", func() bool {
		_ = c.Get(ctx, client.ObjectKey{Name: "l2"}, &l2)
		return l2.Status.Phase == v1alpha1.LabPhaseFailed
	})
	// envtest runs no garbage collector: delete the child too, as GC would.
	if err := c.Delete(ctx, &lab); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, &child); err != nil {
		t.Fatal(err)
	}
	eventually("l2 recovers once the conflicting extension is gone", func() bool {
		return c.Get(ctx, client.ObjectKey{Name: "lab-l2"}, &v1alpha1.InstallAIExtension{}) == nil
	})
}
