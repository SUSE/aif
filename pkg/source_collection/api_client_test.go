package source_collection

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// testApp is a minimal fixture builder that produces a (list item,
// detail) pair for a given slug. Tests register pairs with newTestServer.
type testApp struct {
	list   apiListItem
	detail apiAppDetail
}

func newTestApp(slug, name, version string, categories ...string) testApp {
	labels := make([]string, 0, len(categories)+1)
	for _, c := range categories {
		labels = append(labels, "category:"+c)
	}
	labels = append(labels, "license:Apache-2.0")
	return testApp{
		list: apiListItem{
			SlugName:        slug,
			Name:            name,
			Description:     "Description of " + name,
			ProjectURL:      "https://example.com/" + slug,
			LogoURL:         "/logos/" + slug + ".png",
			LastUpdatedAt:   "2026-04-30T23:56:07.607227Z",
			PackagingFormat: "HELM_CHART",
		},
		detail: apiAppDetail{
			SlugName: slug,
			Labels:   labels,
			Branches: []apiBranch{
				{ID: 1, BranchName: "0", BranchPattern: "^0\\.\\d+\\.\\d+$", Baseline: version, IsLTS: false},
			},
		},
	}
}

// newTestServer routes /v1/applications to a paginated list and
// /v1/applications/{slug} to the matching detail. Apps are split across
// pages of `pageSize` items in registration order.
func newTestServer(t *testing.T, pageSize int, apps ...testApp) *httptest.Server {
	t.Helper()
	bySlug := make(map[string]apiAppDetail, len(apps))
	for _, a := range apps {
		bySlug[a.list.SlugName] = a.detail
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applications", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page_number"))
		if page < 1 {
			page = 1
		}
		size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		if size <= 0 {
			size = pageSize
		}
		start := (page - 1) * pageSize
		if start > len(apps) {
			start = len(apps)
		}
		end := start + pageSize
		if end > len(apps) {
			end = len(apps)
		}
		items := make([]apiListItem, 0, end-start)
		for _, a := range apps[start:end] {
			items = append(items, a.list)
		}
		total := len(apps)
		totalPages := (total + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiListResponse{
			Items:      items,
			Page:       page,
			PageSize:   size,
			TotalSize:  total,
			TotalPages: totalPages,
		})
	})
	mux.HandleFunc("/v1/applications/", func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimPrefix(r.URL.Path, "/v1/applications/")
		d, ok := bySlug[slug]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(d)
	})
	return httptest.NewServer(mux)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ── Construction & configuration ──────────────────────────────────────

func TestNewClient(t *testing.T) {
	c, annReader := NewClient(discardLogger())
	if c == nil {
		t.Fatal("expected non-nil Client")
	}
	if annReader == nil {
		t.Fatal("expected non-nil AnnotationReader")
	}
}

func TestList_NotConfigured(t *testing.T) {
	c, _ := NewClient(discardLogger())
	_, err := c.List(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got: %v", err)
	}
}

func TestGetChart_NotConfigured(t *testing.T) {
	c, _ := NewClient(discardLogger())
	_, err := c.GetChart(context.Background(), "", "ollama", "0.4.1")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got: %v", err)
	}
}

func TestFakeClient_ImplementsClient(t *testing.T) {
	var _ Client = &FakeClient{}
}

func TestUpdateSettings(t *testing.T) {
	c, _ := NewClient(discardLogger())
	apiC := c.(*apiClient)

	s := EngineSettings{
		APIURL:   "https://custom.example.com",
		OCIHost:  "oci.example.com",
		Username: "user",
		Token:    "tok",
	}
	apiC.UpdateSettings(s)

	apiC.mu.RLock()
	defer apiC.mu.RUnlock()
	if apiC.settings.APIURL != "https://custom.example.com" {
		t.Errorf("APIURL = %q", apiC.settings.APIURL)
	}
	if apiC.settings.Username != "user" {
		t.Errorf("Username = %q", apiC.settings.Username)
	}
	if apiC.settings.Token != "tok" {
		t.Errorf("Token = %q", apiC.settings.Token)
	}
}

// ── List happy paths ─────────────────────────────────────────────────

