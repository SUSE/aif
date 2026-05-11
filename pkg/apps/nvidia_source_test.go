package apps

import (
	"context"
	stderrors "errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/SUSE/aif/pkg/nvidia"
)

// fakeNVIDIADiscovery is an in-memory test double for nvidia.Discovery
// (4 methods). Tests control what Refresh/Index return and observe
// what UpdateSettings was called with.
type fakeNVIDIADiscovery struct {
	mu sync.Mutex

	// Configurable behavior:
	indexResult    []nvidia.NIMEntry
	refreshErr     error
	indexErr       error
	getResult      nvidia.NIMEntry
	getErr         error
	settingsCalls  []nvidia.EngineSettings
	refreshCalls   int
}

func (f *fakeNVIDIADiscovery) Index(_ context.Context) ([]nvidia.NIMEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.indexErr != nil {
		return nil, f.indexErr
	}
	out := make([]nvidia.NIMEntry, len(f.indexResult))
	copy(out, f.indexResult)
	return out, nil
}

func (f *fakeNVIDIADiscovery) Get(_ context.Context, _ string) (nvidia.NIMEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getResult, f.getErr
}

func (f *fakeNVIDIADiscovery) Refresh(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls++
	return f.refreshErr
}

func (f *fakeNVIDIADiscovery) UpdateSettings(s nvidia.EngineSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settingsCalls = append(f.settingsCalls, s)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func sampleNIMEntries() []nvidia.NIMEntry {
	return []nvidia.NIMEntry{
		{
			ID:          "nim-llm:1.0.0",
			Chart:       "nim-llm",
			Version:     "1.0.0",
			DisplayName: "nim-llm",
			Type:        nvidia.TypeLLM,
			ChartRef:    "oci://registry.suse.com/ai/charts/nvidia/nim-llm:1.0.0",
		},
		{
			ID:          "nim-vlm:2.0.0",
			Chart:       "nim-vlm",
			Version:     "2.0.0",
			DisplayName: "nim-vlm",
			Type:        nvidia.TypeVLM,
			ChartRef:    "oci://registry.suse.com/ai/charts/nvidia/nim-vlm:2.0.0",
		},
	}
}

// --- Behavior: Name ---

func TestNVIDIASource_Name_IsNvidia(t *testing.T) {
	s := NewNVIDIASource(&fakeNVIDIADiscovery{}, discardLogger(), 10*time.Minute)
	if got := s.Name(); got != "nvidia" {
		t.Errorf("Name() = %q, want %q", got, "nvidia")
	}
}

// --- Behavior: Refresh + List + ID namespacing + translation ---

func TestNVIDIASource_RefreshThenList_ReturnsNamespacedApps(t *testing.T) {
	d := &fakeNVIDIADiscovery{indexResult: sampleNIMEntries()}
	s := NewNVIDIASource(d, discardLogger(), 10*time.Minute)

	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if d.refreshCalls != 1 {
		t.Errorf("expected 1 underlying Discovery.Refresh call, got %d", d.refreshCalls)
	}

	apps, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })

	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}

	llm := apps[0]
	if llm.ID != "nvidia.nim-llm:1.0.0" {
		t.Errorf("LLM ID = %q, want %q", llm.ID, "nvidia.nim-llm:1.0.0")
	}
	if llm.Source != "nvidia" {
		t.Errorf("LLM Source = %q, want %q", llm.Source, "nvidia")
	}
	if llm.Name != "nim-llm" {
		t.Errorf("LLM Name = %q, want %q", llm.Name, "nim-llm")
	}
	if llm.Version != "1.0.0" {
		t.Errorf("LLM Version = %q, want %q", llm.Version, "1.0.0")
	}
	if llm.Publisher != "NVIDIA" {
		t.Errorf("LLM Publisher = %q, want %q", llm.Publisher, "NVIDIA")
	}
	if llm.AssetType != "chart" {
		t.Errorf("LLM AssetType = %q, want %q", llm.AssetType, "chart")
	}
	if len(llm.Categories) != 1 || llm.Categories[0] != "llm" {
		t.Errorf("LLM Categories = %v, want [llm]", llm.Categories)
	}
	if llm.ChartRef.Repo != "oci://registry.suse.com/ai/charts/nvidia" {
		t.Errorf("LLM ChartRef.Repo = %q, want %q",
			llm.ChartRef.Repo, "oci://registry.suse.com/ai/charts/nvidia")
	}
	if llm.ChartRef.Chart != "nim-llm" {
		t.Errorf("LLM ChartRef.Chart = %q, want %q", llm.ChartRef.Chart, "nim-llm")
	}
	if llm.ChartRef.Version != "1.0.0" {
		t.Errorf("LLM ChartRef.Version = %q, want %q", llm.ChartRef.Version, "1.0.0")
	}

	vlm := apps[1]
	if vlm.ID != "nvidia.nim-vlm:2.0.0" {
		t.Errorf("VLM ID = %q, want %q", vlm.ID, "nvidia.nim-vlm:2.0.0")
	}
	if len(vlm.Categories) != 1 || vlm.Categories[0] != "vlm" {
		t.Errorf("VLM Categories = %v, want [vlm]", vlm.Categories)
	}
}

// --- Behavior: List before Refresh returns empty (not error) ---

