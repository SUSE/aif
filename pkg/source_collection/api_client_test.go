package source_collection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// testApp is a minimal fixture builder that produces a (list item,
// detail) pair for a given slug. Tests register pairs with newTestServer.
type testApp struct {
	list   apiListItem
	detail apiAppDetail
	// chartVersion / chartRevision drive the /v1/artifacts handler in
	// newTestServer. When chartVersion is empty, the handler returns an
	// empty items array (mirrors upstream "no chart artifact" for this
	// slug). The composed chart tag is "<version>" when revision is
	// empty, otherwise "<version>-<revision>".
	chartVersion  string // e.g. "1.55.0"
	chartRevision string // e.g. "13.1"
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
		},
		// Default to empty revision so the composed chart tag is just
		// `version` and existing assertions on LatestVersion: version stay
		// green. Tests that exercise the version-revision composition set
		// chartRevision explicitly.
		chartVersion:  version,
		chartRevision: "",
	}
}

// revisionSuffix returns "-<rev>" for non-empty rev, "" otherwise.
func revisionSuffix(rev string) string {
	if rev == "" {
		return ""
	}
	return "-" + rev
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
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, r *http.Request) {
		slug := r.URL.Query().Get("component_slug_name")
		for _, a := range apps {
			if a.list.SlugName != slug {
				continue
			}
			if a.chartVersion == "" {
				_, _ = w.Write([]byte(`{"items": [], "page": 1, "page_size": 1, "total_size": 0, "total_pages": 0}`))
				return
			}
			payload := fmt.Sprintf(`{
				"items": [{
					"name": "%s:%s%s",
					"version": %q,
					"revision": %q,
					"packaging_format": "HELM_CHART",
					"application_version": "ignored"
				}],
				"page": 1, "page_size": 1, "total_size": 1, "total_pages": 1
			}`, a.list.SlugName, a.chartVersion, revisionSuffix(a.chartRevision), a.chartVersion, a.chartRevision)
			_, _ = w.Write([]byte(payload))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
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
		})
	})
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"items":[{"name":"ollama:0.4.1","version":"0.4.1","revision":"","packaging_format":"HELM_CHART","application_version":"ignored"}],
			"page":1,"page_size":1,"total_size":1,"total_pages":1
		}`))
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
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, r *http.Request) {
		slug := r.URL.Query().Get("component_slug_name")
		switch slug {
		case "good":
			_, _ = w.Write([]byte(`{
				"items":[{"name":"good:1.0.0","version":"1.0.0","revision":"","packaging_format":"HELM_CHART","application_version":"ignored"}],
				"page":1,"page_size":1,"total_size":1,"total_pages":1
			}`))
		default:
			// broken never reaches /v1/artifacts; only good is asserted.
			http.Error(w, "not found", http.StatusNotFound)
		}
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
	// Assert errors.Is(context.Canceled), not just non-nil — the
	// detail-fan-out path used to mask cancellation as
	// ErrUpstreamUnavailable, so this guards against that regression.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestList_ContextCancelledMidFanOut_ReturnsContextError(t *testing.T) {
	// Detail handler blocks until the client connection closes (which
	// happens when our parent ctx fires). This exercises the fan-out
	// cancellation path: the list page succeeds, the detail fetch is
	// in flight when the deadline hits, and we want the resulting
	// error to surface as context.DeadlineExceeded — not as a partial
	// success with the slow app silently dropped, and not as
	// ErrUpstreamUnavailable.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applications", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiListResponse{
			Items: []apiListItem{{SlugName: "slow", Name: "Slow", PackagingFormat: "HELM_CHART"}},
			Page:  1, PageSize: 100, TotalSize: 1, TotalPages: 1,
		})
	})
	mux.HandleFunc("/v1/applications/", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	apps, err := c.List(ctx)
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context error, got err=%v apps=%v", err, apps)
	}
	if len(apps) != 0 {
		t.Errorf("expected empty result on cancellation, got %d apps", len(apps))
	}
}

// ── GetChart ────────────────────────────────────────────────────────

func TestGetChart_ReturnsAppVersionForMatchingChartTag(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"items": [
				{"name":"ollama:1.55.0-13.1","version":"1.55.0","revision":"13.1","packaging_format":"HELM_CHART","application_version":"0.21.2"},
				{"name":"ollama:1.38.0-12.4","version":"1.38.0","revision":"12.4","packaging_format":"HELM_CHART","application_version":"0.14.2"}
			],
			"page":1,"page_size":50,"total_size":2,"total_pages":1
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	meta, err := c.GetChart(context.Background(), "", "ollama", "1.38.0-12.4")
	if err != nil {
		t.Fatalf("GetChart: %v", err)
	}
	if meta.Name != "ollama" {
		t.Errorf("Name = %q, want ollama", meta.Name)
	}
	if meta.Version != "1.38.0" {
		t.Errorf("Version = %q, want 1.38.0 (bare Chart.yaml :version, not the OCI tag we requested)", meta.Version)
	}
	if meta.AppVersion != "0.14.2" {
		t.Errorf("AppVersion = %q, want 0.14.2 (artifact.application_version)", meta.AppVersion)
	}
}

func TestGetChart_UnknownVersion_ReturnsErrVersionNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"items":[{"name":"ollama:1.55.0-13.1","version":"1.55.0","revision":"13.1","packaging_format":"HELM_CHART","application_version":"0.21.2"}],
			"page":1,"page_size":50,"total_size":1,"total_pages":1
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})
	_, err := c.GetChart(context.Background(), "", "ollama", "9.9.9-0.0")
	if !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("expected ErrVersionNotFound, got %v", err)
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
		})
	})
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"items":[{"name":"milvus:2.4.0","version":"2.4.0","revision":"","packaging_format":"HELM_CHART","application_version":"ignored"}],
			"page":1,"page_size":1,"total_size":1,"total_pages":1
		}`))
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
		{"empty host returns empty (no silent SaaS fallback)", "", "ollama", "0.4.1", ""},
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

