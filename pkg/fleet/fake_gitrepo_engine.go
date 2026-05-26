package fleet

import (
	"context"
	"sync"
)

// FakeGitRepoEngine is an in-memory FleetGitRepoEngine for controller
// tests. Records Apply/Teardown invocations; configurable error and
// observed status.
type FakeGitRepoEngine struct {
	mu sync.Mutex

	Applied  []GitRepoDeploymentSpec
	TornDown []string // "namespace/workloadID"

	Settings FleetSettings

	ApplyErr       error
	TeardownErr    error
	ObservedStatus GitRepoObservedStatus
}

func (f *FakeGitRepoEngine) Apply(_ context.Context, spec GitRepoDeploymentSpec) (GitRepoObservedStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Applied = append(f.Applied, spec)
	if f.ApplyErr != nil {
		return GitRepoObservedStatus{}, f.ApplyErr
	}
	return f.ObservedStatus, nil
}

func (f *FakeGitRepoEngine) Teardown(_ context.Context, namespace, workloadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TornDown = append(f.TornDown, namespace+"/"+workloadID)
	return f.TeardownErr
}

func (f *FakeGitRepoEngine) UpdateSettings(s FleetSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Settings = s
}

// AppliedSnapshot returns a copy of Applied under the mutex. Tests that
// read the recorded specs from a goroutine other than the one calling
// Apply (e.g. envtest specs polling via Eventually while the reconciler
// drives Apply on the controller's worker) must use this accessor to
// avoid `go test -race` failures.
func (f *FakeGitRepoEngine) AppliedSnapshot() []GitRepoDeploymentSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]GitRepoDeploymentSpec, len(f.Applied))
	copy(out, f.Applied)
	return out
}

// TearDownSnapshot returns a copy of TornDown under the mutex.
// Companion to AppliedSnapshot for tests that poll teardown progress
// from a goroutine other than the one calling Teardown.
func (f *FakeGitRepoEngine) TearDownSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.TornDown))
	copy(out, f.TornDown)
	return out
}

var _ FleetGitRepoEngine = (*FakeGitRepoEngine)(nil)
