package api

import (
	"bytes"
	"context"
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
	h := NewWorkloadsHandler(upgrader, nil, nil, nil, logger)
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

// listDeleteTestRig wires list + delete handlers over FakeRepository. The
// auth checker is optional — pass nil to skip SAR enforcement (existing
// behavior-only tests) or a fakeAuthChecker to assert SAR was consulted.
type listDeleteTestRig struct {
	mux     *http.ServeMux
	repo    *workload.FakeRepository
	checker *fakeAuthChecker
}

func newListDeleteTestRig(t *testing.T) *listDeleteTestRig {
	t.Helper()
	return newListDeleteTestRigWithAuth(t, nil)
}

func newListDeleteTestRigWithAuth(t *testing.T, checker *fakeAuthChecker) *listDeleteTestRig {
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
	var auth AuthChecker
	if checker != nil {
		auth = checker
	}
	h := NewWorkloadsHandler(upgrader, repo, repo, auth, logger)
	h.Register(mux)
	return &listDeleteTestRig{mux: mux, repo: repo, checker: checker}
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

func TestWorkloadsList_NamespaceFilter(t *testing.T) {
	rig := newListDeleteTestRig(t)
	seedWorkload(rig.repo, "ns-a", "wl-1", aifv1.WorkloadSourceKindApp)
	seedWorkload(rig.repo, "ns-b", "wl-2", aifv1.WorkloadSourceKindApp)

	req := httptest.NewRequest("GET", "/api/v1/workloads?namespace=ns-a", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var items []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item filtered by namespace, got %d: %+v", len(items), items)
	}
	if meta, _ := items[0]["metadata"].(map[string]any); meta["namespace"] != "ns-a" {
		t.Errorf("expected ns-a, got %v", meta["namespace"])
	}
}

func TestWorkloadsList_LabelSelectorFilter(t *testing.T) {
	rig := newListDeleteTestRig(t)
	matchedW := &aifv1.Workload{}
	matchedW.Namespace = "team-a"
	matchedW.Name = "matched"
	matchedW.Labels = map[string]string{"app": "foo"}
	matchedW.Spec.Source.Kind = aifv1.WorkloadSourceKindApp
	rig.repo.Seed(matchedW)
	otherW := &aifv1.Workload{}
	otherW.Namespace = "team-a"
	otherW.Name = "other"
	otherW.Labels = map[string]string{"app": "bar"}
	otherW.Spec.Source.Kind = aifv1.WorkloadSourceKindApp
	rig.repo.Seed(otherW)

	req := httptest.NewRequest("GET", "/api/v1/workloads?labelSelector=app%3Dfoo", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var items []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d: %+v", len(items), items)
	}
	if meta, _ := items[0]["metadata"].(map[string]any); meta["name"] != "matched" {
		t.Errorf("expected matched, got %v", meta["name"])
	}
}

func TestWorkloadsList_MalformedLabelSelector(t *testing.T) {
	rig := newListDeleteTestRig(t)
	req := httptest.NewRequest("GET", "/api/v1/workloads?labelSelector=BAD!", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on malformed selector, got %d: %s", rr.Code, rr.Body.String())
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

// PUT replaces the whole spec, but the display-name field (spec.name) is
// required (CRD MinLength=1) and rarely something a caller wants to change
// when, say, bumping a chart version. The handler falls back to the existing
// spec.name when the body omits it; this test locks that contract in.
func TestWorkloadsPut_PreservesSpecNameWhenOmitted(t *testing.T) {
	rig := newListDeleteTestRig(t)
	w := &aifv1.Workload{}
	w.Namespace = "team-a"
	w.Name = "my-app"
	w.ResourceVersion = "1"
	w.Spec.Name = "Original Display Name"
	w.Spec.Source.Kind = aifv1.WorkloadSourceKindApp
	w.Spec.Source.App = &aifv1.AppRef{Repo: "repo", Chart: "chart", Version: "1.0.0"}
	rig.repo.Seed(w)

	// Body intentionally OMITS spec.name — only the source changes.
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

	got, err := rig.repo.Get(context.Background(), "team-a", "my-app")
	if err != nil {
		t.Fatalf("get after PUT: %v", err)
	}
	if got.Spec.Name != "Original Display Name" {
		t.Errorf("expected spec.Name preserved as %q, got %q", "Original Display Name", got.Spec.Name)
	}
	// Sanity: the field we DID send was applied.
	if got.Spec.Source.App == nil || got.Spec.Source.App.Version != "2.0.0" {
		t.Errorf("expected spec.source.app.version updated to 2.0.0, got %+v", got.Spec.Source.App)
	}
}

// SAR enforcement — each CRUD route must call CheckResource with the right
// (namespace, verb) and reject when the checker denies.

func TestWorkloadsList_SARDenied(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: false}
	rig := newListDeleteTestRigWithAuth(t, checker)

	req := httptest.NewRequest("GET", "/api/v1/workloads?namespace=team-a", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if len(checker.resourceCalls) != 1 || checker.resourceCalls[0].verb != "list" ||
		checker.resourceCalls[0].resource != "workloads" || checker.resourceCalls[0].namespace != "team-a" {
		t.Errorf("expected list/workloads/team-a SAR call, got %+v", checker.resourceCalls)
	}
}

func TestWorkloadsGet_SARDenied(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: false}
	rig := newListDeleteTestRigWithAuth(t, checker)
	seedWorkload(rig.repo, "team-a", "wl-1", aifv1.WorkloadSourceKindApp)

	req := httptest.NewRequest("GET", "/api/v1/workloads/team-a/wl-1", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if len(checker.resourceCalls) != 1 || checker.resourceCalls[0].verb != "get" ||
		checker.resourceCalls[0].namespace != "team-a" {
		t.Errorf("expected get/team-a SAR call, got %+v", checker.resourceCalls)
	}
}

func TestWorkloadsDelete_SARDenied(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: false}
	rig := newListDeleteTestRigWithAuth(t, checker)
	seedWorkload(rig.repo, "team-a", "wl-1", aifv1.WorkloadSourceKindApp)

	req := httptest.NewRequest("DELETE", "/api/v1/workloads/team-a/wl-1", nil)
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if len(checker.resourceCalls) != 1 || checker.resourceCalls[0].verb != "delete" {
		t.Errorf("expected delete SAR call, got %+v", checker.resourceCalls)
	}
}

func TestWorkloadsPut_SARDenied(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: false}
	rig := newListDeleteTestRigWithAuth(t, checker)

	body := map[string]any{"spec": map[string]any{"source": map[string]any{"kind": "App"}}}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/api/v1/workloads/team-a/wl-1", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if len(checker.resourceCalls) != 1 || checker.resourceCalls[0].verb != "update" {
		t.Errorf("expected update SAR call, got %+v", checker.resourceCalls)
	}
}

func TestWorkloadsCreate_SARDeniedFromBodyNamespace(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: false}
	rig := newListDeleteTestRigWithAuth(t, checker)

	body := map[string]any{
		"metadata": map[string]any{"name": "wl", "namespace": "team-a"},
		"spec":     map[string]any{"source": map[string]any{"kind": "App"}},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/workloads", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	// SAR must use the namespace from the request body, not the URL.
	if len(checker.resourceCalls) != 1 || checker.resourceCalls[0].namespace != "team-a" ||
		checker.resourceCalls[0].verb != "create" {
		t.Errorf("expected create/team-a SAR call (namespace from body), got %+v", checker.resourceCalls)
	}
}

func TestWorkloadsCreate_SARAllowed(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: true}
	rig := newListDeleteTestRigWithAuth(t, checker)

	body := map[string]any{
		"metadata": map[string]any{"name": "wl", "namespace": "team-a"},
		"spec": map[string]any{
			"source": map[string]any{
				"kind": "App",
				"app":  map[string]any{"repo": "r", "chart": "c", "version": "1.0.0"},
			},
			"targetClusters": []string{"c-1"},
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
