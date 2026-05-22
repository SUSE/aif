package workload

import (
	"context"
	"sync"
)

// FakeDeployer is the in-memory test double for the Deployer port.
// Records every Deploy/Teardown call; returns the configured result/err.
// Race-safe (mutex-guarded) — the controller suite_test.go shares one
// instance across Ginkgo specs.
type FakeDeployer struct {
	mu sync.Mutex

	// Configurable returns
	DeployResult DeployResult
	DeployErr    error
	TeardownErr  error

	// Call recorders
	DeployCalls   []DeployRequest
	TeardownCalls []TeardownCall
}

// TeardownCall captures one Teardown invocation for assertion.
type TeardownCall struct {
	Namespace  string
	WorkloadID string
	Releases   []ComponentRelease
}

// Deploy implements Deployer. Records the request and returns the
// configured result/error.
func (f *FakeDeployer) Deploy(_ context.Context, req DeployRequest) (DeployResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeployCalls = append(f.DeployCalls, req)
	return f.DeployResult, f.DeployErr
}

// Teardown implements Deployer. Records the call and returns the
// configured error.
func (f *FakeDeployer) Teardown(_ context.Context, namespace, workloadID string, releases []ComponentRelease) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TeardownCalls = append(f.TeardownCalls, TeardownCall{
		Namespace:  namespace,
		WorkloadID: workloadID,
		Releases:   releases,
	})
	return f.TeardownErr
}

// Reset clears the call log AND configured returns. Suite-level BeforeEach
// calls this to keep specs order-independent.
func (f *FakeDeployer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeployCalls = nil
	f.TeardownCalls = nil
	f.DeployResult = DeployResult{}
	f.DeployErr = nil
	f.TeardownErr = nil
}

// SetDeployResult thread-safely sets the result returned by Deploy.
// Use this from tests that mutate the result after the reconciler has
// started running (e.g., between Eventually polls).
func (f *FakeDeployer) SetDeployResult(r DeployResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeployResult = r
}

// SetDeployErr thread-safely sets the error returned by Deploy.
func (f *FakeDeployer) SetDeployErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeployErr = err
}

// SetTeardownErr thread-safely sets the error returned by Teardown.
func (f *FakeDeployer) SetTeardownErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TeardownErr = err
}

// GetDeployCalls thread-safely returns a snapshot of the Deploy call log.
func (f *FakeDeployer) GetDeployCalls() []DeployRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot := make([]DeployRequest, len(f.DeployCalls))
	copy(snapshot, f.DeployCalls)
	return snapshot
}

// GetTeardownCalls thread-safely returns a snapshot of the Teardown call log.
func (f *FakeDeployer) GetTeardownCalls() []TeardownCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot := make([]TeardownCall, len(f.TeardownCalls))
	copy(snapshot, f.TeardownCalls)
	return snapshot
}