func TestList_SinglePage_PopulatesAllFields(t *testing.T) {
	srv := newTestServer(t, 100,
		newTestApp("ollama", "Ollama", "0.4.1", "AI", "Inference"),
		newTestApp("vllm", "vLLM", "0.6.0", "AI"),
		newTestApp("milvus", "Milvus", "2.4.0", "Vector DB"),
	)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{
		APIURL:   srv.URL,
		OCIHost:  "dp.apps.rancher.io",
		Username: "testuser",
		Token:    "testtoken",
	})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(apps))
	}

	bySlug := make(map[string]CatalogApp, len(apps))
	for _, a := range apps {
		bySlug[a.ID] = a
	}

	ollama, ok := bySlug["ollama"]
	if !ok {
		t.Fatal("ollama missing from result")
	}
	if ollama.DisplayName != "Ollama" {
		t.Errorf("DisplayName = %q, want Ollama", ollama.DisplayName)
	}
	if ollama.LatestVersion != "0.4.1" {
		t.Errorf("LatestVersion = %q, want 0.4.1", ollama.LatestVersion)
	}
	if ollama.ChartRef != "oci://dp.apps.rancher.io/charts/ollama:0.4.1" {
		t.Errorf("ChartRef = %q", ollama.ChartRef)
	}
	if len(ollama.Categories) != 2 || ollama.Categories[0] != "AI" || ollama.Categories[1] != "Inference" {
		t.Errorf("Categories = %v, want [AI Inference]", ollama.Categories)
	}
	if ollama.Source != "api" {
		t.Errorf("Source = %q, want api", ollama.Source)
	}
	if ollama.LastUpdatedAt != "2026-04-30T23:56:07.607227Z" {
		t.Errorf("LastUpdatedAt = %q", ollama.LastUpdatedAt)
	}
	if ollama.ProjectURL != "https://example.com/ollama" {
		t.Errorf("ProjectURL = %q", ollama.ProjectURL)
	}
	if ollama.LogoURL != srv.URL+"/logos/ollama.png" {
		t.Errorf("LogoURL = %q, want %q", ollama.LogoURL, srv.URL+"/logos/ollama.png")
	}
}

func TestList_SendsBasicAuthAndPackagingFilter(t *testing.T) {
	gotAuth := false
	gotFilter := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applications", func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if ok && u == "testuser" && p == "testtoken" {
			gotAuth = true
		}
		gotFilter = r.URL.Query().Get("packaging_formats")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiListResponse{Items: nil, TotalPages: 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL, Username: "testuser", Token: "testtoken"})
	_, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if !gotAuth {
		t.Error("expected BasicAuth testuser:testtoken")
	}
	if gotFilter != "HELM_CHART" {
		t.Errorf("packaging_formats = %q, want HELM_CHART", gotFilter)
	}
}

func TestList_EmptyResults(t *testing.T) {
	srv := newTestServer(t, 100)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("expected 0 apps, got %d", len(apps))
	}
}

// ── Pagination ──────────────────────────────────────────────────────

func TestList_PageBasedPagination_WalksAllPages(t *testing.T) {
	srv := newTestServer(t, 2,
		newTestApp("a", "A", "1.0.0", "AI"),
		newTestApp("b", "B", "2.0.0", "AI"),
		newTestApp("c", "C", "3.0.0", "AI"),
	)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(apps) != 3 {
		t.Fatalf("expected 3 apps across 2 pages, got %d", len(apps))
	}
	got := []string{apps[0].ID, apps[1].ID, apps[2].ID}
	want := []string{"a", "b", "c"}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestList_Deduplication_KeepsFirstSeen(t *testing.T) {
	// Same slug returned on two pages — second occurrence dropped.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applications", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page_number"))
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 1:
			_ = json.NewEncoder(w).Encode(apiListResponse{
				Items: []apiListItem{
					{SlugName: "ollama", Name: "Ollama", PackagingFormat: "HELM_CHART"},
				},
				Page: 1, PageSize: 1, TotalSize: 2, TotalPages: 2,
			})
		case 2:
			_ = json.NewEncoder(w).Encode(apiListResponse{
				Items: []apiListItem{
					{SlugName: "ollama", Name: "Ollama-Duplicate", PackagingFormat: "HELM_CHART"},
				},
				Page: 2, PageSize: 1, TotalSize: 2, TotalPages: 2,
			})
		}
	})
	mux.HandleFunc("/v1/applications/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiAppDetail{
			SlugName: "ollama",
			Labels:   []string{"category:AI"},
			Branches: []apiBranch{{Baseline: "0.4.1"}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 app (deduped), got %d", len(apps))
	}
	if apps[0].DisplayName != "Ollama" {
		t.Errorf("expected first-seen DisplayName 'Ollama', got %q", apps[0].DisplayName)
	}
}

