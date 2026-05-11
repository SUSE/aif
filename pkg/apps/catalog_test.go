package apps

import (
	"context"
	stderrors "errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSource is a Source-only test double (does NOT implement Lifecycle).
// Useful for catalog tests that don't exercise Start.
type fakeSource struct {
	name          string
	apps          []App
	listErr       error
	refreshErr    error
	refreshCalls  int32
	settingsCalls []EngineSettings
	mu            sync.Mutex
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) List(_ context.Context) ([]App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]App, len(f.apps))
	copy(out, f.apps)
	return out, nil
}

func (f *fakeSource) Refresh(_ context.Context) error {
	atomic.AddInt32(&f.refreshCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshErr
}

func (f *fakeSource) UpdateSettings(s EngineSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settingsCalls = append(f.settingsCalls, s)
}

// fakeLifecycleSource adds Start; implements both Source AND Lifecycle.
type fakeLifecycleSource struct {
	fakeSource
	startCalls int32
}

func (f *fakeLifecycleSource) Start(_ context.Context) {
	atomic.AddInt32(&f.startCalls, 1)
}

// --- AddSource ---

func TestCatalog_AddSource_Appends(t *testing.T) {
	c := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	c.AddSource(&fakeSource{name: "a"})
	c.AddSource(&fakeSource{name: "b"})
	if len(c.sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(c.sources))
	}
}

// --- List ---

