package apps

import (
	"context"
	stderrors "errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/SUSE/aif/pkg/source_collection"
)

// fakeAppCoClient is an in-memory test double for source_collection.Client.
type fakeAppCoClient struct {
	mu sync.Mutex

	listResult     []source_collection.CatalogApp
	listErr        error
	settingsCalls  []source_collection.EngineSettings
	listCalls      int
}

func (f *fakeAppCoClient) List(_ context.Context) ([]source_collection.CatalogApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]source_collection.CatalogApp, len(f.listResult))
	copy(out, f.listResult)
	return out, nil
}

func (f *fakeAppCoClient) GetChart(_ context.Context, _, _, _ string) (*source_collection.ChartMetadata, error) {
	return nil, nil
}

func (f *fakeAppCoClient) UpdateSettings(s source_collection.EngineSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settingsCalls = append(f.settingsCalls, s)
}

func sampleCatalogApps() []source_collection.CatalogApp {
	return []source_collection.CatalogApp{
		{
			ID:            "ollama",
			DisplayName:   "Ollama",
			Description:   "Local LLM runtime",
			Publisher:     "Ollama Inc",
			Categories:    []string{"AI", "Inference"},
			ChartRef:      "oci://dp.apps.rancher.io/charts/ollama:0.4.1",
			LatestVersion: "0.4.1",
			Source:        "api",
		},
		{
			ID:            "milvus",
			DisplayName:   "Milvus",
			Description:   "Vector DB",
			Publisher:     "Zilliz",
			Categories:    []string{"AI", "Vector DB"},
			ChartRef:      "oci://dp.apps.rancher.io/charts/milvus:2.4.0",
			LatestVersion: "2.4.0",
			Source:        "api",
		},
	}
}

// --- Behavior: Name ---

func TestAppCoSource_Name_IsSuse(t *testing.T) {
	s := NewAppCoSource(&fakeAppCoClient{}, discardLogger(), 10*time.Minute)
	if got := s.Name(); got != "suse" {
		t.Errorf("Name() = %q, want %q", got, "suse")
	}
}

// --- Behavior: Refresh + List + ID namespacing + translation ---

func TestAppCoSource_RefreshThenList_ReturnsNamespacedApps(t *testing.T) {
	c := &fakeAppCoClient{listResult: sampleCatalogApps()}
	s := NewAppCoSource(c, discardLogger(), 10*time.Minute)

	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if c.listCalls != 1 {
		t.Errorf("expected 1 underlying Client.List call, got %d", c.listCalls)
	}

	apps, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })

	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}

	milvus := apps[0]
	if milvus.ID != "suse/milvus:2.4.0" {
		t.Errorf("Milvus ID = %q, want %q", milvus.ID, "suse/milvus:2.4.0")
	}
	if milvus.Source != "suse" {
		t.Errorf("Milvus Source = %q, want %q", milvus.Source, "suse")
	}
	if milvus.Name != "milvus" {
		t.Errorf("Milvus Name = %q, want %q", milvus.Name, "milvus")
	}
	if milvus.DisplayName != "Milvus" {
		t.Errorf("Milvus DisplayName = %q, want %q", milvus.DisplayName, "Milvus")
	}
	if milvus.Publisher != "Zilliz" {
		t.Errorf("Milvus Publisher = %q, want %q", milvus.Publisher, "Zilliz")
	}
	if milvus.Description != "Vector DB" {
		t.Errorf("Milvus Description = %q, want %q", milvus.Description, "Vector DB")
	}
	if milvus.Version != "2.4.0" {
		t.Errorf("Milvus Version = %q, want %q", milvus.Version, "2.4.0")
	}
	if milvus.AssetType != "chart" {
		t.Errorf("Milvus AssetType = %q, want %q", milvus.AssetType, "chart")
	}
	wantCats := []string{"AI", "Vector DB"}
	if len(milvus.Categories) != 2 || milvus.Categories[0] != wantCats[0] || milvus.Categories[1] != wantCats[1] {
		t.Errorf("Milvus Categories = %v, want %v", milvus.Categories, wantCats)
	}
	if milvus.ChartRef.Repo != "oci://dp.apps.rancher.io/charts" {
		t.Errorf("Milvus ChartRef.Repo = %q, want %q",
			milvus.ChartRef.Repo, "oci://dp.apps.rancher.io/charts")
	}
	if milvus.ChartRef.Chart != "milvus" {
		t.Errorf("Milvus ChartRef.Chart = %q, want %q", milvus.ChartRef.Chart, "milvus")
	}
	if milvus.ChartRef.Version != "2.4.0" {
		t.Errorf("Milvus ChartRef.Version = %q, want %q", milvus.ChartRef.Version, "2.4.0")
	}

	ollama := apps[1]
	if ollama.ID != "suse/ollama:0.4.1" {
		t.Errorf("Ollama ID = %q, want %q", ollama.ID, "suse/ollama:0.4.1")
	}
}