// ── Detail-fetch degradation ────────────────────────────────────────

func TestList_DetailFetch404_AppDropped(t *testing.T) {
	// Detail endpoint returns 404 for the only app → app has empty version
	// → filtered out (no usable chartRef). Expected: empty result list.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applications", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiListResponse{
			Items: []apiListItem{{SlugName: "ghost", Name: "Ghost", PackagingFormat: "HELM_CHART"}},
			Page:  1, PageSize: 100, TotalSize: 1, TotalPages: 1,
		})
	})
	mux.HandleFunc("/v1/applications/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected ghost app to be filtered out, got %d apps", len(apps))
	}
}

func TestList_DetailFetchPartialFailure_OtherAppsSurvive(t *testing.T) {
	// One app's detail returns 500; the other returns 200. List should
	// succeed and include the good app; the broken one is filtered out
	// because its version is empty.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applications", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiListResponse{
			Items: []apiListItem{
				{SlugName: "good", Name: "Good", PackagingFormat: "HELM_CHART"},
				{SlugName: "broken", Name: "Broken", PackagingFormat: "HELM_CHART"},
			},
			Page: 1, PageSize: 100, TotalSize: 2, TotalPages: 1,
		})
	})
	mux.HandleFunc("/v1/applications/good", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiAppDetail{
			SlugName: "good", Labels: []string{"category:AI"},
			Branches: []apiBranch{{Baseline: "1.0.0"}},
		})
	})
	// /v1/applications/broken returns 500 on every attempt (retry exhausts).
	brokenCalls := 0
	mu := sync.Mutex{}
	mux.HandleFunc("/v1/applications/broken", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		brokenCalls++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "good" {
		t.Fatalf("expected only good app to survive, got %v", apps)
	}
	mu.Lock()
	defer mu.Unlock()
	if brokenCalls < 1 {
		t.Errorf("expected at least one detail-fetch attempt on broken, got %d", brokenCalls)
	}
}

// ── Error classification ────────────────────────────────────────────

func TestList_AuthFailure401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL, Username: "bad", Token: "creds"})

	_, err := c.List(context.Background())
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestList_AuthFailure403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL, Username: "user", Token: "tok"})

	_, err := c.List(context.Background())
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestList_ServerError500_ReturnsUnavailable(t *testing.T) {
	// 5xx is classified as ErrUpstreamUnavailable and NOT retried —
	// isRetryable only matches errRetryableStatus (408/429) and
	// ErrCatalogMalformed. The attempt-count assertion pins that
	// behavior so a future change to retry 5xx is caught here.
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	_, err := c.List(context.Background())
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("expected ErrUpstreamUnavailable, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (5xx is not retried), got %d", attempts)
	}
}

func TestList_MalformedJSON_RetriesAndReturnsMalformed(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	_, err := c.List(context.Background())
	if !errors.Is(err, ErrCatalogMalformed) {
		t.Errorf("expected ErrCatalogMalformed, got %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts (original + 1 retry), got %d", attempts)
	}
}

func TestList_RateLimited429_RetriesAndReturnsUnavailable(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	_, err := c.List(context.Background())
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("expected ErrUpstreamUnavailable, got %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestList_ContextCancelled(t *testing.T) {
	srv := newTestServer(t, 100, newTestApp("a", "A", "1.0.0", "AI"))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.List(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// ── GetChart ────────────────────────────────────────────────────────

func TestGetChart_HappyPath_ReturnsMetadata(t *testing.T) {
	srv := newTestServer(t, 100, newTestApp("ollama", "Ollama", "0.4.1", "AI"))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL, Username: "u", Token: "t"})

	meta, err := c.GetChart(context.Background(), "oci://dp.apps.rancher.io/charts", "ollama", "0.4.1")
	if err != nil {
		t.Fatalf("GetChart failed: %v", err)
	}
	if meta.Name != "ollama" || meta.Version != "0.4.1" || meta.AppVersion != "0.4.1" {
		t.Errorf("got %+v", meta)
	}
}

func TestGetChart_VersionNotFound(t *testing.T) {
	srv := newTestServer(t, 100, newTestApp("ollama", "Ollama", "0.4.1", "AI"))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	_, err := c.GetChart(context.Background(), "", "ollama", "9.9.9")
	if !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("expected ErrVersionNotFound, got %v", err)
	}
}

