package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// blueprintTestRig wires the handler against blueprint.FakeRepository plus an
// in-test deployment counter. The auth checker is optional — pass nil to skip
// SAR enforcement (the handler's own missing-user check still returns 403),
// or supply a fakeAuthChecker to assert SAR was consulted.
type blueprintTestRig struct {
	mux     *http.ServeMux
	repo    *blueprint.FakeRepository
	counter *fakeBlueprintCounter
	checker *fakeAuthChecker
}

// fakeBlueprintCounter implements blueprintDeploymentCounter for tests.
type fakeBlueprintCounter struct {
	count int32
	err   error
	calls []struct{ name, version string }
}

func (f *fakeBlueprintCounter) CountByBlueprint(_ context.Context, name, version string) (int32, error) {
	f.calls = append(f.calls, struct{ name, version string }{name, version})
	return f.count, f.err
}

func newBlueprintTestRig(t *testing.T) *blueprintTestRig {
	t.Helper()
	return newBlueprintTestRigWithAuth(t, nil)
}

func newBlueprintTestRigWithAuth(t *testing.T, checker *fakeAuthChecker) *blueprintTestRig {
	t.Helper()
	repo := blueprint.NewFakeRepository()
	counter := &fakeBlueprintCounter{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	var auth AuthChecker
	if checker != nil {
		auth = checker
	}
	h := NewBlueprintsHandler(repo, counter, auth, logger)
	h.Register(mux)
	return &blueprintTestRig{mux: mux, repo: repo, counter: counter, checker: checker}
}

// seedBlueprintCR seeds the FakeRepository with a Blueprint CR named
// "{lineage}.{version}" plus the standard lineage/version labels. Named with
// the CR suffix to disambiguate from workloads_test.go's seedBlueprint
// (which seeds an UpgradeBlueprintView for the workload upgrader).
func seedBlueprintCR(repo *blueprint.FakeRepository, lineage, version string, phase aifv1.BlueprintPhase) {
	bp := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: lineage + "." + version,
			Labels: map[string]string{
				"ai.suse.com/blueprint-name":    lineage,
				"ai.suse.com/blueprint-version": version,
			},
		},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: lineage,
			Version:       version,
			UseCase:       "inference",
			Source:        aifv1.BlueprintSource{Type: aifv1.BlueprintSourcePublished},
			Components:    []aifv1.ComponentRef{{Name: "nim", Kind: aifv1.ComponentKindApp}},
			PublishedBy:   "admin",
		},
		Status: aifv1.BlueprintStatus{Phase: phase},
	}
	repo.Seed(bp)
}

// --- POST /api/v1/blueprints ---

func TestBlueprintsCreate_HappyPath(t *testing.T) {
	rig := newBlueprintTestRig(t)

	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"useCase":       "inference",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		"source":        map[string]any{"type": "Published"},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	// Verify stored CR carries the canonical name and the publishedBy header value.
	got, err := rig.repo.Get(context.Background(), "rag.1.0.0")
	if err != nil {
		t.Fatalf("Get after create: %v", err)
	}
	if got.Spec.PublishedBy != "admin" {
		t.Errorf("publishedBy = %q, want admin", got.Spec.PublishedBy)
	}
}