func TestCatalog_List_NoSources_ReturnsEmpty(t *testing.T) {
	c := New(discardLogger(), 10*time.Minute)
	apps, err := c.List(context.Background(), ListOpts{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected empty list with no sources, got %d apps", len(apps))
	}
}

func TestCatalog_List_TwoSources_ConcatenatedAndSortedByID(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	cat.AddSource(&fakeSource{
		name: "nvidia",
		apps: []App{
			{ID: "nvidia.nim-llm:1.0.0", Source: "nvidia"},
			{ID: "nvidia.nim-vlm:2.0.0", Source: "nvidia"},
		},
	})
	cat.AddSource(&fakeSource{
		name: "suse",
		apps: []App{
			{ID: "suse.ollama:0.4.1", Source: "suse"},
			{ID: "suse.milvus:2.4.0", Source: "suse"},
		},
	})

	got, err := cat.List(context.Background(), ListOpts{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	wantIDs := []string{
		"nvidia.nim-llm:1.0.0",
		"nvidia.nim-vlm:2.0.0",
		"suse.milvus:2.4.0",
		"suse.ollama:0.4.1",
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("expected %d apps, got %d", len(wantIDs), len(got))
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("apps[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}
}

func TestCatalog_List_DedupesByID_FirstSourceWins(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	cat.AddSource(&fakeSource{
		name: "first",
		apps: []App{{ID: "shared/foo:1.0", Source: "first", Publisher: "FIRST"}},
	})
	cat.AddSource(&fakeSource{
		name: "second",
		apps: []App{{ID: "shared/foo:1.0", Source: "second", Publisher: "SECOND"}},
	})

	got, err := cat.List(context.Background(), ListOpts{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected dedup to 1 app, got %d", len(got))
	}
	if got[0].Publisher != "FIRST" {
		t.Errorf("expected first registered source to win, got Publisher=%q", got[0].Publisher)
	}
}

func TestCatalog_List_FiltersBySource(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	cat.AddSource(&fakeSource{
		name: "nvidia",
		apps: []App{{ID: "nvidia.a:1", Source: "nvidia"}, {ID: "nvidia.b:1", Source: "nvidia"}},
	})
	cat.AddSource(&fakeSource{
		name: "suse",
		apps: []App{{ID: "suse.c:1", Source: "suse"}},
	})

	got, err := cat.List(context.Background(), ListOpts{Source: "nvidia"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 nvidia apps, got %d", len(got))
	}
	for _, a := range got {
		if a.Source != "nvidia" {
			t.Errorf("got non-nvidia App in filtered result: %+v", a)
		}
	}
}

func TestCatalog_List_FiltersByCategory_ExactMatch(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	cat.AddSource(&fakeSource{
		name: "nvidia",
		apps: []App{
			{ID: "nvidia.a:1", Source: "nvidia", Categories: []string{"llm"}},
			{ID: "nvidia.b:1", Source: "nvidia", Categories: []string{"vlm"}},
			{ID: "nvidia.c:1", Source: "nvidia", Categories: []string{"llm", "embedding"}},
		},
	})

	got, err := cat.List(context.Background(), ListOpts{Category: "llm"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 llm apps, got %d", len(got))
	}
	for _, a := range got {
		hasLLM := false
		for _, c := range a.Categories {
			if c == "llm" {
				hasLLM = true
				break
			}
		}
		if !hasLLM {
			t.Errorf("got App without 'llm' category in filtered result: %+v", a)
		}
	}
}

// --- Get ---

func TestCatalog_Get_DispatchesByNamespacePrefix(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	nvSrc := &fakeSource{
		name: "nvidia",
		apps: []App{{ID: "nvidia.nim-llm:1.0.0", Source: "nvidia", Name: "nim-llm"}},
	}
	suseSrc := &fakeSource{
		name: "suse",
		apps: []App{{ID: "suse.ollama:0.4.1", Source: "suse", Name: "ollama"}},
	}
	cat.AddSource(nvSrc)
	cat.AddSource(suseSrc)

	got, err := cat.Get(context.Background(), "nvidia.nim-llm:1.0.0")
	if err != nil {
		t.Fatalf("Get(nvidia.nim-llm:...): %v", err)
	}
	if got.Name != "nim-llm" {
		t.Errorf("Get(nvidia.nim-llm:1.0.0).Name = %q, want %q", got.Name, "nim-llm")
	}

	got, err = cat.Get(context.Background(), "suse.ollama:0.4.1")
	if err != nil {
		t.Fatalf("Get(suse.ollama:...): %v", err)
	}
	if got.Name != "ollama" {
		t.Errorf("Get(suse.ollama:0.4.1).Name = %q, want %q", got.Name, "ollama")
	}
}

func TestCatalog_Get_UnknownPrefix_ReturnsErrUnknownSource(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	cat.AddSource(&fakeSource{name: "nvidia"})

	_, err := cat.Get(context.Background(), "mystery/whatever:1.0")
	if !stderrors.Is(err, ErrUnknownSource) {
		t.Errorf("Get with unknown prefix err = %v, want ErrUnknownSource", err)
	}
}

func TestCatalog_Get_MissingApp_ReturnsErrAppNotFound(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	cat.AddSource(&fakeSource{
		name: "nvidia",
		apps: []App{{ID: "nvidia.nim-llm:1.0.0"}},
	})

	_, err := cat.Get(context.Background(), "nvidia.does-not-exist:9.9.9")
	if !stderrors.Is(err, ErrAppNotFound) {
		t.Errorf("Get with missing id err = %v, want ErrAppNotFound", err)
	}
}

func TestCatalog_Get_MalformedID_ReturnsErrUnknownSource(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	cat.AddSource(&fakeSource{name: "nvidia"})

	_, err := cat.Get(context.Background(), "no-slash-here")
	if !stderrors.Is(err, ErrUnknownSource) {
		t.Errorf("Get with malformed id err = %v, want ErrUnknownSource", err)
	}
}

// --- Refresh ---

func TestCatalog_Refresh_FansOutToAllSources(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	a := &fakeSource{name: "a"}
	b := &fakeSource{name: "b"}
	cat.AddSource(a)
	cat.AddSource(b)

	if err := cat.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if atomic.LoadInt32(&a.refreshCalls) != 1 {
		t.Errorf("source a Refresh calls = %d, want 1", a.refreshCalls)
	}
	if atomic.LoadInt32(&b.refreshCalls) != 1 {
		t.Errorf("source b Refresh calls = %d, want 1", b.refreshCalls)
	}
}

func TestCatalog_Refresh_PartialFailure_ReturnsNil(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	cat.AddSource(&fakeSource{name: "ok"})
	cat.AddSource(&fakeSource{name: "broken", refreshErr: stderrors.New("boom")})

	err := cat.Refresh(context.Background())
	if err != nil {
		t.Errorf("partial failure should return nil; got %v", err)
	}
}

func TestCatalog_Refresh_AllFailed_ReturnsError(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	cat.AddSource(&fakeSource{name: "x", refreshErr: stderrors.New("x-boom")})
	cat.AddSource(&fakeSource{name: "y", refreshErr: stderrors.New("y-boom")})

	err := cat.Refresh(context.Background())
	if err == nil {
		t.Error("expected error when ALL sources fail; got nil")
	}
}

// --- UpdateSettings ---

func TestCatalog_UpdateSettings_FansOutToAllSources(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	a := &fakeSource{name: "a"}
	b := &fakeSource{name: "b"}
	cat.AddSource(a)
	cat.AddSource(b)

	want := EngineSettings{RefreshInterval: 7 * time.Minute}
	cat.UpdateSettings(want)

	if len(a.settingsCalls) != 1 || a.settingsCalls[0] != want {
		t.Errorf("source a settings calls = %v, want exactly one call with %v",
			a.settingsCalls, want)
	}
	if len(b.settingsCalls) != 1 || b.settingsCalls[0] != want {
		t.Errorf("source b settings calls = %v, want exactly one call with %v",
			b.settingsCalls, want)
	}
}

// --- Start (Lifecycle) ---

func TestCatalog_Start_OnlyCallsLifecycleSources(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	plain := &fakeSource{name: "plain"}                                // Source only
	withLC := &fakeLifecycleSource{fakeSource: fakeSource{name: "lc"}} // Source + Lifecycle
	cat.AddSource(plain)
	cat.AddSource(withLC)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cat.Start(ctx)

	if got := atomic.LoadInt32(&withLC.startCalls); got != 1 {
		t.Errorf("Lifecycle source Start calls = %d, want 1", got)
	}
	// plain has no Start method — type assertion in Catalog.Start must skip it.
	// (Compile-time: fakeSource does NOT have a Start method, so it can't be
	// asserted to Lifecycle. This is the test that the type-assertion path works.)
}

// Compile-time guard: fakeSource must NOT satisfy Lifecycle (otherwise the
// "skipped by type assertion" branch in Catalog.Start is untested).
func TestCatalog_Start_FakeSourceIsNotLifecycle(t *testing.T) {
	var s any = &fakeSource{}
	if _, ok := s.(Lifecycle); ok {
		t.Fatal("fakeSource accidentally satisfies Lifecycle — test guard broken")
	}
}

// --- Sort verification: List output is stable regardless of source registration order ---

func TestCatalog_List_StableSortByID(t *testing.T) {
	cat := New(discardLogger(), 10*time.Minute).(*catalogImpl)
	// Register suse first, then nvidia. Output should still be sorted by ID
	// (so nvidia.* comes before suse.* alphabetically).
	cat.AddSource(&fakeSource{
		name: "suse",
		apps: []App{{ID: "suse.zzz:1", Source: "suse"}},
	})
	cat.AddSource(&fakeSource{
		name: "nvidia",
		apps: []App{{ID: "nvidia.aaa:1", Source: "nvidia"}},
	})

	got, _ := cat.List(context.Background(), ListOpts{})
	if len(got) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(got))
	}
	if got[0].ID != "nvidia.aaa:1" || got[1].ID != "suse.zzz:1" {
		t.Errorf("sort order = [%s, %s], want [nvidia.aaa:1, suse.zzz:1]",
			got[0].ID, got[1].ID)
	}
	// Sanity: confirm sort.SliceIsSorted reports it as sorted.
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].ID < got[j].ID }) {
		t.Error("List output is not sorted by ID")
	}
}
