package source_collection

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNewClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, annReader := NewClient(logger)
	if c == nil {
		t.Fatal("expected non-nil Client")
	}
	if annReader == nil {
		t.Fatal("expected non-nil AnnotationReader")
	}
}

func TestList_NotConfigured(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	_, err := c.List(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got: %v", err)
	}
}

func TestGetChart_NotConfigured(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	_, err := c.GetChart(context.Background(), "", "ollama", "0.4.1")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got: %v", err)
	}
}

func TestFakeClient_ImplementsClient(t *testing.T) {
	var _ Client = &FakeClient{}
}

func TestUpdateSettings(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
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
		t.Errorf("expected APIURL 'https://custom.example.com', got %q", apiC.settings.APIURL)
	}
	if apiC.settings.Username != "user" {
		t.Errorf("expected Username 'user', got %q", apiC.settings.Username)
	}
	if apiC.settings.Token != "tok" {
		t.Errorf("expected Token 'tok', got %q", apiC.settings.Token)
	}
}

func newTestApp(slug, title, publisher, version string) apiApplication {
	return apiApplication{
		SlugName:      slug,
		Title:         title,
		Description:   "Description of " + title,
		PublisherName: publisher,
		Categories:    []apiCategory{{ID: "ai", Name: "AI"}, {ID: "ml", Name: "ML"}},
		Tags:          []string{"gpu", "inference"},
		LogoURL:       "https://example.com/" + slug + ".png",
		Helm: apiHelm{
			RepositoryURL: "oci://dp.apps.rancher.io/charts",
			ChartName:     slug,
		},
		LatestVersion: apiVersion{Version: version},
		LastUpdatedAt: "2026-04-30T23:56:07.607227Z",
	}
}

func TestList_SinglePage(t *testing.T) {
	resp := apiResponse{
		Items: []apiApplication{
			newTestApp("ollama", "Ollama", "Ollama Inc", "0.4.1"),
			newTestApp("vllm", "vLLM", "vLLM Project", "0.6.0"),
			newTestApp("milvus", "Milvus", "Zilliz", "2.4.0"),
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("packaging_format") != "HELM_CHART" {
			t.Errorf("expected packaging_format=HELM_CHART, got %q", r.URL.Query().Get("packaging_format"))
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "testuser" || pass != "testtoken" {
			t.Errorf("expected basic auth testuser:testtoken, got %q:%q (ok=%v)", user, pass, ok)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{
		APIURL:   srv.URL,
		Username: "testuser",
		Token:    "testtoken",
	})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(apps))
	}

	app := apps[0]
	if app.ID != "ollama" {
		t.Errorf("expected ID 'ollama', got %q", app.ID)
	}
	if app.DisplayName != "Ollama" {
		t.Errorf("expected DisplayName 'Ollama', got %q", app.DisplayName)
	}
	if app.Publisher != "Ollama Inc" {
		t.Errorf("expected Publisher 'Ollama Inc', got %q", app.Publisher)
	}
	if app.LatestVersion != "0.4.1" {
		t.Errorf("expected LatestVersion '0.4.1', got %q", app.LatestVersion)
	}
	if app.ChartRef != "oci://dp.apps.rancher.io/charts/ollama:0.4.1" {
		t.Errorf("expected ChartRef 'oci://dp.apps.rancher.io/charts/ollama:0.4.1', got %q", app.ChartRef)
	}
	if len(app.Categories) != 2 || app.Categories[0] != "AI" || app.Categories[1] != "ML" {
		t.Errorf("expected categories [AI, ML], got %v", app.Categories)
	}
	if app.Source != "api" {
		t.Errorf("expected Source 'api', got %q", app.Source)
	}
	if app.LastUpdatedAt != "2026-04-30T23:56:07.607227Z" {
		t.Errorf("expected LastUpdatedAt '2026-04-30T23:56:07.607227Z', got %q", app.LastUpdatedAt)
	}
}

func TestList_EmptyResults(t *testing.T) {
	resp := apiResponse{Items: []apiApplication{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("expected 0 apps, got %d", len(apps))
	}
}

func TestList_Pagination(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 0:
			page++
			json.NewEncoder(w).Encode(apiResponse{
				Items: []apiApplication{newTestApp("app-a", "App A", "Pub A", "1.0.0")},
				Next:  "http://" + r.Host + "/v1/applications?packaging_format=HELM_CHART&page=2",
			})
		case 1:
			page++
			json.NewEncoder(w).Encode(apiResponse{
				Items: []apiApplication{newTestApp("app-b", "App B", "Pub B", "2.0.0")},
				Next:  "http://" + r.Host + "/v1/applications?packaging_format=HELM_CHART&page=3",
			})
		case 2:
			json.NewEncoder(w).Encode(apiResponse{
				Items: []apiApplication{newTestApp("app-c", "App C", "Pub C", "3.0.0")},
			})
		}
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(apps))
	}
	if apps[0].ID != "app-a" || apps[1].ID != "app-b" || apps[2].ID != "app-c" {
		t.Errorf("unexpected app order: %v, %v, %v", apps[0].ID, apps[1].ID, apps[2].ID)
	}
}

