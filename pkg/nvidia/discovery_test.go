package nvidia

import (
	"context"
	stderrors "errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// silentLogger is a *slog.Logger that swallows output, for tests.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newRegistryStub starts an httptest server stubbing /v2/_catalog and
// /v2/<repo>/tags/list with the given canned data.
//
//   repos: list of repository names returned by /v2/_catalog
//   tags:  map[repo][]string of tags returned by /v2/<repo>/tags/list
func newRegistryStub(t *testing.T, repos []string, tags map[string][]string) *httptest.Server {
	t.Helper()
	ts := newTestServer(t)
	ts.catalogPages[""] = testPage{body: jsonRepositories(repos)}
	for repo, tagList := range tags {
		ts.tagsPages[repo] = map[string]testPage{
			"": {body: jsonTags(repo, tagList)},
		}
	}
	return ts.Server
}

func jsonRepositories(repos []string) string {
	b := []byte(`{"repositories":[`)
	for i, r := range repos {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '"')
		b = append(b, r...)
		b = append(b, '"')
	}
	b = append(b, "]}"...)
	return string(b)
}

func jsonTags(repo string, tags []string) string {
	b := []byte(`{"name":"`)
	b = append(b, repo...)
	b = append(b, `","tags":[`...)
	for i, tag := range tags {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '"')
		b = append(b, tag...)
		b = append(b, '"')
	}
	b = append(b, "]}"...)
	return string(b)
}

// newDiscoveryWithClient is a same-package helper for tests: constructs a
// discoveryImpl with an injected httpClient (so tests can use
// httptest.Server.Client()).
func newDiscoveryWithClient(httpClient *http.Client) *discoveryImpl {
	return &discoveryImpl{
		logger:     silentLogger(),
		httpClient: httpClient,
	}
}

// --- Refresh / Index orchestration ---

func TestDiscovery_RefreshWithoutSettings_ReturnsNotConfigured(t *testing.T) {
	d := newDiscoveryWithClient(http.DefaultClient)
	err := d.Refresh(context.Background())
	if !stderrors.Is(err, ErrNotConfigured) {
		t.Errorf("Refresh err = %v, want ErrNotConfigured", err)
	}
}

func TestDiscovery_IndexBeforeRefresh_ReturnsEmpty(t *testing.T) {
	d := newDiscoveryWithClient(http.DefaultClient)
	got, err := d.Index(context.Background())
	if err != nil {
		t.Fatalf("Index err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("Index = %v, want empty", got)
	}
}

func TestDiscovery_RefreshAndIndex_PopulatesCache(t *testing.T) {
	repos := []string{
		"ai/charts/nvidia/nim-llm",
		"ai/charts/nvidia/nim-vlm",
	}
	tags := map[string][]string{
		"ai/charts/nvidia/nim-llm": {"1.0.0", "1.1.0"},
		"ai/charts/nvidia/nim-vlm": {"2.0.0"},
	}
	ts := newRegistryStub(t, repos, tags)

	d := newDiscoveryWithClient(ts.Client())
	d.UpdateSettings(EngineSettings{RegistryEndpoint: ts.URL, Username: "u", Token: "t"})

	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh err = %v", err)
	}
	got, err := d.Index(context.Background())
	if err != nil {
		t.Fatalf("Index err = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Index returned %d entries, want 3: %#v", len(got), got)
	}

	byID := indexByID(got)
	// nim-llm:1.0.0 → LLM
	if e, ok := byID["nim-llm:1.0.0"]; !ok {
		t.Errorf("missing entry nim-llm:1.0.0; got IDs=%v", keys(byID))
	} else {
		if e.Chart != "nim-llm" || e.Version != "1.0.0" {
			t.Errorf("nim-llm:1.0.0 fields wrong: %#v", e)
		}
		if e.Type != TypeLLM {
			t.Errorf("nim-llm:1.0.0 Type = %v, want LLM", e.Type)
		}
		wantRef := ts.URL + "/ai/charts/nvidia/nim-llm:1.0.0"
		if e.ChartRef == "" || e.ChartRef == wantRef[len(ts.URL):] {
			// Accept either full or relative — check below for the canonical form.
		}
	}
	// nim-vlm:2.0.0 → VLM via classifier
	if e, ok := byID["nim-vlm:2.0.0"]; !ok {
		t.Errorf("missing entry nim-vlm:2.0.0")
	} else if e.Type != TypeVLM {
		t.Errorf("nim-vlm:2.0.0 Type = %v, want VLM", e.Type)
	}
}