func TestBlueprintsCreate_MissingUser(t *testing.T) {
	rig := newBlueprintTestRig(t)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestBlueprintsCreate_InvalidBody(t *testing.T) {
	rig := newBlueprintTestRig(t)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeInvalidInput {
		t.Errorf("expected %s, got %s", ErrCodeInvalidInput, got)
	}
}

func TestBlueprintsCreate_MissingRequiredFields(t *testing.T) {
	rig := newBlueprintTestRig(t)
	body := map[string]any{"blueprintName": "rag"} // version + components missing
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestBlueprintsCreate_UnknownField(t *testing.T) {
	rig := newBlueprintTestRig(t)
	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		"source":        map[string]any{"type": "Published"},
		"bogus":         "field",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on unknown field, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBlueprintsCreate_Conflict(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)

	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"useCase":       "inference",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		"source":        map[string]any{"type": "Published"},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBlueprintsCreate_SARDenied(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: false}
	rig := newBlueprintTestRigWithAuth(t, checker)

	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		"source":        map[string]any{"type": "Published"},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if len(checker.resourceCalls) != 1 ||
		checker.resourceCalls[0].verb != "create" ||
		checker.resourceCalls[0].resource != "blueprints" ||
		checker.resourceCalls[0].group != "ai.suse.com" {
		t.Errorf("expected create/blueprints SAR call, got %+v", checker.resourceCalls)
	}
}

func TestBlueprintsCreate_SARAllowed(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: true}
	rig := newBlueprintTestRigWithAuth(t, checker)

	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"useCase":       "inference",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		"source":        map[string]any{"type": "Published"},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- PATCH /api/v1/blueprints/{name}/{version} ---

func TestBlueprintsDeprecate_HappyPath(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)

	body := map[string]any{"deprecated": true}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/v1/blueprints/rag/1.0.0", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got, _ := rig.repo.Get(context.Background(), "rag.1.0.0")
	if got.Status.Phase != aifv1.BlueprintPhaseDeprecated {
		t.Errorf("phase = %q, want Deprecated", got.Status.Phase)
	}
	if got.Status.Deprecation == nil || got.Status.Deprecation.ActionedBy != "admin" {
		t.Errorf("Deprecation = %+v, want ActionedBy=admin", got.Status.Deprecation)
	}
}

func TestBlueprintsDeprecate_Undeprecate(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseDeprecated)

	body := map[string]any{"deprecated": false}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/v1/blueprints/rag/1.0.0", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got, _ := rig.repo.Get(context.Background(), "rag.1.0.0")
	if got.Status.Phase != aifv1.BlueprintPhaseActive {
		t.Errorf("phase = %q, want Active", got.Status.Phase)
	}
	if got.Status.Deprecation != nil {
		t.Errorf("Deprecation = %+v, want nil after undeprecate", got.Status.Deprecation)
	}
}

func TestBlueprintsDeprecate_MissingUser(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)

	body := map[string]any{"deprecated": true}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/v1/blueprints/rag/1.0.0", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestBlueprintsDeprecate_InvalidBody(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)

	req := httptest.NewRequest("PATCH", "/api/v1/blueprints/rag/1.0.0", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestBlueprintsDeprecate_NotFound(t *testing.T) {
	rig := newBlueprintTestRig(t)
	body := map[string]any{"deprecated": true}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/v1/blueprints/rag/9.9.9", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestBlueprintsDeprecate_SARDenied(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: false}
	rig := newBlueprintTestRigWithAuth(t, checker)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)

	body := map[string]any{"deprecated": true}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/v1/blueprints/rag/1.0.0", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if len(checker.resourceCalls) != 1 || checker.resourceCalls[0].verb != "update" {
		t.Errorf("expected update SAR call, got %+v", checker.resourceCalls)
	}
}

// --- DELETE /api/v1/blueprints/{name}/{version} ---

func TestBlueprintsDelete_HappyPath(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)

	req := httptest.NewRequest("DELETE", "/api/v1/blueprints/rag/1.0.0", nil)
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(rig.counter.calls) != 1 {
		t.Errorf("expected 1 counter call, got %d", len(rig.counter.calls))
	}
}

func TestBlueprintsDelete_BlockedByWorkloads(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)
	rig.counter.count = 2

	req := httptest.NewRequest("DELETE", "/api/v1/blueprints/rag/1.0.0", nil)
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeConflict {
		t.Errorf("expected %s, got %s", ErrCodeConflict, got)
	}
}

func TestBlueprintsDelete_CounterError(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)
	rig.counter.err = errors.New("k8s unavailable")

	req := httptest.NewRequest("DELETE", "/api/v1/blueprints/rag/1.0.0", nil)
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBlueprintsDelete_NotFound(t *testing.T) {
	rig := newBlueprintTestRig(t)
	req := httptest.NewRequest("DELETE", "/api/v1/blueprints/rag/9.9.9", nil)
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestBlueprintsDelete_MissingUser(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)

	req := httptest.NewRequest("DELETE", "/api/v1/blueprints/rag/1.0.0", nil)
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestBlueprintsDelete_SARDenied(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: false}
	rig := newBlueprintTestRigWithAuth(t, checker)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseActive)

	req := httptest.NewRequest("DELETE", "/api/v1/blueprints/rag/1.0.0", nil)
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if len(checker.resourceCalls) != 1 || checker.resourceCalls[0].verb != "delete" {
		t.Errorf("expected delete SAR call, got %+v", checker.resourceCalls)
	}
}

