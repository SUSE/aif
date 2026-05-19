package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newSettingsScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	s := kruntime.NewScheme()
	if err := aifv1.AddToScheme(s); err != nil {
		t.Fatalf("add aif scheme: %v", err)
	}
	return s
}

func newSettingsFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(newSettingsScheme(t)).
		WithStatusSubresource(&aifv1.Settings{}).
		WithObjects(objects...).
		Build()
}

func newSettingsHandlerForTest(c client.Client, applier SettingsApplier) http.Handler {
	mux := http.NewServeMux()
	NewSettingsHandler(c, applier).Register(mux)
	return mux
}

// sampleSettingsCR returns a minimal pre-existing singleton Settings CR.
func sampleSettingsCR() *aifv1.Settings {
	return &aifv1.Settings{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "settings",
			Namespace: "aif",
		},
	}
}

// --- (a) GET /api/v1/settings returns 200 with the current Settings spec ---

func TestSettingsHandler_Get_Returns200WithCurrentSpec(t *testing.T) {
	existing := sampleSettingsCR()
	existing.Spec.Fleet = &aifv1.FleetConfig{RepoURL: "https://git.example.com"}

	c := newSettingsFakeClient(t, existing)
	h := newSettingsHandlerForTest(c, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json prefix", ct)
	}

	var got aifv1.Settings
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if got.Spec.Fleet == nil || got.Spec.Fleet.RepoURL != "https://git.example.com" {
		t.Errorf("fleet.repoURL = %v, want https://git.example.com", got.Spec.Fleet)
	}
}

// --- (b) GET /api/v1/settings returns 404 when no Settings CR exists ---

func TestSettingsHandler_Get_Returns404WhenNoCR(t *testing.T) {
	c := newSettingsFakeClient(t) // no objects seeded
	h := newSettingsHandlerForTest(c, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	var apiErr APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal APIError: %v; body=%s", err, rec.Body.String())
	}
	if apiErr.Code != ErrCodeNotFound {
		t.Errorf("error.code = %q, want %q", apiErr.Code, ErrCodeNotFound)
	}
}

// --- (c) PUT /api/v1/settings returns 200 and updates the CR in the cluster ---

func TestSettingsHandler_Put_Returns200AndUpdatesCR(t *testing.T) {
	c := newSettingsFakeClient(t, sampleSettingsCR())
	h := newSettingsHandlerForTest(c, nil)

	body := `{"spec":{"fleet":{"repoURL":"https://git.example.com","branch":"main"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// Response body must contain the updated spec.
	var resp aifv1.Settings
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	if resp.Spec.Fleet == nil || resp.Spec.Fleet.RepoURL != "https://git.example.com" {
		t.Errorf("response fleet.repoURL = %v, want https://git.example.com", resp.Spec.Fleet)
	}

	// CR must also be updated in the cluster (not just the response).
	var stored aifv1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aif", Name: "settings"}, &stored); err != nil {
		t.Fatalf("Get after PUT: %v", err)
	}
	if stored.Spec.Fleet == nil || stored.Spec.Fleet.RepoURL != "https://git.example.com" {
		t.Errorf("stored fleet.repoURL = %v, want https://git.example.com", stored.Spec.Fleet)
	}
}

// fakeSettingsApplier is a recording SettingsApplier for test (d).
// Tracks Apply calls to verify the handler never drives engine propagation.
type fakeSettingsApplier struct {
	calls int
}

func (f *fakeSettingsApplier) Apply(_ context.Context, _ SettingsSnapshot) error {
	f.calls++
	return nil
}

// --- (d) PUT /api/v1/settings does not synchronously drive the engine bus ---
//
// Engine propagation is async, driven by SettingsReconciler on the next
// reconcile loop (ARCHITECTURE.md §8.2.1). This test injects a FakeSettingsApplier
// and asserts zero Apply calls after a successful PUT, guarding against future
// refactors that might accidentally call Apply synchronously.

func TestSettingsHandler_Put_DoesNotDriveEngineBus(t *testing.T) {
	c := newSettingsFakeClient(t, sampleSettingsCR())
	applier := &fakeSettingsApplier{}
	h := newSettingsHandlerForTest(c, applier)

	body := `{"spec":{"fleet":{"repoURL":"https://git.example.com"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// The handler MUST NOT call applier.Apply — reconcile is async.
	if applier.calls != 0 {
		t.Errorf("applier.Apply called %d times, want 0 (engine propagation is async)", applier.calls)
	}
}

// --- PUT with invalid JSON returns 400 ---

func TestSettingsHandler_Put_InvalidJSON_Returns400(t *testing.T) {
	c := newSettingsFakeClient(t)
	h := newSettingsHandlerForTest(c, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader("{invalid"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var apiErr APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal APIError: %v; body=%s", err, rec.Body.String())
	}
	if apiErr.Code != ErrCodeInvalidInput {
		t.Errorf("error.code = %q, want %q", apiErr.Code, ErrCodeInvalidInput)
	}
}