func TestFetchLatestChartArtifact_ReturnsVersionAndChartTag(t *testing.T) {
	mux := http.NewServeMux()
	var gotURL string
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_, _ = w.Write([]byte(`{
			"items": [{
				"name": "ollama:1.55.0-13.1",
				"version": "1.55.0",
				"revision": "13.1",
				"packaging_format": "HELM_CHART",
				"application_version": "0.21.2",
				"registered_at": "2026-04-30T23:56:07.607227Z"
			}],
			"page": 1, "page_size": 1, "total_size": 15, "total_pages": 15
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})
	cc := c.(*apiClient)
	settings, _ := cc.effectiveSettings()

	gotVersion, gotTag, err := cc.fetchLatestChartArtifact(context.Background(), settings, "ollama")
	if err != nil {
		t.Fatalf("fetchLatestChartArtifact: %v", err)
	}
	if gotVersion != "1.55.0" {
		t.Errorf("version = %q, want 1.55.0 (bare Chart.yaml :version)", gotVersion)
	}
	if gotTag != "1.55.0-13.1" {
		t.Errorf("chartTag = %q, want 1.55.0-13.1 (OCI tag)", gotTag)
	}
	if !strings.Contains(gotURL, "component_slug_name=ollama") {
		t.Errorf("URL missing component_slug_name=ollama: %s", gotURL)
	}
	if !strings.Contains(gotURL, "packaging_formats=HELM_CHART") {
		t.Errorf("URL missing packaging_formats=HELM_CHART: %s", gotURL)
	}
	if !strings.Contains(gotURL, "page_size=1") {
		t.Errorf("URL missing page_size=1: %s", gotURL)
	}
}

func TestFetchLatestChartArtifact_EmptyItems_ReturnsEmptyStrings(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items": [], "page": 1, "page_size": 1, "total_size": 0, "total_pages": 0}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})
	cc := c.(*apiClient)
	settings, _ := cc.effectiveSettings()

	gotVersion, gotTag, err := cc.fetchLatestChartArtifact(context.Background(), settings, "unknown-slug")
	if err != nil {
		t.Fatalf("expected nil err on empty items, got %v", err)
	}
	if gotVersion != "" || gotTag != "" {
		t.Errorf("expected empty version+tag on empty items, got (%q, %q)", gotVersion, gotTag)
	}
}

func TestFetchLatestChartArtifact_404_ReturnsErrChartNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})
	cc := c.(*apiClient)
	settings, _ := cc.effectiveSettings()

	_, _, err := cc.fetchLatestChartArtifact(context.Background(), settings, "anything")
	if !errors.Is(err, ErrChartNotFound) {
		t.Fatalf("expected ErrChartNotFound, got %v", err)
	}
}

func TestFetchLatestChartArtifact_EmptyRevision_TagEqualsVersion(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"items": [{
				"name": "foo:2.0.0",
				"version": "2.0.0",
				"revision": "",
				"packaging_format": "HELM_CHART",
				"application_version": "1.0.0"
			}],
			"page": 1, "page_size": 1, "total_size": 1, "total_pages": 1
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})
	cc := c.(*apiClient)
	settings, _ := cc.effectiveSettings()

	gotVersion, gotTag, _ := cc.fetchLatestChartArtifact(context.Background(), settings, "foo")
	if gotVersion != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", gotVersion)
	}
	if gotTag != "2.0.0" {
		t.Errorf("chartTag = %q, want 2.0.0 (no trailing hyphen when revision empty)", gotTag)
	}
}

func TestList_PopulatesVersionAndChartTagFromArtifact(t *testing.T) {
	// chartVersion + chartRevision compose into a tag value that no other
	// upstream field could supply — proving both LatestVersion (bare
	// version, display) and ChartTag (OCI tag, pull key) are sourced
	// from the /v1/artifacts endpoint.
	srv := newTestServer(t, 10, testApp{
		list: apiListItem{
			SlugName:    "ollama",
			Name:        "Ollama",
			Description: "Run LLMs locally.",
		},
		detail: apiAppDetail{
			SlugName: "ollama",
			Labels:   []string{"category:llm"},
		},
		chartVersion:  "1.55.0",
		chartRevision: "13.1",
	})
	defer srv.Close()

	c, _ := NewClient(discardLogger())
	c.UpdateSettings(EngineSettings{APIURL: srv.URL, OCIHost: "dp.apps.rancher.io"})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	got := apps[0]
	if got.LatestVersion != "1.55.0" {
		t.Errorf("LatestVersion = %q, want 1.55.0 (bare Chart.yaml :version, not the OCI tag)", got.LatestVersion)
	}
	if got.ChartTag != "1.55.0-13.1" {
		t.Errorf("ChartTag = %q, want 1.55.0-13.1 (artifact version-revision)", got.ChartTag)
	}
	if got.ChartRef != "oci://dp.apps.rancher.io/charts/ollama:1.55.0-13.1" {
		t.Errorf("ChartRef = %q, want oci://…/charts/ollama:1.55.0-13.1 (uses ChartTag suffix, not LatestVersion)", got.ChartRef)
	}
	if len(got.Categories) != 1 || got.Categories[0] != "llm" {
		t.Errorf("Categories = %v, want [llm] (still sourced from /v1/applications labels)", got.Categories)
	}
}
