package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/workload"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// upgradeTestRig wires a real workload.Upgrader to fakes and registers the
// handler on a fresh ServeMux. Each test gets its own rig — no shared state.
type upgradeTestRig struct {
	mux         *http.ServeMux
	workloadFR  *workload.FakeRepository
	blueprintFR *blueprint.FakeRepository
	eventFR     *workload.FakeUpgradeEventRecorder
}

func newUpgradeTestRig(t *testing.T) *upgradeTestRig {
	t.Helper()
	wRepo := workload.NewFakeRepository()
	bpRepo := blueprint.NewFakeRepository()
	rec := &workload.FakeUpgradeEventRecorder{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	upgrader := workload.NewUpgrader(wRepo, bpRepo, rec, logger)

	mux := http.NewServeMux()
	h := NewWorkloadsHandler(upgrader, logger)
	h.Register(mux)
	return &upgradeTestRig{mux: mux, workloadFR: wRepo, blueprintFR: bpRepo, eventFR: rec}
}

func (r *upgradeTestRig) post(t *testing.T, ns, name string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf := &bytes.Buffer{}
	if body != nil {
		_ = json.NewEncoder(buf).Encode(body)
	}
	req := httptest.NewRequest("POST", "/api/v1/workloads/"+ns+"/"+name+"/upgrade", buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.mux.ServeHTTP(rr, req)
	return rr
}

func seedBlueprintWorkload(rig *upgradeTestRig, version string) {
	rig.workloadFR.Seed(&aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "team-a",
			Name:            "rag-prod",
			ResourceVersion: "100",
		},
		Spec: aifv1.WorkloadSpec{
			Name: "rag-prod",
			Source: aifv1.WorkloadSource{
				Kind:      aifv1.WorkloadSourceKindBlueprint,
				Blueprint: &aifv1.BlueprintRef{Name: "rag", Version: version},
			},
		},
	})
}

func seedBlueprint(rig *upgradeTestRig, lineage, version string, phase aifv1.BlueprintPhase) {
	rig.blueprintFR.Seed(&aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: lineage + "." + version},
		Spec:       aifv1.BlueprintSpec{BlueprintName: lineage, Version: version},
		Status:     aifv1.BlueprintStatus{Phase: phase},
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

func TestWorkloadUpgrade_MalformedBody(t *testing.T) {
	rig := newUpgradeTestRig(t)
	req := httptest.NewRequest("POST", "/api/v1/workloads/ns/wl/upgrade", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
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
	rig.workloadFR.Seed(&aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "app-wl", ResourceVersion: "1"},
		Spec: aifv1.WorkloadSpec{
			Name: "app-wl",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindApp,
				App:  &aifv1.AppRef{Repo: "https://x", Chart: "y", Version: "1.0.0"},
			},
		},
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
	rig.blueprintFR.Seed(&aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "rag.1.1.0"},
		Spec:       aifv1.BlueprintSpec{BlueprintName: "vision", Version: "1.1.0"},
		Status:     aifv1.BlueprintStatus{Phase: aifv1.BlueprintPhaseActive},
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
	seedBlueprint(rig, "rag", "1.1.0", aifv1.BlueprintPhaseWithdrawn)
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
	seedBlueprint(rig, "rag", "1.4.0", aifv1.BlueprintPhaseActive)
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
	seedBlueprint(rig, "rag", "1.1.0", aifv1.BlueprintPhaseActive)

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
	if len(rig.eventFR.Events) != 1 {
		t.Errorf("expected 1 event, got %v", rig.eventFR.Events)
	}
}

func TestWorkloadUpgrade_Conflict(t *testing.T) {
	rig := newUpgradeTestRig(t)
	seedBlueprintWorkload(rig, "1.0.0")
	seedBlueprint(rig, "rag", "1.1.0", aifv1.BlueprintPhaseActive)
	rig.workloadFR.PatchErr = apiConflictForTesting()

	rr := rig.post(t, "team-a", "rag-prod", map[string]string{"toBlueprintVersion": "1.1.0"})
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
	if got := decodeAPIError(t, rr).Code; got != ErrCodeConflict {
		t.Errorf("expected %s, got %s", ErrCodeConflict, got)
	}
}

func apiConflictForTesting() error {
	return apierrors.NewConflict(
		schema.GroupResource{Group: "ai.suse.com", Resource: "workloads"},
		"rag-prod",
		errors.New("simulated"),
	)
}
