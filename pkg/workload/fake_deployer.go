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
	Namespace string
	Releases  []ComponentRelease
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
func (f *FakeDeployer) Teardown(_ context.Context, namespace string, releases []ComponentRelease) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TeardownCalls = append(f.TeardownCalls, TeardownCall{
		Namespace: namespace,
		Releases:  releases,
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
