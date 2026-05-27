package fleet

import (
	"context"
	"sync"
)

// FakeBundleEngine is an in-memory FleetBundleEngine for downstream
// unit tests (workload deployer, reconciler). Goroutine-safe.
type FakeBundleEngine struct {
	mu       sync.Mutex
	Applied  []BundleDeploymentSpec
	TornDown []string // "namespace/workloadID"
	Settings FleetSettings

	// ApplyErr, TeardownErr can be set by tests to simulate failures.
	ApplyErr    error
	TeardownErr error

	// ObservedStatus overrides what Apply returns. Default: one
	// ClusterDeploymentObserved per TargetClusters entry with empty FleetState.
	ObservedStatus *BundleObservedStatus
}

func NewFakeBundleEngine() *FakeBundleEngine {
	return &FakeBundleEngine{}
}

func (f *FakeBundleEngine) Apply(_ context.Context, spec BundleDeploymentSpec) (BundleObservedStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Applied = append(f.Applied, spec)
	if f.ApplyErr != nil {
		return BundleObservedStatus{}, f.ApplyErr
	}
	if f.ObservedStatus != nil {
		return *f.ObservedStatus, nil
	}
	out := BundleObservedStatus{PerCluster: make([]ClusterDeploymentObserved, 0, len(spec.TargetClusters))}
	for _, c := range spec.TargetClusters {
		out.PerCluster = append(out.PerCluster, ClusterDeploymentObserved{ClusterName: c})
	}
	return out, nil
}

func (f *FakeBundleEngine) Teardown(_ context.Context, ns, workloadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.TeardownErr != nil {
		return f.TeardownErr
	}
	f.TornDown = append(f.TornDown, ns+"/"+workloadID)
	return nil
}

func (f *FakeBundleEngine) UpdateSettings(s FleetSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Settings = s
}

// LastSettings returns the most recent FleetSettings under the mutex,
// safe to call from a goroutine other than the one calling UpdateSettings.
func (f *FakeBundleEngine) LastSettings() FleetSettings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Settings
}