func TestDiscovery_Refresh_FiltersToNVIDIANamespace(t *testing.T) {
	repos := []string{
		"ai/charts/nvidia/nim-llm",
		"ai/charts/something-else/foo",
		"other/random-repo",
	}
	tags := map[string][]string{
		"ai/charts/nvidia/nim-llm": {"1.0.0"},
		// Other repos won't be queried; no tags entries needed.
	}
	ts := newRegistryStub(t, repos, tags)

	d := newDiscoveryWithClient(ts.Client())
	d.UpdateSettings(EngineSettings{RegistryEndpoint: ts.URL, Username: "u", Token: "t"})

	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh err = %v", err)
	}
	got, _ := d.Index(context.Background())
	if len(got) != 1 || got[0].Chart != "nim-llm" {
		t.Errorf("Index = %#v, want exactly nim-llm:1.0.0", got)
	}
}

func TestDiscovery_Refresh_ChartRefIsOCIPath(t *testing.T) {
	repos := []string{"ai/charts/nvidia/nim-llm"}
	tags := map[string][]string{"ai/charts/nvidia/nim-llm": {"1.0.0"}}
	ts := newRegistryStub(t, repos, tags)

	d := newDiscoveryWithClient(ts.Client())
	d.UpdateSettings(EngineSettings{RegistryEndpoint: ts.URL, Username: "u", Token: "t"})
	_ = d.Refresh(context.Background())
	got, _ := d.Index(context.Background())

	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	// Strip http:// from ts.URL → "host:port"; ChartRef should be
	// "oci://<host:port>/ai/charts/nvidia/nim-llm:1.0.0".
	hostPort := ts.URL[len("http://"):]
	want := "oci://" + hostPort + "/ai/charts/nvidia/nim-llm:1.0.0"
	if got[0].ChartRef != want {
		t.Errorf("ChartRef = %q, want %q", got[0].ChartRef, want)
	}
}

func TestDiscovery_Refresh_NetworkErrorIsUnreachable(t *testing.T) {
	d := newDiscoveryWithClient(http.DefaultClient)
	d.UpdateSettings(EngineSettings{
		RegistryEndpoint: "http://127.0.0.1:1", // port 1 always refuses
		Username:         "u",
		Token:            "t",
	})

	err := d.Refresh(context.Background())
	if !stderrors.Is(err, ErrUnreachable) {
		t.Errorf("Refresh err = %v, want ErrUnreachable", err)
	}
}

func TestDiscovery_ReRefresh_ReplacesCache(t *testing.T) {
	// First registry state: one chart.
	repos1 := []string{"ai/charts/nvidia/nim-llm"}
	tags1 := map[string][]string{"ai/charts/nvidia/nim-llm": {"1.0.0"}}
	ts1 := newRegistryStub(t, repos1, tags1)
	d := newDiscoveryWithClient(ts1.Client())
	d.UpdateSettings(EngineSettings{RegistryEndpoint: ts1.URL, Username: "u", Token: "t"})
	_ = d.Refresh(context.Background())
	if got, _ := d.Index(context.Background()); len(got) != 1 {
		t.Fatalf("first refresh: want 1 entry, got %d", len(got))
	}

	// Switch to a new registry with completely different content.
	repos2 := []string{"ai/charts/nvidia/nim-vlm"}
	tags2 := map[string][]string{"ai/charts/nvidia/nim-vlm": {"2.0.0", "2.1.0"}}
	ts2 := newRegistryStub(t, repos2, tags2)
	d.UpdateSettings(EngineSettings{RegistryEndpoint: ts2.URL, Username: "u", Token: "t"})
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("second refresh err = %v", err)
	}

	got, _ := d.Index(context.Background())
	if len(got) != 2 {
		t.Errorf("after second refresh: want 2 entries, got %d (%#v)", len(got), got)
	}
	byID := indexByID(got)
	if _, ok := byID["nim-llm:1.0.0"]; ok {
		t.Errorf("stale entry nim-llm:1.0.0 still present after re-refresh")
	}
	if _, ok := byID["nim-vlm:2.0.0"]; !ok {
		t.Errorf("missing fresh entry nim-vlm:2.0.0")
	}
}