func TestList_Deduplication(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 0:
			page++
			json.NewEncoder(w).Encode(apiResponse{
				Items: []apiApplication{
					newTestApp("ollama", "Ollama", "Ollama Inc", "0.4.1"),
					newTestApp("vllm", "vLLM", "vLLM Project", "0.6.0"),
				},
				Next: "http://" + r.Host + "/v1/applications?page=2",
			})
		case 1:
			json.NewEncoder(w).Encode(apiResponse{
				Items: []apiApplication{
					newTestApp("ollama", "Ollama Duplicate", "Other Publisher", "0.5.0"),
					newTestApp("milvus", "Milvus", "Zilliz", "2.4.0"),
				},
			})
		}
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 3 {
		t.Fatalf("expected 3 apps (deduped), got %d", len(apps))
	}

	for _, app := range apps {
		if app.ID == "ollama" {
			if app.Publisher != "Ollama Inc" {
				t.Errorf("expected first-seen publisher 'Ollama Inc', got %q", app.Publisher)
			}
			if app.LatestVersion != "0.4.1" {
				t.Errorf("expected first-seen version '0.4.1', got %q", app.LatestVersion)
			}
			return
		}
	}
	t.Error("ollama not found in results")
}

func TestList_AuthFailure401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL, Username: "bad", Token: "creds"})

	_, err := c.List(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestList_AuthFailure403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL, Username: "user", Token: "tok"})

	_, err := c.List(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestList_ServerError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	_, err := c.List(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("expected ErrUpstreamUnavailable, got %v", err)
	}
}

func TestList_MalformedJSON(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	_, err := c.List(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrCatalogMalformed) {
		t.Errorf("expected ErrCatalogMalformed, got %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts (original + 1 retry), got %d", attempts)
	}
}

func TestList_RateLimited429(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	_, err := c.List(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("expected ErrUpstreamUnavailable, got %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts (original + 1 retry), got %d", attempts)
	}
}

func TestList_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiResponse{Items: []apiApplication{}})
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.List(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetChart_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/applications/ollama/versions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiVersionsResponse{
			Items: []apiVersionEntry{
				{Version: "0.3.0", AppVersion: "0.3.0"},
				{Version: "0.4.1", AppVersion: "0.4.1"},
				{Version: "0.5.0", AppVersion: "0.5.0-beta"},
			},
		})
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL, Username: "user", Token: "tok"})

	meta, err := c.GetChart(context.Background(), "oci://dp.apps.rancher.io/charts", "ollama", "0.4.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Name != "ollama" {
		t.Errorf("expected Name 'ollama', got %q", meta.Name)
	}
	if meta.Version != "0.4.1" {
		t.Errorf("expected Version '0.4.1', got %q", meta.Version)
	}
	if meta.AppVersion != "0.4.1" {
		t.Errorf("expected AppVersion '0.4.1', got %q", meta.AppVersion)
	}
}

func TestGetChart_VersionNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiVersionsResponse{
			Items: []apiVersionEntry{
				{Version: "0.3.0", AppVersion: "0.3.0"},
			},
		})
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	_, err := c.GetChart(context.Background(), "oci://dp.apps.rancher.io/charts", "ollama", "9.9.9")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("expected ErrVersionNotFound, got %v", err)
	}
}

func TestGetChart_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiVersionsResponse{
			Items: []apiVersionEntry{{Version: "0.3.0", AppVersion: "0.3.0"}},
		})
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.GetChart(ctx, "", "ollama", "0.3.0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpdateSettings_ReflectedInList(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiResponse{
			Items: []apiApplication{newTestApp("from-srv1", "Server 1 App", "Pub1", "1.0.0")},
		})
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		if user != "newuser" || pass != "newtoken" {
			t.Errorf("expected newuser:newtoken, got %q:%q", user, pass)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiResponse{
			Items: []apiApplication{newTestApp("from-srv2", "Server 2 App", "Pub2", "2.0.0")},
		})
	}))
	defer srv2.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c, _ := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv1.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("first List failed: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "from-srv1" {
		t.Fatalf("expected from-srv1, got %v", apps)
	}

	c.UpdateSettings(EngineSettings{APIURL: srv2.URL, Username: "newuser", Token: "newtoken"})

	apps, err = c.List(context.Background())
	if err != nil {
		t.Fatalf("second List failed: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "from-srv2" {
		t.Fatalf("expected from-srv2, got %v", apps)
	}
}

func TestToApp_MapsLastUpdatedAt(t *testing.T) {
	app := apiApplication{
		SlugName:      "ollama",
		Title:         "Ollama",
		PublisherName: "Ollama Inc",
		Helm:          apiHelm{RepositoryURL: "oci://dp.apps.rancher.io/charts", ChartName: "ollama"},
		LatestVersion: apiVersion{Version: "0.4.1"},
		LastUpdatedAt: "2026-04-30T23:56:07.607227Z",
	}
	got := app.toApp()
	if got.LastUpdatedAt != "2026-04-30T23:56:07.607227Z" {
		t.Errorf("LastUpdatedAt = %q, want %q", got.LastUpdatedAt, "2026-04-30T23:56:07.607227Z")
	}
}

func TestToApp_LastUpdatedAt_EmptyWhenAbsent(t *testing.T) {
	app := apiApplication{
		SlugName:      "milvus",
		Title:         "Milvus",
		PublisherName: "Zilliz",
		Helm:          apiHelm{RepositoryURL: "oci://dp.apps.rancher.io/charts", ChartName: "milvus"},
		LatestVersion: apiVersion{Version: "2.4.0"},
	}
	got := app.toApp()
	if got.LastUpdatedAt != "" {
		t.Errorf("LastUpdatedAt = %q, want empty", got.LastUpdatedAt)
	}
}