// --- PR #58 review fix-ups: CRD-parity validation + Withdrawn guard + constructor fail-fast ---

func TestBlueprintsCreate_MissingUseCase(t *testing.T) {
	rig := newBlueprintTestRig(t)
	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		"source":        map[string]any{"type": "Published"},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeInvalidInput {
		t.Errorf("expected %s, got %s", ErrCodeInvalidInput, got)
	}
}

func TestBlueprintsCreate_InvalidUseCase(t *testing.T) {
	rig := newBlueprintTestRig(t)
	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"useCase":       "bogus",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		"source":        map[string]any{"type": "Published"},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeInvalidInput {
		t.Errorf("expected %s, got %s", ErrCodeInvalidInput, got)
	}
}

func TestBlueprintsCreate_RejectsWrapsVendorChartSource(t *testing.T) {
	rig := newBlueprintTestRig(t)
	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"useCase":       "inference",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		"source":        map[string]any{"type": "WrapsVendorChart"},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeInvalidInput {
		t.Errorf("expected %s, got %s", ErrCodeInvalidInput, got)
	}
}

func TestBlueprintsCreate_DerivesSourceLabel(t *testing.T) {
	rig := newBlueprintTestRig(t)
	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"useCase":       "inference",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		"source":        map[string]any{"type": "Published"},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	got, err := rig.repo.Get(context.Background(), "rag.1.0.0")
	if err != nil {
		t.Fatalf("Get after create: %v", err)
	}
	if label := got.Labels["ai.suse.com/blueprint-source"]; label != string(aifv1.BlueprintSourcePublished) {
		t.Errorf("blueprint-source label = %q, want %q", label, aifv1.BlueprintSourcePublished)
	}
	if got.Spec.Source.Type != aifv1.BlueprintSourcePublished {
		t.Errorf("spec.source.type = %q, want %q", got.Spec.Source.Type, aifv1.BlueprintSourcePublished)
	}
}

func TestBlueprintsCreate_DefaultsSourceTypeToPublished(t *testing.T) {
	rig := newBlueprintTestRig(t)
	body := map[string]any{
		"blueprintName": "rag",
		"version":       "1.0.0",
		"useCase":       "inference",
		"components":    []map[string]any{{"name": "nim", "kind": "App"}},
		// source omitted entirely
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	got, _ := rig.repo.Get(context.Background(), "rag.1.0.0")
	if got.Spec.Source.Type != aifv1.BlueprintSourcePublished {
		t.Errorf("spec.source.type = %q, want %q (defaulted)", got.Spec.Source.Type, aifv1.BlueprintSourcePublished)
	}
	if label := got.Labels["ai.suse.com/blueprint-source"]; label != string(aifv1.BlueprintSourcePublished) {
		t.Errorf("blueprint-source label = %q, want %q", label, aifv1.BlueprintSourcePublished)
	}
}

func TestBlueprintsDeprecate_RejectsWithdrawn(t *testing.T) {
	rig := newBlueprintTestRig(t)
	seedBlueprintCR(rig.repo, "rag", "1.0.0", aifv1.BlueprintPhaseWithdrawn)

	body := map[string]any{"deprecated": false}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/v1/blueprints/rag/1.0.0", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "admin")
	rr := httptest.NewRecorder()
	rig.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeConflict {
		t.Errorf("expected %s, got %s", ErrCodeConflict, got)
	}
}

// --- Constructor fail-fast ---

func TestNewBlueprintsHandler_PanicsOnNilRepo(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil repo")
		}
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_ = NewBlueprintsHandler(nil, &fakeBlueprintCounter{}, nil, logger)
}

func TestNewBlueprintsHandler_PanicsOnNilCounter(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil counter")
		}
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_ = NewBlueprintsHandler(blueprint.NewFakeRepository(), nil, nil, logger)
}