func TestGetChart_ChartNotFound(t *testing.T) {
	srv := newTestServer(t, 100)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	_, err := c.GetChart(context.Background(), "", "missing-chart", "0.1.0")
	if !errors.Is(err, ErrChartNotFound) {
		t.Errorf("expected ErrChartNotFound, got %v", err)
	}
}

func TestGetChart_ContextCancelled(t *testing.T) {
	srv := newTestServer(t, 100, newTestApp("ollama", "Ollama", "0.4.1", "AI"))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.GetChart(ctx, "", "ollama", "0.4.1")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// ── UpdateSettings reflection ───────────────────────────────────────

func TestUpdateSettings_ReflectedInList(t *testing.T) {
	srv1 := newTestServer(t, 100, newTestApp("from-srv1", "Server 1", "1.0.0", "AI"))
	defer srv1.Close()
	srv2 := newTestServer(t, 100, newTestApp("from-srv2", "Server 2", "2.0.0", "AI"))
	defer srv2.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv1.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("first List failed: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "from-srv1" {
		t.Fatalf("expected from-srv1, got %v", apps)
	}

	c.UpdateSettings(EngineSettings{APIURL: srv2.URL})

	apps, err = c.List(context.Background())
	if err != nil {
		t.Fatalf("second List failed: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "from-srv2" {
		t.Fatalf("expected from-srv2, got %v", apps)
	}
}

// ── Field mappings ──────────────────────────────────────────────────

func TestList_MapsLastUpdatedAt(t *testing.T) {
	srv := newTestServer(t, 100, newTestApp("ollama", "Ollama", "0.4.1", "AI"))
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, _ := c.List(context.Background())
	if apps[0].LastUpdatedAt != "2026-04-30T23:56:07.607227Z" {
		t.Errorf("LastUpdatedAt = %q", apps[0].LastUpdatedAt)
	}
}

func TestList_LastUpdatedAt_EmptyWhenAbsent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applications", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiListResponse{
			Items: []apiListItem{{SlugName: "milvus", Name: "Milvus", PackagingFormat: "HELM_CHART"}},
			Page:  1, PageSize: 100, TotalSize: 1, TotalPages: 1,
		})
	})
	mux.HandleFunc("/v1/applications/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiAppDetail{
			SlugName: "milvus",
			Labels:   []string{"category:Vector DB"},
			Branches: []apiBranch{{Baseline: "2.4.0"}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, _ := c.List(context.Background())
	if apps[0].LastUpdatedAt != "" {
		t.Errorf("LastUpdatedAt = %q, want empty", apps[0].LastUpdatedAt)
	}
}

// ── Pure-function unit tests ────────────────────────────────────────

func TestBuildChartRef(t *testing.T) {
	cases := []struct {
		name, host, slug, version, want string
	}{
		{"bare host", "dp.apps.rancher.io", "ollama", "0.4.1", "oci://dp.apps.rancher.io/charts/ollama:0.4.1"},
		{"https scheme stripped", "https://dp.apps.rancher.io", "vllm", "0.6.0", "oci://dp.apps.rancher.io/charts/vllm:0.6.0"},
		{"path stripped", "dp.apps.rancher.io/charts", "milvus", "2.4.0", "oci://dp.apps.rancher.io/charts/milvus:2.4.0"},
		{"empty host defaults to production", "", "ollama", "0.4.1", "oci://dp.apps.rancher.io/charts/ollama:0.4.1"},
		{"missing version returns empty", "dp.apps.rancher.io", "ollama", "", ""},
		{"missing slug returns empty", "dp.apps.rancher.io", "", "0.4.1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildChartRef(tc.host, tc.slug, tc.version)
			if got != tc.want {
				t.Errorf("buildChartRef(%q, %q, %q) = %q, want %q", tc.host, tc.slug, tc.version, got, tc.want)
			}
		})
	}
}

