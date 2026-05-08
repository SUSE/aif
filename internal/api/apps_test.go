package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SUSE/aif/pkg/apps"
)

// fakeCatalog is a stub apps.Catalog for handler-level tests. List
// echoes the configured slice with opts honored (so the handler's
// query-param parsing is testable end-to-end). Get and the
// settings/refresh methods are minimal stubs.
type fakeCatalog struct {
	listResult []apps.App
	listErr    error
	getResult  apps.App
	getErr     error
	listOpts   apps.ListOpts // captured for assertions
}

func (f *fakeCatalog) List(_ context.Context, opts apps.ListOpts) ([]apps.App, error) {
	f.listOpts = opts
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]apps.App, 0, len(f.listResult))
	for _, a := range f.listResult {
		if opts.Source != "" && a.Source != opts.Source {
			continue
		}
		if opts.Category != "" {
			hit := false
			for _, c := range a.Categories {
				if c == opts.Category {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeCatalog) Get(_ context.Context, _ string) (apps.App, error) {
	return f.getResult, f.getErr
}

func (f *fakeCatalog) Refresh(_ context.Context) error      { return nil }
func (f *fakeCatalog) UpdateSettings(_ apps.EngineSettings) {}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// sampleApps mixes Reference-Blueprint and non-RB apps across both
// sources so filter tests can be written.
func sampleApps() []apps.App {
	return []apps.App{
		{ID: "nvidia/nim-llm:1.0.0", Source: "nvidia", Name: "nim-llm",
			Categories: []string{"llm"}, ReferenceBlueprint: false},
		{ID: "nvidia/nim-vlm:2.0.0", Source: "nvidia", Name: "nim-vlm",
			Categories: []string{"vlm"}, ReferenceBlueprint: false},
		{ID: "nvidia/rag-blueprint:1.0", Source: "nvidia", Name: "rag-blueprint",
			Categories: []string{"reference-blueprint"}, ReferenceBlueprint: true},
		{ID: "suse/ollama:0.4.1", Source: "suse", Name: "ollama",
			Categories: []string{"AI", "Inference"}, ReferenceBlueprint: false},
		{ID: "suse/milvus:2.4.0", Source: "suse", Name: "milvus",
			Categories: []string{"AI", "Vector DB"}, ReferenceBlueprint: false},
	}
}

func newAppsHandlerForTest(c apps.Catalog) http.Handler {
	mux := http.NewServeMux()
	NewAppsHandler(c, discardLogger()).Register(mux)
	return mux
}

// --- GET /api/v1/apps: default (RBs hidden) ---

func TestAppsHandler_List_Default_HidesReferenceBlueprints(t *testing.T) {
	cat := &fakeCatalog{listResult: sampleApps()}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json prefix", got)
	}

	var got []apps.App
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, rec.Body.String())
	}
	for _, a := range got {
		if a.ReferenceBlueprint {
			t.Errorf("default response leaked Reference Blueprint app: %+v", a)
		}
	}
	// Sanity: 4 non-RB apps in sampleApps.
	if len(got) != 4 {
		t.Errorf("expected 4 non-RB apps in default response, got %d", len(got))
	}
}

// --- GET /api/v1/apps?includeReferenceBlueprints=true ---

func TestAppsHandler_List_IncludeReferenceBlueprintsTrue_ShowsRBs(t *testing.T) {
	cat := &fakeCatalog{listResult: sampleApps()}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps?includeReferenceBlueprints=true", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []apps.App
	_ = json.Unmarshal(rec.Body.Bytes(), &got)

	hasRB := false
	for _, a := range got {
		if a.ReferenceBlueprint {
			hasRB = true
			break
		}
	}
	if !hasRB {
		t.Error("includeReferenceBlueprints=true did not return any RB app")
	}
	if len(got) != 5 {
		t.Errorf("expected all 5 apps with includeReferenceBlueprints=true, got %d", len(got))
	}
}

// --- GET /api/v1/apps?includeReferenceBlueprints=false (explicit) ---

func TestAppsHandler_List_IncludeReferenceBlueprintsFalse_HidesRBs(t *testing.T) {
	cat := &fakeCatalog{listResult: sampleApps()}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps?includeReferenceBlueprints=false", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var got []apps.App
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 4 {
		t.Errorf("explicit includeReferenceBlueprints=false should hide RBs; got %d apps", len(got))
	}
}

// --- GET /api/v1/apps?source=nvidia ---

func TestAppsHandler_List_FilterBySource_ForwardsToCatalog(t *testing.T) {
	cat := &fakeCatalog{listResult: sampleApps()}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps?source=nvidia", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if cat.listOpts.Source != "nvidia" {
		t.Errorf("handler forwarded ListOpts.Source=%q, want %q", cat.listOpts.Source, "nvidia")
	}
	var got []apps.App
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	for _, a := range got {
		if a.Source != "nvidia" {
			t.Errorf("got non-nvidia app in source=nvidia response: %+v", a)
		}
	}
}

// --- GET /api/v1/apps?category=llm ---

func TestAppsHandler_List_FilterByCategory_ForwardsToCatalog(t *testing.T) {
	cat := &fakeCatalog{listResult: sampleApps()}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps?category=llm", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if cat.listOpts.Category != "llm" {
		t.Errorf("handler forwarded ListOpts.Category=%q, want %q", cat.listOpts.Category, "llm")
	}
}

// --- GET /api/v1/apps with empty result returns [] not null ---

func TestAppsHandler_List_EmptyResult_SerializesAsEmptyArray(t *testing.T) {
	cat := &fakeCatalog{listResult: nil}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("empty list serialized as %q, want %q", body, "[]")
	}
}