func TestNVIDIASource_ListBeforeRefresh_ReturnsEmpty(t *testing.T) {
	s := NewNVIDIASource(&fakeNVIDIADiscovery{}, discardLogger(), 10*time.Minute)
	apps, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected empty slice before Refresh, got %d apps", len(apps))
	}
}

// --- Behavior: Refresh failure leaves prior cache intact (stale-but-good) ---

func TestNVIDIASource_RefreshFailure_LeavesPriorCacheIntact(t *testing.T) {
	d := &fakeNVIDIADiscovery{indexResult: sampleNIMEntries()}
	s := NewNVIDIASource(d, discardLogger(), 10*time.Minute)

	// First Refresh succeeds.
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh failed: %v", err)
	}
	apps1, _ := s.List(context.Background())
	if len(apps1) != 2 {
		t.Fatalf("expected 2 apps after first Refresh, got %d", len(apps1))
	}

	// Second Refresh fails at the engine level.
	d.refreshErr = stderrors.New("upstream boom")
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatalf("expected Refresh to return upstream error, got nil")
	}

	// List still returns the prior successful cache.
	apps2, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List after failed Refresh returned error: %v", err)
	}
	if len(apps2) != 2 {
		t.Errorf("expected stale-but-good cache (2 apps) after failed Refresh, got %d",
			len(apps2))
	}
}

// --- Behavior: Refresh failure records LastError; success records LastSuccessAt + EntryCount ---

func TestNVIDIASource_Refresh_UpdatesStatus(t *testing.T) {
	d := &fakeNVIDIADiscovery{indexResult: sampleNIMEntries()}
	s := NewNVIDIASource(d, discardLogger(), 10*time.Minute)

	before := time.Now()
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	st := s.Status()
	if st.LastError != nil {
		t.Errorf("after success, LastError = %v, want nil", st.LastError)
	}
	if st.EntryCount != 2 {
		t.Errorf("after success, EntryCount = %d, want 2", st.EntryCount)
	}
	if !st.LastSuccessAt.After(before) && !st.LastSuccessAt.Equal(before) {
		t.Errorf("LastSuccessAt = %v, expected to be at or after %v", st.LastSuccessAt, before)
	}

	d.refreshErr = stderrors.New("upstream boom")
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatalf("expected error from failing Refresh")
	}
	st = s.Status()
	if st.LastError == nil {
		t.Errorf("after failure, LastError = nil, want non-nil")
	}
	if st.EntryCount != 2 {
		t.Errorf("after failure, EntryCount = %d, want still 2 (cache intact)", st.EntryCount)
	}
}

// --- Behavior: UpdateSettings translates and forwards to underlying Discovery ---

func TestNVIDIASource_UpdateSettings_ForwardsRegistrySliceToEngine(t *testing.T) {
	d := &fakeNVIDIADiscovery{}
	s := NewNVIDIASource(d, discardLogger(), 10*time.Minute)

	s.UpdateSettings(EngineSettings{
		SUSERegistry: RegistrySettings{
			Endpoint: "registry.example.com",
			Username: "alice",
			Token:    "s3cr3t",
		},
		// ApplicationCollection slice intentionally set — should be IGNORED by NVIDIASource.
		ApplicationCollection: AppCollectionSettings{
			APIURL:   "should-be-ignored",
			Username: "ignored-user",
			Token:    "ignored-token",
		},
		RefreshInterval: 5 * time.Minute,
	})

	if len(d.settingsCalls) != 1 {
		t.Fatalf("expected 1 call to underlying Discovery.UpdateSettings, got %d",
			len(d.settingsCalls))
	}
	got := d.settingsCalls[0]
	want := nvidia.EngineSettings{
		RegistryEndpoint: "registry.example.com",
		Username:         "alice",
		Token:            "s3cr3t",
		RefreshInterval:  5 * time.Minute,
	}
	if got != want {
		t.Errorf("forwarded nvidia.EngineSettings = %+v, want %+v", got, want)
	}
}

// --- Compile-time: NVIDIASource implements Source AND Lifecycle ---

var _ Source = (*NVIDIASource)(nil)
var _ Lifecycle = (*NVIDIASource)(nil)

// --- Behavior: Start triggers Refresh on the ticker interval ---

func TestNVIDIASource_Start_TickerTriggersRefresh(t *testing.T) {
	d := &fakeNVIDIADiscovery{indexResult: sampleNIMEntries()}
	s := NewNVIDIASource(d, discardLogger(), 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// Wait long enough for the immediate Refresh + at least 2 ticks.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		calls := d.refreshCalls
		d.mu.Unlock()
		if calls >= 3 { // initial + 2 ticks
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	d.mu.Lock()
	final := d.refreshCalls
	d.mu.Unlock()
	if final < 3 {
		t.Errorf("expected ≥3 underlying Refresh calls (initial + ≥2 ticks) within 200ms; got %d",
			final)
	}

	// After cancel, give the goroutine a moment to exit, then verify the
	// count stops growing.
	time.Sleep(20 * time.Millisecond)
	d.mu.Lock()
	afterCancel := d.refreshCalls
	d.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	d.mu.Lock()
	stillAfter := d.refreshCalls
	d.mu.Unlock()
	if stillAfter > afterCancel {
		t.Errorf("ticker continued after ctx cancel: %d → %d", afterCancel, stillAfter)
	}
}
