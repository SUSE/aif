package helm

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sort"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	helmtime "helm.sh/helm/v3/pkg/time"
)

// fakeRunner is a hand-rolled implementation of helmRunner for unit tests.
// Each method records its call so tests can assert on call ordering.
type fakeRunner struct {
	exists       map[string]bool
	pullPath     string
	pullErr      error
	installRel   *helmrelease.Release
	installErr   error
	upgradeRel   *helmrelease.Release
	upgradeErr   error
	uninstallErr error
	getRel       *helmrelease.Release
	getErr       error
	historyRels  []*helmrelease.Release
	historyErr   error
	rollbackErr  error

	calls []string // method names, in order
}

func (f *fakeRunner) Install(_ context.Context, _ *action.Configuration, _ installArgs) (*helmrelease.Release, error) {
	f.calls = append(f.calls, "Install")
	return f.installRel, f.installErr
}
func (f *fakeRunner) Upgrade(_ context.Context, _ *action.Configuration, _ string, _ upgradeArgs) (*helmrelease.Release, error) {
	f.calls = append(f.calls, "Upgrade")
	return f.upgradeRel, f.upgradeErr
}
func (f *fakeRunner) Uninstall(_ context.Context, _ *action.Configuration, _ string) error {
	f.calls = append(f.calls, "Uninstall")
	return f.uninstallErr
}
func (f *fakeRunner) Get(_ context.Context, _ *action.Configuration, _ string) (*helmrelease.Release, error) {
	f.calls = append(f.calls, "Get")
	return f.getRel, f.getErr
}
func (f *fakeRunner) History(_ context.Context, _ *action.Configuration, _ string) ([]*helmrelease.Release, error) {
	f.calls = append(f.calls, "History")
	return f.historyRels, f.historyErr
}
func (f *fakeRunner) Rollback(_ context.Context, _ *action.Configuration, _ string, _ int) error {
	f.calls = append(f.calls, "Rollback")
	return f.rollbackErr
}
func (f *fakeRunner) Exists(_ context.Context, _ *action.Configuration, name string) (bool, error) {
	f.calls = append(f.calls, "Exists")
	return f.exists[name], nil
}
func (f *fakeRunner) Pull(_ context.Context, _ *action.Configuration, _, _ string) (string, error) {
	f.calls = append(f.calls, "Pull")
	return f.pullPath, f.pullErr
}

// newTestEngine builds an engine wired to fr, with a temp chart directory,
// a discard logger, and a no-op cfgFactory so unit tests don't touch a real
// apiserver.
func newTestEngine(t *testing.T, fr helmRunner) *engine {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	return &engine{
		logger:   logger,
		config:   nil,
		runner:   fr,
		chartDir: dir,
		cfgFactory: func(_ string) (*action.Configuration, error) {
			return &action.Configuration{}, nil
		},
	}
}

