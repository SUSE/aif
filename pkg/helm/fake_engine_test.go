// pkg/helm/fake_engine_test.go
package helm

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestFakeEngine_RecordsCallsInOrder(t *testing.T) {
	f := NewFake()

	if _, err := f.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "r1", ChartRef: "oci://x:1",
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := f.Uninstall(context.Background(), "ns", "r1"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := f.Status(context.Background(), "ns", "r1"); err == nil {
		// default Status returns ErrReleaseNotFound; nil is unexpected
		t.Fatalf("expected default Status to return ErrReleaseNotFound, got nil")
	}

	got := f.Calls
	if len(got) != 3 {
		t.Fatalf("expected 3 calls, got %d: %+v", len(got), got)
	}
	if got[0].Method != "InstallChartFromRepo" || got[0].Request.ReleaseName != "r1" {
		t.Errorf("call[0] mismatch: %+v", got[0])
	}
	if got[1].Method != "Uninstall" || got[1].Name != "r1" {
		t.Errorf("call[1] mismatch: %+v", got[1])
	}
	if got[2].Method != "Status" {
		t.Errorf("call[2] mismatch: %+v", got[2])
	}
}

func TestFakeEngine_DefaultInstallReturnsDeployedRev1(t *testing.T) {
	f := NewFake()
	got, err := f.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "r1", ChartRef: "oci://x:1",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got.Revision != 1 || got.Status != "deployed" || got.Name != "r1" {
		t.Errorf("default install result wrong: %+v", got)
	}
}

func TestFakeEngine_StubOverridesDefault(t *testing.T) {
	stubErr := errors.New("simulated boom")
	f := NewFake()
	f.InstallResult = func(InstallRequest) (ReleaseStatus, error) {
		return ReleaseStatus{}, stubErr
	}

	_, err := f.InstallChartFromRepo(context.Background(), InstallRequest{
		Namespace: "ns", ReleaseName: "r1", ChartRef: "oci://x:1",
	})
	if !errors.Is(err, stubErr) {
		t.Errorf("expected stubbed error, got %v", err)
	}
}

func TestFakeEngine_UpdateSettings_StoredInSettings(t *testing.T) {
	f := NewFake()
	want := EngineSettings{RegistryEndpoints: RegistryEndpoints{SUSERegistry: "r.example"}}
	f.UpdateSettings(want)
	if f.Settings.RegistryEndpoints.SUSERegistry != "r.example" {
		t.Errorf("Settings not stored: %+v", f.Settings)
	}
}

func TestFakeEngine_ConcurrentCalls_NoRace(t *testing.T) {
	f := NewFake()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.InstallChartFromRepo(context.Background(), InstallRequest{Namespace: "ns", ReleaseName: "r"})
			_ = f.Uninstall(context.Background(), "ns", "r")
		}()
	}
	wg.Wait()
	if len(f.Calls) != 200 {
		t.Errorf("expected 200 calls, got %d", len(f.Calls))
	}
}

func TestFakeEngine_SatisfiesEngineInterface(t *testing.T) {
	var _ Engine = NewFake()
}