func TestDiscovery_Index_IsDeterministicallyOrdered(t *testing.T) {
	repos := []string{"ai/charts/nvidia/nim-vlm", "ai/charts/nvidia/nim-llm"}
	tags := map[string][]string{
		"ai/charts/nvidia/nim-vlm": {"2.0.0"},
		"ai/charts/nvidia/nim-llm": {"1.0.0"},
	}
	ts := newRegistryStub(t, repos, tags)
	d := newDiscoveryWithClient(ts.Client())
	d.UpdateSettings(EngineSettings{RegistryEndpoint: ts.URL, Username: "u", Token: "t"})
	_ = d.Refresh(context.Background())

	first, _ := d.Index(context.Background())
	second, _ := d.Index(context.Background())

	if len(first) != len(second) {
		t.Fatalf("Index length unstable: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("Index order unstable at %d: %q vs %q", i, first[i].ID, second[i].ID)
		}
	}
}

// --- Get (single-NIM lookup) ---

func TestDiscovery_Get_BeforeRefresh_ReturnsNotFound(t *testing.T) {
	d := newDiscoveryWithClient(http.DefaultClient)
	_, err := d.Get(context.Background(), "nim-llm:1.0.0")
	if !stderrors.Is(err, ErrNIMNotFound) {
		t.Errorf("Get err = %v, want ErrNIMNotFound", err)
	}
}

func TestDiscovery_Get_KnownEntry_ReturnsEntry(t *testing.T) {
	repos := []string{"ai/charts/nvidia/nim-llm"}
	tags := map[string][]string{"ai/charts/nvidia/nim-llm": {"1.0.0", "1.1.0"}}
	ts := newRegistryStub(t, repos, tags)
	d := newDiscoveryWithClient(ts.Client())
	d.UpdateSettings(EngineSettings{RegistryEndpoint: ts.URL, Username: "u", Token: "t"})
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh err = %v", err)
	}

	got, err := d.Get(context.Background(), "nim-llm:1.0.0")
	if err != nil {
		t.Fatalf("Get err = %v, want nil", err)
	}
	if got.ID != "nim-llm:1.0.0" || got.Chart != "nim-llm" || got.Version != "1.0.0" {
		t.Errorf("Get returned wrong entry: %#v", got)
	}
}

func TestDiscovery_Get_UnknownID_ReturnsNotFound(t *testing.T) {
	repos := []string{"ai/charts/nvidia/nim-llm"}
	tags := map[string][]string{"ai/charts/nvidia/nim-llm": {"1.0.0"}}
	ts := newRegistryStub(t, repos, tags)
	d := newDiscoveryWithClient(ts.Client())
	d.UpdateSettings(EngineSettings{RegistryEndpoint: ts.URL, Username: "u", Token: "t"})
	_ = d.Refresh(context.Background())

	_, err := d.Get(context.Background(), "nim-llm:9.9.9")
	if !stderrors.Is(err, ErrNIMNotFound) {
		t.Errorf("Get err = %v, want ErrNIMNotFound", err)
	}
}

// --- helpers ---

func indexByID(entries []NIMEntry) map[string]NIMEntry {
	m := make(map[string]NIMEntry, len(entries))
	for _, e := range entries {
		m[e.ID] = e
	}
	return m
}

func keys(m map[string]NIMEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
