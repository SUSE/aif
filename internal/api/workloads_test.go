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
	h := NewWorkloadsHandler(upgrader, logger)
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
