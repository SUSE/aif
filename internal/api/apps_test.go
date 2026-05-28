package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SUSE/aif/pkg/apps"
	"github.com/SUSE/aif/pkg/helm"
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
	gotID      string        // captured: most recent id passed to Get
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
		if !opts.IncludeReferenceBlueprints && a.ReferenceBlueprint {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeCatalog) Get(_ context.Context, id string) (apps.App, error) {
	f.gotID = id
	return f.getResult, f.getErr
}

func (f *fakeCatalog) Refresh(_ context.Context) error      { return nil }
func (f *fakeCatalog) UpdateSettings(_ apps.EngineSettings) {}

// sampleApps mixes Reference-Blueprint and non-RB apps across both
// sources so filter tests can be written.
func sampleApps() []apps.App {
	return []apps.App{
		{ID: "nvidia.nim-llm:1.0.0", Source: "nvidia", Name: "nim-llm",
			Categories: []string{"llm"}, ReferenceBlueprint: false},
		{ID: "nvidia.nim-vlm:2.0.0", Source: "nvidia", Name: "nim-vlm",
			Categories: []string{"vlm"}, ReferenceBlueprint: false},
		{ID: "nvidia.rag-blueprint:1.0", Source: "nvidia", Name: "rag-blueprint",
			Categories: []string{"reference-blueprint"}, ReferenceBlueprint: true},
		{ID: "suse.ollama:0.4.1", Source: "suse", Name: "ollama",
			Categories: []string{"AI", "Inference"}, ReferenceBlueprint: false},
		{ID: "suse.milvus:2.4.0", Source: "suse", Name: "milvus",
			Categories: []string{"AI", "Vector DB"}, ReferenceBlueprint: false},
	}
}

func newAppsHandlerForTest(c apps.Catalog) http.Handler {
	mux := http.NewServeMux()
	NewAppsHandler(c, &fakeInspector{}).Register(mux)
	return mux
}

// newAppsHandlerWithInspector lets the chart-values tests inject a
// custom helm.ChartInspector while reusing the same registration path
// as production callers.
func newAppsHandlerWithInspector(c apps.Catalog, ins helm.ChartInspector) http.Handler {
	mux := http.NewServeMux()
	NewAppsHandler(c, ins).Register(mux)
	return mux
}

// fakeInspector is a stub helm.ChartInspector for handler-level tests.
// It records the (repo, chart, version) triple the handler forwarded so
// tests can pin the normalization contract (oci:// scheme stripping).
type fakeInspector struct {
	values    map[string]any
	questions map[string]any
	err       error

	gotRepo, gotChart, gotVersion string
}

func (f *fakeInspector) DefaultValues(_ context.Context, repo, chart, version string) (map[string]any, map[string]any, error) {
	f.gotRepo, f.gotChart, f.gotVersion = repo, chart, version
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.values, f.questions, nil
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

// --- includeReferenceBlueprints accepts strconv.ParseBool forms ---

func TestAppsHandler_List_IncludeReferenceBlueprints_AcceptsCommonBoolForms(t *testing.T) {
	// strconv.ParseBool accepts "1/t/T/TRUE/true/True" as true and
	// "0/f/F/FALSE/false/False" as false. The handler MUST treat all
	// "true"-equivalents the same so frontend devs don't get bitten by
	// case sensitivity.
	cases := []struct {
		raw     string
		showRBs bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"t", true},
		{"false", false},
		{"False", false},
		{"0", false},
		{"", false},       // absent → default false
		{"yes", false},    // garbage → default false
		{"banana", false}, // garbage → default false
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			cat := &fakeCatalog{listResult: sampleApps()}
			h := newAppsHandlerForTest(cat)

			url := "/api/v1/apps"
			if tc.raw != "" {
				url += "?includeReferenceBlueprints=" + tc.raw
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			var got []apps.App
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			wantCount := 4 // RBs hidden
			if tc.showRBs {
				wantCount = 5 // RBs visible
			}
			if len(got) != wantCount {
				t.Errorf("raw=%q: got %d apps, want %d (showRBs=%v)",
					tc.raw, len(got), wantCount, tc.showRBs)
			}
		})
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

// --- GET /api/v1/apps/{id...}: happy path ---

func TestAppsHandler_Get_HappyPath_Returns200AndApp(t *testing.T) {
	want := apps.App{
		ID: "nvidia.nim-llm:1.0.0", Name: "nim-llm", Source: "nvidia",
	}
	cat := &fakeCatalog{getResult: want}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/nvidia.nim-llm:1.0.0", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got apps.App
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, rec.Body.String())
	}
	if got.ID != want.ID || got.Name != want.Name || got.Source != want.Source {
		t.Errorf("Get response = %+v, want %+v", got, want)
	}
}

