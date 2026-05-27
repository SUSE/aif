package fleet

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestFakeBundleEngine_RecordsApplyAndTeardown(t *testing.T) {
	f := NewFakeBundleEngine()
	spec := BundleDeploymentSpec{
		WorkloadID: "x", WorkloadNS: "n",
		Components:     []ComponentBundle{{Name: "c", ChartRef: "oci://r/c:1", Values: map[string]any{}}},
		TargetClusters: []string{"a", "b"},
		Owner:          OwnerRef{Name: "x"},
	}
	obs, err := f.Apply(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.PerCluster) != 2 {
		t.Fatalf("PerCluster len = %d, want 2", len(obs.PerCluster))
	}
	if f.Applied[0].WorkloadID != "x" {
		t.Fatalf("Apply not recorded")
	}
	if err := f.Teardown(context.Background(), "n", "x"); err != nil {
		t.Fatal(err)
	}
	if len(f.TornDown) != 1 || f.TornDown[0] != "n/x" {
		t.Fatalf("Teardown not recorded: %v", f.TornDown)
	}
}

// TestFakeBundleEngine_LastSettings_Concurrent exercises the engine's
// sync.Mutex under concurrent UpdateSettings / LastSettings calls.
// Run with `go test -race` to catch a regression of the mutex being
// removed or scoped incorrectly. Without the mutex, the FleetSettings
// struct copy flags as a data race.
func TestFakeBundleEngine_LastSettings_Concurrent(t *testing.T) {
	f := NewFakeBundleEngine()
	const n = 32

	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			f.UpdateSettings(FleetSettings{GitRepoURL: fmt.Sprintf("https://x.example/%d", i)})
		}(i)
		go func() {
			defer wg.Done()
			_ = f.LastSettings()
		}()
	}
	wg.Wait()

	// Last writer wins; we only assert that we observe one of the n values.
	got := f.LastSettings().GitRepoURL
	if got == "" {
		t.Fatalf("LastSettings returned zero value after %d UpdateSettings calls", n)
	}
}
