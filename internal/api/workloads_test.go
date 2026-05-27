package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/workload"
)

// upgradeTestRig wires a real workload.Upgrader directly onto the domain
// fakes (no aifv1 in this file). The handler talks to the Upgrader via its
// port, so the test surface is the same shape as production — only the
// internal/workload adapters are replaced.
type upgradeTestRig struct {
	mux        *http.ServeMux
	workloads  *workload.FakeWorkloadStore
	blueprints *workload.FakeBlueprintReader
	events     *workload.FakeUpgradeEventRecorder
}

func newUpgradeTestRig(t *testing.T) *upgradeTestRig {
	t.Helper()
	wStore := workload.NewFakeWorkloadStore()
	bpReader := workload.NewFakeBlueprintReader()
	rec := &workload.FakeUpgradeEventRecorder{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	upgrader := workload.NewUpgrader(wStore, bpReader, rec, logger)

	mux := http.NewServeMux()
	h := NewWorkloadsHandler(upgrader, nil, nil, logger)
	h.Register(mux)
	return &upgradeTestRig{mux: mux, workloads: wStore, blueprints: bpReader, events: rec}
}

func (r *upgradeTestRig) post(t *testing.T, ns, name string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf := &bytes.Buffer{}
	if body != nil {
		_ = json.NewEncoder(buf).Encode(body)
	}
	req := httptest.NewRequest("POST", "/api/v1/workloads/"+ns+"/"+name+"/upgrade", buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	r.mux.ServeHTTP(rr, req)
	return rr
}

func seedBlueprintWorkload(rig *upgradeTestRig, version string) {
	rig.workloads.Seed(&workload.UpgradeWorkloadView{
		Namespace:       "team-a",
		Name:            "rag-prod",
		ResourceVersion: "100",
		SourceKind:      workload.SourceKindBlueprint,
		Blueprint:       &workload.BlueprintRef{Name: "rag", Version: version},
	})
}

func seedBlueprint(rig *upgradeTestRig, lineage, version string, withdrawn bool) {
	rig.blueprints.Seed(&workload.UpgradeBlueprintView{
		Name:      lineage + "." + version,
		Lineage:   lineage,
		Withdrawn: withdrawn,
	})
}

func decodeAPIError(t *testing.T, rr *httptest.ResponseRecorder) *APIError {
	t.Helper()
	var e APIError
	if err := json.NewDecoder(rr.Body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return &e
}

func TestWorkloadUpgrade_MissingImpersonateUser(t *testing.T) {
	rig := newUpgradeTestRig(t)
	// Bypass rig.post so we can omit the Impersonate-User header.
	req := httptest.NewRequest("POST", "/api/v1/workloads/team-a/rag-prod/upgrade",
		strings.NewReader(`{"toBlueprintVersion":"1.1.0"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeForbidden {
		t.Errorf("expected error code %s, got %s", ErrCodeForbidden, got)
	}
}

func TestWorkloadUpgrade_MalformedBody(t *testing.T) {
	rig := newUpgradeTestRig(t)
	req := httptest.NewRequest("POST", "/api/v1/workloads/ns/wl/upgrade", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeInvalidInput {
		t.Errorf("expected error code %s, got %s", ErrCodeInvalidInput, got)
	}
}

func TestWorkloadUpgrade_MalformedVersion(t *testing.T) {
	rig := newUpgradeTestRig(t)
	rr := rig.post(t, "team-a", "rag-prod", map[string]string{"toBlueprintVersion": "not-a-semver"})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeInvalidInput {
		t.Errorf("expected error code %s, got %s", ErrCodeInvalidInput, got)
	}
}

func TestWorkloadUpgrade_WorkloadNotFound(t *testing.T) {
	rig := newUpgradeTestRig(t)
	rr := rig.post(t, "team-a", "missing", map[string]string{"toBlueprintVersion": "1.1.0"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeNotFound {
		t.Errorf("expected %s, got %s", ErrCodeNotFound, got)
	}
}

func TestWorkloadUpgrade_SourceNotBlueprint(t *testing.T) {
	rig := newUpgradeTestRig(t)
	rig.workloads.Seed(&workload.UpgradeWorkloadView{
		Namespace:       "team-a",
		Name:            "app-wl",
		ResourceVersion: "1",
		SourceKind:      workload.SourceKindApp,
	})
	rr := rig.post(t, "team-a", "app-wl", map[string]string{"toBlueprintVersion": "1.1.0"})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeInvalidInput {
		t.Errorf("expected %s, got %s", ErrCodeInvalidInput, got)
	}
}

func TestWorkloadUpgrade_BlueprintVersionNotFound(t *testing.T) {
	rig := newUpgradeTestRig(t)
	seedBlueprintWorkload(rig, "1.0.0")
	rr := rig.post(t, "team-a", "rag-prod", map[string]string{"toBlueprintVersion": "1.1.0"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeNotFound {
		t.Errorf("expected %s, got %s", ErrCodeNotFound, got)
	}
}

func TestWorkloadUpgrade_CrossLineage(t *testing.T) {
	rig := newUpgradeTestRig(t)
	seedBlueprintWorkload(rig, "1.0.0")
	rig.blueprints.Seed(&workload.UpgradeBlueprintView{
		Name:    "rag.1.1.0",
		Lineage: "vision", // mismatched lineage
	})
	rr := rig.post(t, "team-a", "rag-prod", map[string]string{"toBlueprintVersion": "1.1.0"})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	apiErr := decodeAPIError(t, rr)
	if apiErr.Code != ErrCodeInvalidInput {
		t.Errorf("expected %s, got %s", ErrCodeInvalidInput, apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "Cross-lineage upgrade not allowed") {
		t.Errorf("expected AC verbatim message, got %q", apiErr.Message)
	}
}

func TestWorkloadUpgrade_TargetWithdrawn(t *testing.T) {
	rig := newUpgradeTestRig(t)
	seedBlueprintWorkload(rig, "1.0.0")
	seedBlueprint(rig, "rag", "1.1.0", true)
	rr := rig.post(t, "team-a", "rag-prod", map[string]string{"toBlueprintVersion": "1.1.0"})
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
	apiErr := decodeAPIError(t, rr)
	if apiErr.Code != ErrCodeInvalidTransition {
		t.Errorf("expected %s, got %s", ErrCodeInvalidTransition, apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "Cannot upgrade to a Withdrawn Blueprint version") {
		t.Errorf("expected AC verbatim message, got %q", apiErr.Message)
	}
}

func TestWorkloadUpgrade_Downgrade(t *testing.T) {
	rig := newUpgradeTestRig(t)
	seedBlueprintWorkload(rig, "1.5.0")
	seedBlueprint(rig, "rag", "1.4.0", false)
	rr := rig.post(t, "team-a", "rag-prod", map[string]string{"toBlueprintVersion": "1.4.0"})
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
	apiErr := decodeAPIError(t, rr)
	if apiErr.Code != ErrCodeInvalidTransition {
		t.Errorf("expected %s, got %s", ErrCodeInvalidTransition, apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "Upgrade must target a higher version") {
		t.Errorf("expected AC verbatim message, got %q", apiErr.Message)
	}
}

type upgradeResponseBody struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	BlueprintName string `json:"blueprintName"`
	OldVersion    string `json:"oldVersion"`
	NewVersion    string `json:"newVersion"`
}

func TestWorkloadUpgrade_HappyPath(t *testing.T) {
	rig := newUpgradeTestRig(t)
	seedBlueprintWorkload(rig, "1.0.0")
	seedBlueprint(rig, "rag", "1.1.0", false)

	rr := rig.post(t, "team-a", "rag-prod", map[string]string{"toBlueprintVersion": "1.1.0"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp upgradeResponseBody
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.OldVersion != "1.0.0" || resp.NewVersion != "1.1.0" || resp.BlueprintName != "rag" {
		t.Errorf("unexpected response body: %+v", resp)
	}
	if len(rig.events.Events) != 1 {
		t.Errorf("expected 1 event, got %v", rig.events.Events)
	}
}

func TestWorkloadUpgrade_Conflict(t *testing.T) {
	rig := newUpgradeTestRig(t)
	seedBlueprintWorkload(rig, "1.0.0")
	seedBlueprint(rig, "rag", "1.1.0", false)
	rig.workloads.PatchErr = workload.ErrUpgradeConflict

	rr := rig.post(t, "team-a", "rag-prod", map[string]string{"toBlueprintVersion": "1.1.0"})
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeConflict {
		t.Errorf("expected %s, got %s", ErrCodeConflict, got)
	}
}

// listDeleteTestRig wires list + delete handlers over FakeRepository.
type listDeleteTestRig struct {
	mux  *http.ServeMux
	repo *workload.FakeRepository
}

func newListDeleteTestRig(t *testing.T) *listDeleteTestRig {
	t.Helper()
	repo := workload.NewFakeRepository()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	upgrader := workload.NewUpgrader(
		workload.NewFakeWorkloadStore(),
		workload.NewFakeBlueprintReader(),
		&workload.FakeUpgradeEventRecorder{},
		logger,
	)
	h := NewWorkloadsHandler(upgrader, repo, repo, logger)
	h.Register(mux)
	return &listDeleteTestRig{mux: mux, repo: repo}
}

func seedWorkload(repo *workload.FakeRepository, ns, name string, kind aifv1.WorkloadSourceKind) {
	w := &aifv1.Workload{}
	w.Namespace = ns
	w.Name = name
	w.Spec.Source.Kind = kind
	repo.Seed(w)
}

func TestWorkloadsList_Empty(t *testing.T) {
	rig := newListDeleteTestRig(t)
	req := httptest.NewRequest("GET", "/api/v1/workloads", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result []any
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array, got %d items", len(result))
	}
}

func TestWorkloadsList_WithItems(t *testing.T) {
	rig := newListDeleteTestRig(t)
	seedWorkload(rig.repo, "ns-a", "wl-1", aifv1.WorkloadSourceKindApp)
	seedWorkload(rig.repo, "ns-b", "wl-2", aifv1.WorkloadSourceKindBlueprint)

	req := httptest.NewRequest("GET", "/api/v1/workloads", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var result []any
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 workloads, got %d", len(result))
	}
}

func TestWorkloadsList_MissingUser(t *testing.T) {
	rig := newListDeleteTestRig(t)
	req := httptest.NewRequest("GET", "/api/v1/workloads", nil)
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestWorkloadsGet_HappyPath(t *testing.T) {
	rig := newListDeleteTestRig(t)
	seedWorkload(rig.repo, "team-a", "wl-1", aifv1.WorkloadSourceKindApp)

	req := httptest.NewRequest("GET", "/api/v1/workloads/team-a/wl-1", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta, _ := got["metadata"].(map[string]any); meta["name"] != "wl-1" || meta["namespace"] != "team-a" {
		t.Errorf("unexpected workload returned: %+v", got)
	}
}

func TestWorkloadsGet_NotFound(t *testing.T) {
	rig := newListDeleteTestRig(t)
	req := httptest.NewRequest("GET", "/api/v1/workloads/team-a/missing", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestWorkloadsGet_MissingUser(t *testing.T) {
	rig := newListDeleteTestRig(t)
	req := httptest.NewRequest("GET", "/api/v1/workloads/team-a/wl-1", nil)
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestWorkloadsDelete_HappyPath(t *testing.T) {
	rig := newListDeleteTestRig(t)
	seedWorkload(rig.repo, "team-a", "my-wl", aifv1.WorkloadSourceKindApp)

	req := httptest.NewRequest("DELETE", "/api/v1/workloads/team-a/my-wl", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
	// Confirm deleted
	req2 := httptest.NewRequest("GET", "/api/v1/workloads", nil)
	req2.Header.Set("Impersonate-User", "alice")
	rr2 := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr2, req2)
	var items []any
	json.NewDecoder(rr2.Body).Decode(&items)
	if len(items) != 0 {
		t.Errorf("expected 0 workloads after delete, got %d", len(items))
	}
}

func TestWorkloadsDelete_NotFound(t *testing.T) {
	rig := newListDeleteTestRig(t)
	req := httptest.NewRequest("DELETE", "/api/v1/workloads/ns/missing", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestWorkloadsDelete_MissingUser(t *testing.T) {
	rig := newListDeleteTestRig(t)
	req := httptest.NewRequest("DELETE", "/api/v1/workloads/ns/wl", nil)
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestWorkloadsCreate_HappyPathApp(t *testing.T) {
	rig := newListDeleteTestRig(t)
	body := map[string]any{
		"metadata": map[string]any{
			"name":      "my-wl",
			"namespace": "team-a",
		},
		"spec": map[string]any{
			"source": map[string]any{
				"kind": "App",
				"app":  map[string]any{"repo": "dp.apps.rancher.io", "chart": "nvidia-nim", "version": "1.0.0"},
			},
			"targetClusters": []string{"c-abc123"},
			"deployStrategy": "helm",
		},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/workloads", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWorkloadsCreate_MissingName(t *testing.T) {
	rig := newListDeleteTestRig(t)
	body := map[string]any{
		"metadata": map[string]any{"namespace": "team-a"},
		"spec":     map[string]any{"source": map[string]any{"kind": "App"}},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/workloads", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestWorkloadsCreate_RejectsUnknownFields(t *testing.T) {
	rig := newListDeleteTestRig(t)
	body := map[string]any{
		"metadata":     map[string]any{"name": "wl", "namespace": "team-a"},
		"spec":         map[string]any{"source": map[string]any{"kind": "App"}},
		"bogusTopKey":  "foo",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/workloads", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on unknown top-level field, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWorkloadsCreate_MissingUser(t *testing.T) {
	rig := newListDeleteTestRig(t)
	req := httptest.NewRequest("POST", "/api/v1/workloads", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestWorkloadsPut_HappyPath(t *testing.T) {
	rig := newListDeleteTestRig(t)
	// Seed an App workload
	w := &aifv1.Workload{}
	w.Namespace = "team-a"
	w.Name = "my-app"
	w.ResourceVersion = "1"
	w.Spec.Source.Kind = aifv1.WorkloadSourceKindApp
	w.Spec.Source.App = &aifv1.AppRef{Repo: "repo", Chart: "chart", Version: "1.0.0"}
	rig.repo.Seed(w)

	body := map[string]any{
		"spec": map[string]any{
			"source": map[string]any{
				"kind": "App",
				"app":  map[string]any{"repo": "repo", "chart": "chart", "version": "2.0.0"},
			},
		},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/api/v1/workloads/team-a/my-app", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWorkloadsPut_NotFound(t *testing.T) {
	rig := newListDeleteTestRig(t)
	body := map[string]any{"spec": map[string]any{}}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/api/v1/workloads/ns/missing", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestWorkloadsPut_RejectsUnknownFields(t *testing.T) {
	rig := newListDeleteTestRig(t)
	w := &aifv1.Workload{}
	w.Namespace = "team-a"
	w.Name = "wl"
	w.ResourceVersion = "1"
	rig.repo.Seed(w)

	body := map[string]any{
		"spec":         map[string]any{"source": map[string]any{"kind": "App"}},
		"bogusTopKey":  "foo",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/api/v1/workloads/team-a/wl", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on unknown top-level field, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWorkloadsPut_MissingUser(t *testing.T) {
	rig := newListDeleteTestRig(t)
	req := httptest.NewRequest("PUT", "/api/v1/workloads/ns/wl", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}
