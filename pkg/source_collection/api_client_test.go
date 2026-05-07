package source_collection

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNewClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c := NewClient(logger)
	if c == nil {
		t.Fatal("expected non-nil Client")
	}
}

func TestUpdateSettings(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c := NewClient(logger).(*apiClient)

	s := EngineSettings{
		APIURL:   "https://custom.example.com",
		OCIHost:  "oci.example.com",
		Username: "user",
		Token:    "tok",
	}
	c.UpdateSettings(s)

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.settings.APIURL != "https://custom.example.com" {
		t.Errorf("expected APIURL 'https://custom.example.com', got %q", c.settings.APIURL)
	}
	if c.settings.Username != "user" {
		t.Errorf("expected Username 'user', got %q", c.settings.Username)
	}
	if c.settings.Token != "tok" {
		t.Errorf("expected Token 'tok', got %q", c.settings.Token)
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
	c := NewClient(logger)
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
}

func TestList_EmptyResults(t *testing.T) {
	resp := apiResponse{Items: []apiApplication{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c := NewClient(logger)
	c.UpdateSettings(EngineSettings{APIURL: srv.URL})

	apps, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("expected 0 apps, got %d", len(apps))
	}
}