func TestCategoriesFromLabels(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{}},
		{"no category labels", []string{"license:MIT", "ecosystem:cncf"}, []string{}},
		{"mixed", []string{"category:observability", "license:Apache-2.0", "category:logging"}, []string{"observability", "logging"}},
		{"empty category value skipped", []string{"category:", "category:AI"}, []string{"AI"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := categoriesFromLabels(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestLatestBaseline_PicksHighestNonLTS(t *testing.T) {
	branches := []apiBranch{
		{Baseline: "0.4.0", IsLTS: false},
		{Baseline: "0.5.0", IsLTS: false},
		{Baseline: "0.10.0", IsLTS: false}, // numerically highest
		{Baseline: "1.0.0", IsLTS: true},   // LTS — skipped
	}
	if got := latestBaseline(branches); got != "0.10.0" {
		t.Errorf("latestBaseline = %q, want 0.10.0", got)
	}
}

func TestLatestBaseline_FallsBackToLTS_WhenAllAreLTS(t *testing.T) {
	branches := []apiBranch{
		{Baseline: "1.0.0", IsLTS: true},
		{Baseline: "2.0.0", IsLTS: true},
	}
	if got := latestBaseline(branches); got != "2.0.0" {
		t.Errorf("latestBaseline = %q, want 2.0.0", got)
	}
}

func TestLatestBaseline_EmptyBranches_ReturnsEmpty(t *testing.T) {
	if got := latestBaseline(nil); got != "" {
		t.Errorf("latestBaseline(nil) = %q, want empty", got)
	}
}

func TestLatestBaseline_FallsBackToBranchName_WhenBaselineMissing(t *testing.T) {
	// Mirrors upstream shape for apps like postgresql: branches only
	// carry branch_name, no baseline. We want the highest non-LTS
	// branch_name so the app still shows in the catalog.
	branches := []apiBranch{
		{BranchName: "12", IsLTS: false},
		{BranchName: "18", IsLTS: false},
		{BranchName: "15", IsLTS: false},
	}
	if got := latestBaseline(branches); got != "18" {
		t.Errorf("latestBaseline = %q, want 18", got)
	}
}

func TestLatestBaseline_PrefersBaselineOverBranchName(t *testing.T) {
	// If any branch has a baseline, it wins even when other branches
	// only carry branch_name.
	branches := []apiBranch{
		{BranchName: "18", IsLTS: false},
		{BranchName: "0", Baseline: "0.27.0", IsLTS: false},
	}
	if got := latestBaseline(branches); got != "0.27.0" {
		t.Errorf("latestBaseline = %q, want 0.27.0", got)
	}
}

func TestLatestBaseline_FallsBackToLTSBranchName_WhenAllLTSAndNoBaseline(t *testing.T) {
	branches := []apiBranch{
		{BranchName: "1.10", IsLTS: true},
		{BranchName: "1.11", IsLTS: true},
	}
	if got := latestBaseline(branches); got != "1.11" {
		t.Errorf("latestBaseline = %q, want 1.11", got)
	}
}

func TestAbsolutizeLogoURL(t *testing.T) {
	cases := []struct {
		name, apiURL, logoURL, want string
	}{
		{
			name:    "SaaS api host rewritten to marketplace host",
			apiURL:  "https://api.apps.rancher.io",
			logoURL: "/logos/alertmanager.png",
			want:    "https://apps.rancher.io/logos/alertmanager.png",
		},
		{
			name:    "non-SaaS host with api. prefix is NOT rewritten (air-gap mirror)",
			apiURL:  "https://api.mirror.internal.corp",
			logoURL: "/logos/x.png",
			want:    "https://api.mirror.internal.corp/logos/x.png",
		},
		{
			name:    "non-SaaS host without api. prefix passes through unchanged",
			apiURL:  "https://mirror.internal.corp",
			logoURL: "/logos/x.png",
			want:    "https://mirror.internal.corp/logos/x.png",
		},
		{
			name:    "generic api.* host (not the SaaS pair) is NOT rewritten",
			apiURL:  "https://api.example.com",
			logoURL: "/logos/x.png",
			want:    "https://api.example.com/logos/x.png",
		},
		{
			name:    "absolute URL passes through unchanged",
			apiURL:  "https://api.example.com",
			logoURL: "https://other.com/x.png",
			want:    "https://other.com/x.png",
		},
		{
			name:    "empty logo stays empty",
			apiURL:  "https://api.example.com",
			logoURL: "",
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := absolutizeLogoURL(tc.apiURL, tc.logoURL)
			if got != tc.want {
				t.Errorf("absolutizeLogoURL(%q, %q) = %q, want %q", tc.apiURL, tc.logoURL, got, tc.want)
			}
		})
	}
}