// --- GET /api/v1/apps/{id}: dot-namespaced ID is a single path segment ---

func TestAppsHandler_Get_NamespacedID_RoutedToCatalog(t *testing.T) {
	cat := &fakeCatalog{getResult: apps.App{ID: "suse.ollama:0.4.1", Source: "suse"}}
	h := newAppsHandlerForTest(cat)

	const wantID = "suse.ollama:0.4.1"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/"+wantID, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Pin the actual id forwarded to catalog.Get — guards against a
	// regression where r.PathValue("id") returns "" or strips the dot.
	if cat.gotID != wantID {
		t.Errorf("catalog.Get received id = %q, want %q", cat.gotID, wantID)
	}
}

// --- GET /api/v1/apps/{id}: ErrAppNotFound → 404 NOT_FOUND ---

func TestAppsHandler_Get_AppNotFound_Returns404(t *testing.T) {
	cat := &fakeCatalog{getErr: apps.ErrAppNotFound}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/nvidia.does-not-exist:9.9.9", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var apiErr APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal APIError: %v\nbody=%s", err, rec.Body.String())
	}
	if apiErr.Code != ErrCodeNotFound {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeNotFound)
	}
}

// --- GET /api/v1/apps/{id}: ErrUnknownSource → 400 INVALID_INPUT ---

func TestAppsHandler_Get_UnknownSource_Returns400(t *testing.T) {
	cat := &fakeCatalog{getErr: apps.ErrUnknownSource}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/mystery.whatever:1.0", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var apiErr APIError
	_ = json.Unmarshal(rec.Body.Bytes(), &apiErr)
	if apiErr.Code != ErrCodeInvalidInput {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeInvalidInput)
	}
}

// --- GET /api/v1/apps/categories ---

