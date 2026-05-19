//go:build envtest
// +build envtest

package helm

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestEngine_HappyPath_Envtest exercises the full lifecycle against a real
// envtest apiserver: install → upgrade → status → history → rollback →
// uninstall. Pull is bypassed (envtest can't reach an OCI registry); the
// chart is loaded from testdata/tiny-chart directly. The underlying SDK
// path (action.NewInstall/Upgrade/Get/History/Rollback/Uninstall) is
// exercised via realRunner.
//
// Run: KUBEBUILDER_ASSETS="$(setup-envtest use --print path 1.32.x)" \
//      go test -tags envtest ./pkg/helm/ -v -run TestEngine_HappyPath_Envtest
func TestEngine_HappyPath_Envtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest (run via Makefile target test-helm-envtest)")
	}

	te := &envtest.Environment{}
	cfg, err := te.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	t.Cleanup(func() { _ = te.Stop() })

	srcChart, err := filepath.Abs(filepath.Join("testdata", "tiny-chart"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	if _, err := loader.Load(srcChart); err != nil {
		t.Fatalf("loader.Load(%q): %v", srcChart, err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	chartScratch := t.TempDir()
	e := New(logger, cfg, WithChartDir(chartScratch)).(*engine)

	// Override Pull only — every other runner method delegates to realRunner
	// against the real envtest apiserver. Copy-per-Pull keeps the testdata
	// source intact when engine.go switches to os.RemoveAll (Task 13).
	e.runner = &localChartRunner{realRunner: realRunner{}, srcChart: srcChart}

	ns := "envtest-helm"
	rel := "tiny"

	// Install
	st, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace:   ns,
		ReleaseName: rel,
		ChartRef:    "oci://ignored",
		Overrides:   Overrides{Workload: map[string]any{"message": "v1"}},
		Wait:        false,
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if st.Revision != 1 {
		t.Errorf("expected install revision 1, got %d", st.Revision)
	}

	// Upgrade (idempotent re-call)
	st, err = e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace:   ns,
		ReleaseName: rel,
		ChartRef:    "oci://ignored",
		Overrides:   Overrides{Workload: map[string]any{"message": "v2"}},
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if st.Revision != 2 {
		t.Errorf("expected upgrade revision 2, got %d", st.Revision)
	}

	// Status
	st, err = e.Status(context.Background(), ns, rel)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Revision != 2 {
		t.Errorf("expected status revision 2, got %d", st.Revision)
	}

	// History — newest first
	hist, err := e.History(context.Background(), ns, rel)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(hist))
	}
	if hist[0].Revision != 2 || hist[1].Revision != 1 {
		t.Errorf("history not newest-first: %+v", hist)
	}

	// Rollback to revision 1
	if err := e.Rollback(context.Background(), ns, rel, 1); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	st, err = e.Status(context.Background(), ns, rel)
	if err != nil {
		t.Fatalf("status post-rollback: %v", err)
	}
	if st.Revision != 3 {
		t.Errorf("expected post-rollback revision 3, got %d", st.Revision)
	}

	// Uninstall
	if err := e.Uninstall(context.Background(), ns, rel); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	// Status now returns ErrReleaseNotFound
	if _, err := e.Status(context.Background(), ns, rel); !errors.Is(err, ErrReleaseNotFound) {
		t.Errorf("expected ErrReleaseNotFound after uninstall, got %v", err)
	}
}

// localChartRunner overrides Pull to copy a local chart directory into a
// fresh per-pull subdir under destDir, then returns a path *inside* that
// subdir so the engine's defer (os.RemoveAll(filepath.Dir(chartPath)))
// cleans the subdir without touching siblings or the testdata source.
// Every other runner method delegates to realRunner.
type localChartRunner struct {
	realRunner
	srcChart string
}

func (r *localChartRunner) Pull(_ context.Context, _ *action.Configuration, _, destDir string) (string, error) {
	pullDir, err := os.MkdirTemp(destDir, "chart-")
	if err != nil {
		return "", err
	}
	chartCopy := filepath.Join(pullDir, "chart")
	if err := os.MkdirAll(chartCopy, 0o755); err != nil {
		_ = os.RemoveAll(pullDir)
		return "", err
	}
	if err := os.CopyFS(chartCopy, os.DirFS(r.srcChart)); err != nil {
		_ = os.RemoveAll(pullDir)
		return "", err
	}
	return chartCopy, nil
}
