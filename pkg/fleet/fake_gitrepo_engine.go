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

var _ FleetGitRepoEngine = (*FakeGitRepoEngine)(nil)