// writeTinyChart writes a minimal valid chart on disk and returns its path.
// The chart only needs to load via loader.Load — its templates are never
// executed by the runner fake.
func writeTinyChart(t *testing.T, dir string) string {
	t.Helper()
	chartDir := dir + "/tiny"
	if err := os.MkdirAll(chartDir+"/templates", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(chartDir+"/Chart.yaml",
		[]byte("apiVersion: v2\nname: tiny\nversion: 0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	if err := os.WriteFile(chartDir+"/values.yaml", []byte("replicas: 1\n"), 0o644); err != nil {
		t.Fatalf("write values.yaml: %v", err)
	}
	if err := os.WriteFile(chartDir+"/templates/_helpers.tpl", []byte(""), 0o644); err != nil {
		t.Fatalf("write helpers: %v", err)
	}
	return chartDir
}

func testRelease(name string, rev int) *helmrelease.Release {
	return &helmrelease.Release{
		Name:    name,
		Version: rev,
		Info: &helmrelease.Info{
			Status:       helmrelease.StatusDeployed,
			LastDeployed: helmtime.Now(),
		},
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInstallChartFromRepo_NoExistingRelease_CallsInstall(t *testing.T) {
	fr := &fakeRunner{
		pullPath:   writeTinyChart(t, t.TempDir()),
		installRel: testRelease("ext", 1),
	}
	e := newTestEngine(t, fr)

	got, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "ext", ChartRef: "oci://x/y:1",
		Overrides: Overrides{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Revision != 1 || got.Name != "ext" {
		t.Errorf("ReleaseStatus mismatch: %+v", got)
	}
	wantCalls := []string{"Exists", "Pull", "Install"}
	if !equalStrings(fr.calls, wantCalls) {
		t.Errorf("call order mismatch: got %v want %v", fr.calls, wantCalls)
	}
}

func TestInstallChartFromRepo_ReleaseExists_CallsUpgrade(t *testing.T) {
	fr := &fakeRunner{
		exists:     map[string]bool{"ext": true},
		pullPath:   writeTinyChart(t, t.TempDir()),
		upgradeRel: testRelease("ext", 2),
	}
	e := newTestEngine(t, fr)

	got, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "ext", ChartRef: "oci://x/y:1",
		Overrides: Overrides{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Revision != 2 {
		t.Errorf("expected upgrade to revision 2, got %d", got.Revision)
	}
	wantCalls := []string{"Exists", "Pull", "Upgrade"}
	if !equalStrings(fr.calls, wantCalls) {
		t.Errorf("call order mismatch: got %v want %v", fr.calls, wantCalls)
	}
}

func TestInstallChartFromRepo_PullFailure_WrapsErrPullFailed(t *testing.T) {
	fr := &fakeRunner{pullErr: errors.New("network down")}
	e := newTestEngine(t, fr)

	_, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "ext", ChartRef: "oci://x/y:1",
		Overrides: Overrides{},
	})
	if !errors.Is(err, ErrPullFailed) {
		t.Fatalf("expected ErrPullFailed, got %v", err)
	}
}

func TestUninstall_ReleaseNotFound_ReturnsNil(t *testing.T) {
	fr := &fakeRunner{uninstallErr: driver.ErrReleaseNotFound}
	e := newTestEngine(t, fr)

	if err := e.Uninstall(context.Background(), "ns", "missing"); err != nil {
		t.Fatalf("expected nil for not-found release, got %v", err)
	}
}

func TestUninstall_OtherError_Wrapped(t *testing.T) {
	fr := &fakeRunner{uninstallErr: errors.New("kaboom")}
	e := newTestEngine(t, fr)

	err := e.Uninstall(context.Background(), "ns", "x")
	if err == nil || !errors.Is(err, fr.uninstallErr) {
		t.Fatalf("expected wrapped 'kaboom' error, got %v", err)
	}
}

func TestStatus_Found_TranslatesRelease(t *testing.T) {
	fr := &fakeRunner{getRel: testRelease("ext", 7)}
	e := newTestEngine(t, fr)

	got, err := e.Status(context.Background(), "ns", "ext")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Revision != 7 {
		t.Errorf("expected revision 7, got %d", got.Revision)
	}
	if got.Status != "deployed" {
		t.Errorf("expected status 'deployed', got %q", got.Status)
	}
}

func TestStatus_NotFound_ReturnsErrReleaseNotFound(t *testing.T) {
	fr := &fakeRunner{getErr: driver.ErrReleaseNotFound}
	e := newTestEngine(t, fr)

	_, err := e.Status(context.Background(), "ns", "missing")
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("expected ErrReleaseNotFound, got %v", err)
	}
}

func TestHistory_SortsNewestFirst(t *testing.T) {
	old := testRelease("ext", 1)
	old.Info.LastDeployed = helmtime.Time{Time: time.Now().Add(-time.Hour)}
	mid := testRelease("ext", 2)
	mid.Info.LastDeployed = helmtime.Time{Time: time.Now().Add(-time.Minute)}
	newest := testRelease("ext", 3) // helmtime.Now() inside testRelease

	fr := &fakeRunner{historyRels: []*helmrelease.Release{old, mid, newest}}
	e := newTestEngine(t, fr)

	got, err := e.History(context.Background(), "ns", "ext")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 revisions, got %d", len(got))
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].Updated.After(got[j].Updated) }) {
		t.Errorf("history not sorted newest-first: %+v", got)
	}
	if got[0].Revision != 3 {
		t.Errorf("expected newest revision 3 first, got %d", got[0].Revision)
	}
}

func TestRollback_PassesRevisionToRunner(t *testing.T) {
	fr := &fakeRunner{}
	e := newTestEngine(t, fr)

	if err := e.Rollback(context.Background(), "ns", "ext", 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := fr.calls; len(got) != 1 || got[0] != "Rollback" {
		t.Errorf("expected one Rollback call, got %v", got)
	}
}

func TestRollback_RunnerError_Wrapped(t *testing.T) {
	fr := &fakeRunner{rollbackErr: errors.New("boom")}
	e := newTestEngine(t, fr)

	err := e.Rollback(context.Background(), "ns", "ext", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, fr.rollbackErr) {
		t.Errorf("expected wrapped 'boom', got %v", err)
	}
}

func TestRollback_NotFound_ReturnsErrReleaseNotFound(t *testing.T) {
	fr := &fakeRunner{rollbackErr: driver.ErrReleaseNotFound}
	e := newTestEngine(t, fr)

	err := e.Rollback(context.Background(), "ns", "missing", 1)
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("expected ErrReleaseNotFound, got %v", err)
	}
}

func TestInstallChartFromRepo_PullFailure_PreservesUnderlyingError(t *testing.T) {
	underlying := errors.New("network down")
	fr := &fakeRunner{pullErr: underlying}
	e := newTestEngine(t, fr)

	_, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "ext", ChartRef: "oci://x/y:1",
		Overrides: Overrides{},
	})
	if !errors.Is(err, ErrPullFailed) {
		t.Errorf("expected ErrPullFailed in chain, got %v", err)
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected underlying error preserved in chain, got %v", err)
	}
}
