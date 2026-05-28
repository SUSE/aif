package helm

import (
	"context"
	"sync"
	"time"
)

// FakeCall records one method invocation against FakeEngine.
type FakeCall struct {
	Method    string         // "InstallChartFromRepo", "Uninstall", "Status", ...
	Request   InstallRequest // populated for InstallChartFromRepo only
	Namespace string
	Name      string // release name for Uninstall/Status/Rollback/History
	Revision  int    // populated for Rollback only
}

// InstallOutcome bundles the (status, err) tuple for per-release routing
// via InstallByRelease. Either field may be zero-valued.
type InstallOutcome struct {
	Status ReleaseStatus
	Err    error
}

// RenderCall records one invocation of Render against FakeEngine.
type RenderCall struct {
	Repo, Chart, Version string
	Overrides            Overrides
}

// FakeEngine is a recording fake satisfying Engine and ValueRenderer.
// Pass it to controllers and HTTP handlers under test; assert on Calls and
// Rendered afterwards.
//
// Defaults are friendly: Install returns {Status:"deployed", Revision:1};
// Uninstall returns nil; Status returns ErrReleaseNotFound; Rollback returns
// nil; History returns nil; Render shallow-merges overrides. Override
// per-method via the *Result hooks or *Fn callbacks.
type FakeEngine struct {
	mu    sync.Mutex
	Calls []FakeCall

	InstallResult        func(InstallRequest) (ReleaseStatus, error)
	InstallFromRepoResult func(InstallFromRepoURLRequest) (ReleaseStatus, error)
	UninstallResult      func(ns, name string) error
	StatusResult    func(ns, name string) (ReleaseStatus, error)
	HistoryResult   func(ns, name string) ([]RevisionInfo, error)
	RollbackResult  func(ns, name string, rev int) error

	// InstallByRelease overrides the InstallResult callback for matching
	// release names. Lookup happens in InstallChartFromRepo BEFORE
	// InstallResult — useful for tests that want per-release outcomes
	// without re-implementing the callback.
	InstallByRelease map[string]InstallOutcome

	// RenderFn overrides the default merge behavior for Render. When nil,
	// Render shallow-merges Blueprint → Workload → NIMGenerated and returns.
	RenderFn func(ctx context.Context, repo, chart, version string, ov Overrides) (map[string]any, error)

	// Rendered records every Render invocation. Inspect in tests.
	Rendered []RenderCall

	Settings EngineSettings // last applied
}

// NewFake constructs a FakeEngine with friendly defaults.
func NewFake() *FakeEngine { return &FakeEngine{} }

func (f *FakeEngine) InstallChartFromRepo(_ context.Context, req InstallRequest) (ReleaseStatus, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{
		Method:    "InstallChartFromRepo",
		Request:   req,
		Namespace: req.Namespace,
		Name:      req.ReleaseName,
	})
	if outcome, ok := f.InstallByRelease[req.ReleaseName]; ok {
		f.mu.Unlock()
		return outcome.Status, outcome.Err
	}
	stub := f.InstallResult
	f.mu.Unlock()

	if stub != nil {
		return stub(req)
	}
	return ReleaseStatus{
		Name:     req.ReleaseName,
		Revision: 1,
		Status:   "deployed",
		Updated:  time.Now(),
	}, nil
}

func (f *FakeEngine) InstallFromRepoURL(_ context.Context, req InstallFromRepoURLRequest) (ReleaseStatus, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{
		Method:    "InstallFromRepoURL",
		Namespace: req.Namespace,
		Name:      req.ReleaseName,
	})
	if outcome, ok := f.InstallByRelease[req.ReleaseName]; ok {
		f.mu.Unlock()
		return outcome.Status, outcome.Err
	}
	stub := f.InstallFromRepoResult
	f.mu.Unlock()

	if stub != nil {
		return stub(req)
	}
	return ReleaseStatus{
		Name:     req.ReleaseName,
		Revision: 1,
		Status:   "deployed",
		Updated:  time.Now(),
	}, nil
}

func (f *FakeEngine) Uninstall(_ context.Context, namespace, releaseName string) error {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{
		Method: "Uninstall", Namespace: namespace, Name: releaseName,
	})
	stub := f.UninstallResult
	f.mu.Unlock()

	if stub != nil {
		return stub(namespace, releaseName)
	}
	return nil
}

func (f *FakeEngine) Status(_ context.Context, namespace, releaseName string) (ReleaseStatus, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{
		Method: "Status", Namespace: namespace, Name: releaseName,
	})
	stub := f.StatusResult
	f.mu.Unlock()

	if stub != nil {
		return stub(namespace, releaseName)
	}
	return ReleaseStatus{}, ErrReleaseNotFound
}

func (f *FakeEngine) Rollback(_ context.Context, namespace, releaseName string, revision int) error {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{
		Method: "Rollback", Namespace: namespace, Name: releaseName, Revision: revision,
	})
	stub := f.RollbackResult
	f.mu.Unlock()

	if stub != nil {
		return stub(namespace, releaseName, revision)
	}
	return nil
}

func (f *FakeEngine) History(_ context.Context, namespace, releaseName string) ([]RevisionInfo, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{
		Method: "History", Namespace: namespace, Name: releaseName,
	})
	stub := f.HistoryResult
	f.mu.Unlock()

	if stub != nil {
		return stub(namespace, releaseName)
	}
	return nil, nil
}

func (f *FakeEngine) Render(ctx context.Context, repo, chart, version string, ov Overrides) (map[string]any, error) {
	f.mu.Lock()
	f.Rendered = append(f.Rendered, RenderCall{Repo: repo, Chart: chart, Version: version, Overrides: ov})
	stub := f.RenderFn
	f.mu.Unlock()

	if stub != nil {
		return stub(ctx, repo, chart, version, ov)
	}
	out := map[string]any{}
	for _, src := range []map[string]any{ov.Blueprint, ov.Workload, ov.NIMGenerated} {
		for k, v := range src {
			out[k] = v
		}
	}
	return out, nil
}

func (f *FakeEngine) UpdateSettings(s EngineSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Settings = s
	f.Calls = append(f.Calls, FakeCall{Method: "UpdateSettings"})
}