func TestAppsHandler_Categories_ReturnsSortedDeduplicated(t *testing.T) {
	cat := &fakeCatalog{listResult: []apps.App{
		{ID: "a", Categories: []string{"Vector DB", "AI"}},
		{ID: "b", Categories: []string{"AI", "Inference"}},
		{ID: "c", Categories: []string{"llm"}},
		{ID: "d", Categories: []string{"AI"}}, // duplicate "AI"
		{ID: "e", Categories: nil},            // no-cats app: must not crash
	}}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/categories", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got []string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, rec.Body.String())
	}

	want := []string{"AI", "Inference", "Vector DB", "llm"}
	if len(got) != len(want) {
		t.Fatalf("got %d categories, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("categories[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// --- GET /api/v1/apps/categories with empty catalog returns [] not null ---

func TestAppsHandler_Categories_EmptyCatalog_SerializesAsEmptyArray(t *testing.T) {
	cat := &fakeCatalog{listResult: nil}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/categories", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("empty categories serialized as %q, want %q", body, "[]")
	}
}

// --- Routing precedence: literal /categories must beat /{id} ---

func TestAppsHandler_Categories_WinsOverGetByID(t *testing.T) {
	// Both /api/v1/apps/categories and /api/v1/apps/{id} could in
	// principle match the path "/api/v1/apps/categories" (with id =
	// "categories"). Go 1.22+ ServeMux resolves this in favour of the
	// more specific literal pattern, but it's worth a guard test in
	// case routing infra changes. If the {id} route ever wins, the
	// handler would call catalog.Get("categories") and the trap App's
	// ID would surface in the response.
	cat := &fakeCatalog{
		listResult: []apps.App{
			{ID: "a", Categories: []string{"AI", "Vector DB"}},
		},
		getResult: apps.App{ID: "trap.should-not-appear:0", Source: "trap"},
	}
	h := newAppsHandlerForTest(cat)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/categories", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Positive: 200 + the actual sorted [AI, Vector DB].
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got []string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("/categories did not return JSON []string; body=%s err=%v", rec.Body.String(), err)
	}
	want := []string{"AI", "Vector DB"}
	if len(got) != len(want) {
		t.Fatalf("/categories returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("/categories[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Negative: confirm catalog.Get was NOT routed via the wildcard.
	if cat.gotID == "categories" {
		t.Errorf("/categories was routed through /{id} (catalog.Get got id=%q)", cat.gotID)
	}
}

// --- Logger contract: catalog-boundary errors carry request_id ---
//
// Round-2 regression test. The CLAUDE.md "structured logging with
// request_id" mandate is only met if the handler retrieves the
// middleware-decorated logger via LoggerFromContext — NOT if it logs
// through the constructor-injected base logger (which carries no
// request_id). This test pins that contract by capturing the slog
// output and asserting both the warn message and the request_id
// attribute are present.

func TestAppsHandler_LogCatalogErr_PropagatesRequestID(t *testing.T) {
	// 1. Build a child logger with a known request_id, writing to a buffer.
	//    This mirrors what LoggingMiddleware constructs at runtime.
	var buf bytes.Buffer
	childLogger := slog.New(slog.NewTextHandler(&buf, nil)).With(
		"component", "api",
		"request_id", "test-req-id-abc-123",
	)

	// 2. Build the handler wired with a failing catalog so the get path
	//    triggers logCatalogErr.
	cat := &fakeCatalog{getErr: apps.ErrAppNotFound}
	mux := http.NewServeMux()
	NewAppsHandler(cat, &fakeInspector{}).Register(mux)

	// 3. Stash the child logger in the request context the same way
	//    LoggingMiddleware does. Bypassing the middleware chain in this
	//    test focuses the assertion on the handler's retrieval path.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/nvidia.does-not-exist:9.9.9", nil)
	req = req.WithContext(ContextWithLogger(req.Context(), childLogger))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// 4. The request itself should still produce a 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	// 5. The captured log output MUST contain the request_id and the
	//    warn message. Without LoggerFromContext, the constructor-
	//    injected logger would have written elsewhere (or nowhere) and
	//    this assertion would fail.
	output := buf.String()
	if !strings.Contains(output, "request_id=test-req-id-abc-123") {
		t.Errorf("expected log output to contain request_id; got:\n%s", output)
	}
	if !strings.Contains(output, "apps handler: catalog call failed") {
		t.Errorf("expected log output to contain warn message; got:\n%s", output)
	}
	if !strings.Contains(output, "op=Get") {
		t.Errorf("expected log output to contain op=Get attribute; got:\n%s", output)
	}
}

// --- GET /api/v1/apps/{id}/values: happy path ---
//
// The Configuration step of the App Install Wizard renders the chart's
// real defaults in an editable textarea. The handler must look up the
// App via the catalog, forward its ChartRef.Repo/Chart and the ?version
// query string to the inspector, and return {values, questions}.

func TestAppValues_HappyPath_Returns200WithMergedShape(t *testing.T) {
	app := apps.App{
		ID:     "nvidia.nim-llm:1.0.0",
		Name:   "nim-llm",
		Source: "nvidia",
		// App.ChartRef.Repo is stored with the oci:// scheme (see
		// pkg/apps/nvidia_source.go). The handler strips it before
		// calling DefaultValues so the inspector receives the bare
		// host/path the helm engine expects.
		ChartRef: apps.ChartRef{
			Repo:    "oci://registry.suse.com/ai/charts/nvidia",
			Chart:   "nim-llm",
			Version: "1.0.0",
		},
	}
	cat := &fakeCatalog{getResult: app}
	ins := &fakeInspector{
		values:    map[string]any{"replicaCount": float64(1)},
		questions: map[string]any{"variables": []any{}},
	}
	h := newAppsHandlerWithInspector(cat, ins)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/nvidia.nim-llm:1.0.0/values?version=1.0.0", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Values    map[string]any `json:"values"`
		Questions map[string]any `json:"questions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, rec.Body.String())
	}
	if got := body.Values["replicaCount"]; got != float64(1) {
		t.Errorf("values.replicaCount = %v, want 1", got)
	}
	if body.Questions == nil {
		t.Errorf("expected non-nil questions when inspector returned a map")
	}

	// Pin the normalization contract: the handler stripped oci:// before
	// calling the inspector.
	if ins.gotRepo != "registry.suse.com/ai/charts/nvidia" {
		t.Errorf("inspector received repo=%q, want %q (oci:// must be stripped)",
			ins.gotRepo, "registry.suse.com/ai/charts/nvidia")
	}
	if ins.gotChart != "nim-llm" {
		t.Errorf("inspector received chart=%q, want %q", ins.gotChart, "nim-llm")
	}
	if ins.gotVersion != "1.0.0" {
		t.Errorf("inspector received version=%q, want %q", ins.gotVersion, "1.0.0")
	}
}

// --- GET /api/v1/apps/{id}/values: unknown app → 404 ---

func TestAppValues_NotFound_Returns404(t *testing.T) {
	cat := &fakeCatalog{getErr: apps.ErrAppNotFound}
	ins := &fakeInspector{}
	h := newAppsHandlerWithInspector(cat, ins)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/nvidia.does-not-exist:9.9.9/values?version=9.9.9", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var apiErr APIError
	_ = json.Unmarshal(rec.Body.Bytes(), &apiErr)
	if apiErr.Code != ErrCodeNotFound {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeNotFound)
	}
	// The inspector must NOT have been called when the app lookup fails.
	if ins.gotRepo != "" || ins.gotChart != "" || ins.gotVersion != "" {
		t.Errorf("inspector should not be called on app-not-found; got repo=%q chart=%q version=%q",
			ins.gotRepo, ins.gotChart, ins.gotVersion)
	}
}

// --- GET /api/v1/apps/{id}/values: missing ?version → 400 ---

func TestAppValues_MissingVersion_Returns400(t *testing.T) {
	cat := &fakeCatalog{getResult: apps.App{
		ID: "nvidia.nim-llm:1.0.0",
		ChartRef: apps.ChartRef{
			Repo:    "oci://registry.suse.com/ai/charts/nvidia",
			Chart:   "nim-llm",
			Version: "1.0.0",
		},
	}}
	ins := &fakeInspector{}
	h := newAppsHandlerWithInspector(cat, ins)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/nvidia.nim-llm:1.0.0/values", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var apiErr APIError
	_ = json.Unmarshal(rec.Body.Bytes(), &apiErr)
	if apiErr.Code != ErrCodeInvalidInput {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeInvalidInput)
	}
	if ins.gotRepo != "" || ins.gotChart != "" || ins.gotVersion != "" {
		t.Errorf("inspector should not be called on missing-version; got repo=%q chart=%q version=%q",
			ins.gotRepo, ins.gotChart, ins.gotVersion)
	}
}

// --- GET /api/v1/apps/{id}/values: inspector failure → 500 ---

func TestAppValues_InspectorError_Returns500(t *testing.T) {
	app := apps.App{
		ID: "nvidia.nim-llm:1.0.0",
		ChartRef: apps.ChartRef{
			Repo:    "oci://registry.suse.com/ai/charts/nvidia",
			Chart:   "nim-llm",
			Version: "1.0.0",
		},
	}
	cat := &fakeCatalog{getResult: app}
	ins := &fakeInspector{err: errors.New("pull failed: connection refused")}
	h := newAppsHandlerWithInspector(cat, ins)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/nvidia.nim-llm:1.0.0/values?version=1.0.0", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

// --- GET /api/v1/apps/{id}/values: nil questions → null in JSON ---

func TestAppValues_NilQuestions_SerializesAsNull(t *testing.T) {
	app := apps.App{
		ID: "nvidia.nim-llm:1.0.0",
		ChartRef: apps.ChartRef{
			Repo:    "oci://registry.suse.com/ai/charts/nvidia",
			Chart:   "nim-llm",
			Version: "1.0.0",
		},
	}
	cat := &fakeCatalog{getResult: app}
	ins := &fakeInspector{
		values:    map[string]any{"foo": "bar"},
		questions: nil,
	}
	h := newAppsHandlerWithInspector(cat, ins)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/nvidia.nim-llm:1.0.0/values?version=1.0.0", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Inspect raw JSON: questions must serialize as null, not be omitted,
	// so the UI's optional handling works uniformly.
	if !strings.Contains(rec.Body.String(), `"questions":null`) {
		t.Errorf("expected questions field present as null; body=%s", rec.Body.String())
	}
}

// And the same for the list path, since logCatalogErr is shared.
func TestAppsHandler_List_LogCatalogErr_PropagatesRequestID(t *testing.T) {
	var buf bytes.Buffer
	childLogger := slog.New(slog.NewTextHandler(&buf, nil)).With(
		"request_id", "test-req-id-list-789",
	)

	cat := &fakeCatalog{listErr: apps.ErrUnknownSource}
	mux := http.NewServeMux()
	NewAppsHandler(cat, &fakeInspector{}).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil)
	req = req.WithContext(ContextWithLogger(req.Context(), childLogger))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	output := buf.String()
	if !strings.Contains(output, "request_id=test-req-id-list-789") {
		t.Errorf("expected log output to contain request_id; got:\n%s", output)
	}
	if !strings.Contains(output, "op=List") {
		t.Errorf("expected log output to contain op=List attribute; got:\n%s", output)
	}
}