// --- Behavior: List before Refresh returns empty (not error) ---

func TestAppCoSource_ListBeforeRefresh_ReturnsEmpty(t *testing.T) {
	s := NewAppCoSource(&fakeAppCoClient{}, discardLogger(), 10*time.Minute)
	apps, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected empty slice before Refresh, got %d apps", len(apps))
	}
}

// --- Behavior: Refresh failure leaves prior cache intact (stale-but-good) ---

func TestAppCoSource_RefreshFailure_LeavesPriorCacheIntact(t *testing.T) {
	c := &fakeAppCoClient{listResult: sampleCatalogApps()}
	s := NewAppCoSource(c, discardLogger(), 10*time.Minute)

	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh failed: %v", err)
	}
	apps1, _ := s.List(context.Background())
	if len(apps1) != 2 {
		t.Fatalf("expected 2 apps after first Refresh, got %d", len(apps1))
	}

	c.listErr = stderrors.New("upstream boom")
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatalf("expected Refresh to surface upstream error, got nil")
	}

	apps2, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List after failed Refresh returned error: %v", err)
	}
	if len(apps2) != 2 {
		t.Errorf("expected stale-but-good cache (2 apps) after failed Refresh, got %d",
			len(apps2))
	}
}

// --- Behavior: Refresh updates SourceStatus on success and failure ---

func TestAppCoSource_Refresh_UpdatesStatus(t *testing.T) {
	c := &fakeAppCoClient{listResult: sampleCatalogApps()}
	s := NewAppCoSource(c, discardLogger(), 10*time.Minute)

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
		t.Errorf("LastSuccessAt = %v, expected at or after %v", st.LastSuccessAt, before)
	}

	c.listErr = stderrors.New("upstream boom")
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

// --- Behavior: UpdateSettings translates and forwards to underlying Client ---

func TestAppCoSource_UpdateSettings_ForwardsAppCoSliceToEngine(t *testing.T) {
	c := &fakeAppCoClient{}
	s := NewAppCoSource(c, discardLogger(), 10*time.Minute)

	s.UpdateSettings(EngineSettings{
		// SUSERegistry slice intentionally set — should be IGNORED by AppCoSource.
		SUSERegistry: RegistrySettings{
			Endpoint: "should-be-ignored",
			Username: "ignored-user",
			Token:    "ignored-token",
		},
		ApplicationCollection: AppCollectionSettings{
			APIURL:   "https://api.example.com",
			OCIHost:  "oci.example.com",
			Username: "alice",
			Token:    "s3cr3t",
		},
		RefreshInterval: 5 * time.Minute,
	})

	if len(c.settingsCalls) != 1 {
		t.Fatalf("expected 1 call to underlying Client.UpdateSettings, got %d",
			len(c.settingsCalls))
	}
	got := c.settingsCalls[0]
	want := source_collection.EngineSettings{
		APIURL:   "https://api.example.com",
		OCIHost:  "oci.example.com",
		Username: "alice",
		Token:    "s3cr3t",
	}
	if got != want {
		t.Errorf("forwarded source_collection.EngineSettings = %+v, want %+v", got, want)
	}
}

// --- Compile-time: AppCoSource implements Source AND Lifecycle ---

var _ Source = (*AppCoSource)(nil)
var _ Lifecycle = (*AppCoSource)(nil)
