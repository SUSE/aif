package helm

import (
	"context"
	"errors"
	"fmt"
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

	// Recording support for Task 9 (layer-1 merge tests).
	lastInstallArgs *installArgs
	lastUpgradeArgs *upgradeArgs
	chartDefaults   map[string]any
}

func (f *fakeRunner) Install(_ context.Context, _ *action.Configuration, args installArgs) (*helmrelease.Release, error) {
	f.calls = append(f.calls, "Install")
	f.lastInstallArgs = &args
	return f.installRel, f.installErr
}
func (f *fakeRunner) Upgrade(_ context.Context, _ *action.Configuration, _ string, args upgradeArgs) (*helmrelease.Release, error) {
	f.calls = append(f.calls, "Upgrade")
	f.lastUpgradeArgs = &args
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

// writeTinyChartWithValues extends writeTinyChart to allow injecting custom
// values.yaml content (for Task 9 layer-1 merge tests).
func writeTinyChartWithValues(t *testing.T, dir string, values map[string]any) string {
	t.Helper()
	chartDir := dir + "/tiny"
	if err := os.MkdirAll(chartDir+"/templates", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(chartDir+"/Chart.yaml",
		[]byte("apiVersion: v2\nname: tiny\nversion: 0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}

	// Convert map to YAML manually for minimal values (avoids yaml dep in test).
	// Only supports string and int leaf types + nested maps (enough for Task 9).
	var valuesYAML string
	for k, v := range values {
		switch tv := v.(type) {
		case map[string]any:
			valuesYAML += k + ":\n"
			for sk, sv := range tv {
				valuesYAML += "  " + sk + ": " + fmt.Sprint(sv) + "\n"
			}
		default:
			valuesYAML += k + ": " + fmt.Sprint(v) + "\n"
		}
	}

	if err := os.WriteFile(chartDir+"/values.yaml", []byte(valuesYAML), 0o644); err != nil {
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
	chartDefaults := map[string]any{
		"image": map[string]any{
			"repository": "registry.example.com/app",
		},
	}
	fr := &fakeRunner{
		pullPath:   writeTinyChartWithValues(t, t.TempDir(), chartDefaults),
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
	chartDefaults := map[string]any{
		"image": map[string]any{
			"repository": "registry.example.com/app",
		},
	}
	fr := &fakeRunner{
		exists:     map[string]bool{"ext": true},
		pullPath:   writeTinyChartWithValues(t, t.TempDir(), chartDefaults),
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

func TestInstallChartFromRepo_MergesChartDefaultsAsLayer1(t *testing.T) {
	chartDefaults := map[string]any{
		"image": map[string]any{
			"repository": "registry.suse.com/ai/llm",
			"tag":        "1.0",
		},
		"replicaCount": 2,
	}
	wlOverrides := map[string]any{
		"replicaCount": 5,
	}

	fr := newRecordingRunner(t,
		withChartDefaults(chartDefaults),
		withInstallSucceeds(),
	)
	e := newTestEngine(t, fr)

	_, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace:   "ns",
		ReleaseName: "rel",
		ChartRef:    "oci://registry.suse.com/ai/charts/llm:1.0",
		Overrides:   Overrides{Workload: wlOverrides},
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	// Engine should pass MERGED values to Helm: layer 1 (defaults) +
	// layer 3 (Workload). image.repository from defaults survives;
	// replicaCount overridden by Workload.
	got := fr.lastInstallValues()
	image, ok := got["image"].(map[string]any)
	if !ok {
		t.Fatalf("merged values missing image map; got=%v", got)
	}
	if image["repository"] != "registry.suse.com/ai/llm" {
		t.Errorf("image.repository=%v, want registry.suse.com/ai/llm", image["repository"])
	}
	if got["replicaCount"] != 5 {
		t.Errorf("replicaCount=%v, want 5 (Workload override)", got["replicaCount"])
	}
}

// Recording runner helpers for Task 9 layer-1 merge tests.

type runnerOption func(*fakeRunner)

func withChartDefaults(defaults map[string]any) runnerOption {
	return func(fr *fakeRunner) {
		fr.chartDefaults = defaults
	}
}

func withInstallSucceeds() runnerOption {
	return func(fr *fakeRunner) {
		fr.installRel = testRelease("rel", 1)
	}
}

func newRecordingRunner(t *testing.T, opts ...runnerOption) *fakeRunner {
	t.Helper()
	fr := &fakeRunner{
		pullPath: writeTinyChartWithValues(t, t.TempDir(), nil),
	}
	for _, opt := range opts {
		opt(fr)
	}
	// If chartDefaults are provided, rewrite the chart on disk to contain them.
	if fr.chartDefaults != nil {
		fr.pullPath = writeTinyChartWithValues(t, t.TempDir(), fr.chartDefaults)
	}
	return fr
}

func (f *fakeRunner) lastInstallValues() map[string]any {
	if f.lastInstallArgs == nil {
		return nil
	}
	return f.lastInstallArgs.Values
}

func TestInstallChartFromRepo_AppliesImageRewriteFromEngineSettings(t *testing.T) {
	chartDefaults := map[string]any{
		"image": map[string]any{"repository": "registry.suse.com/ai/llm", "tag": "1.0"},
	}
	runner := newRecordingRunner(t, withChartDefaults(chartDefaults), withInstallSucceeds())
	e := newTestEngine(t, runner)

	e.UpdateSettings(EngineSettings{
		ImageRewrite: ImageRewriteConfig{
			Enabled: true,
			Rules: []ImageRewriteRule{
				{Match: "registry.suse.com/", Replace: "harbor.example.com/suse/"},
			},
		},
	})

	_, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "rel", ChartRef: "oci://x/y:1",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	got := runner.lastInstallValues()
	image, ok := got["image"].(map[string]any)
	if !ok {
		t.Fatalf("image map missing: %v", got)
	}
	if image["repository"] != "harbor.example.com/suse/ai/llm" {
		t.Errorf("image.repository=%v, want harbor.example.com/suse/ai/llm", image["repository"])
	}
}

func TestInstallChartFromRepo_NoRewrite_WhenRulesEmpty(t *testing.T) {
	chartDefaults := map[string]any{
		"image": map[string]any{"repository": "registry.suse.com/ai/llm", "tag": "1.0"},
	}
	runner := newRecordingRunner(t, withChartDefaults(chartDefaults), withInstallSucceeds())
	e := newTestEngine(t, runner)

	// No UpdateSettings call → ImageRewrite zero-value → no rewrite.

	_, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "rel", ChartRef: "oci://x/y:1",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	image := runner.lastInstallValues()["image"].(map[string]any)
	if image["repository"] != "registry.suse.com/ai/llm" {
		t.Errorf("image.repository=%v, want unchanged", image["repository"])
	}
}

func TestInstallChartFromRepo_AppendsImagePullSecretsAsLayer6(t *testing.T) {
	runner := newRecordingRunner(t,
		withChartDefaults(map[string]any{"image": map[string]any{"repository": "registry.suse.com/x", "tag": "1"}}),
		withInstallSucceeds(),
	)
	e := newTestEngine(t, runner)

	_, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "rel", ChartRef: "oci://x/y:1",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	got := runner.lastInstallValues()
	ips, ok := got["imagePullSecrets"].([]any)
	if !ok {
		t.Fatalf("imagePullSecrets missing or wrong type: %T", got["imagePullSecrets"])
	}
	if len(ips) != 1 {
		t.Fatalf("imagePullSecrets len=%d, want 1", len(ips))
	}
	entry, ok := ips[0].(map[string]any)
	if !ok || entry["name"] != "suse-registry-creds" {
		t.Errorf("imagePullSecrets[0]=%v, want {name: suse-registry-creds}", ips[0])
	}
}

func TestInstallChartFromRepo_OverridesPullSecretEntry_StillEmits(t *testing.T) {
	// User-supplied imagePullSecrets are forbidden top-level keys (per
	// MergeValues policy); they should be silently dropped from layer 3,
	// then layer 6 adds the constant entry. Result: still exactly 1 entry.
	runner := newRecordingRunner(t,
		withChartDefaults(map[string]any{"image": map[string]any{"repository": "registry.suse.com/x", "tag": "1"}}),
		withInstallSucceeds(),
	)
	e := newTestEngine(t, runner)

	_, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "rel", ChartRef: "oci://x/y:1",
		Overrides: Overrides{Workload: map[string]any{
			"imagePullSecrets": []any{map[string]any{"name": "evil"}},
		}},
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	got := runner.lastInstallValues()
	ips, ok := got["imagePullSecrets"].([]any)
	if !ok {
		t.Fatalf("imagePullSecrets missing or wrong type: %T", got["imagePullSecrets"])
	}
	if len(ips) != 1 {
		t.Fatalf("imagePullSecrets len=%d, want 1 (user override dropped, constant added)", len(ips))
	}
	entry, ok := ips[0].(map[string]any)
	if !ok || entry["name"] != "suse-registry-creds" {
		t.Errorf("imagePullSecrets[0]=%v, want {name: suse-registry-creds}", ips[0])
	}
}

func TestInstallChartFromRepo_ReturnsErrMissingImageRepository(t *testing.T) {
	chartDefaults := map[string]any{} // no image at all
	runner := newRecordingRunner(t, withChartDefaults(chartDefaults))
	e := newTestEngine(t, runner)

	_, err := e.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace:              "ns",
		ReleaseName:            "rel",
		ChartRef:               "oci://x/y:1",
		RequireImageRepository: true,
	})
	if !errors.Is(err, ErrMissingImageRepository) {
		t.Errorf("err=%v, want ErrMissingImageRepository", err)
	}
}

func TestEngine_Render_DoesNotInstall(t *testing.T) {
	chartDefaults := map[string]any{
		"image": map[string]any{"repository": "registry.example.com/app"},
		"replicas": 1,
	}
	fr := &fakeRunner{
		pullPath: writeTinyChartWithValues(t, t.TempDir(), chartDefaults),
	}
	e := newTestEngine(t, fr)

	got, err := e.Render(context.Background(), "registry.example.com", "ai/charts/nim-llm", "1.2.3",
		Overrides{Workload: map[string]any{"replicas": 3}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["replicas"] != 3 {
		t.Errorf("expected Workload override to win, got %v", got["replicas"])
	}
	// Layer 6 (imagePullSecrets) must NOT be applied — Fleet ships
	// pull-secrets as a separate resource.
	if _, ok := got["imagePullSecrets"]; ok {
		t.Errorf("Render must not inject imagePullSecrets; got %v", got["imagePullSecrets"])
	}
	wantCalls := []string{"Pull"}
	if !equalStrings(fr.calls, wantCalls) {
		t.Errorf("call order mismatch: got %v want %v", fr.calls, wantCalls)
	}
}
