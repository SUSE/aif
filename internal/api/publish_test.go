package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/publish"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ Handler = (*PublishHandler)(nil)

func setupPublishTest(bundles ...*aifv1.Bundle) (*http.ServeMux, *bundle.FakeRepository) {
	repo := bundle.NewFakeRepository()
	repo.Seed(bundles...)

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	wf := publish.New(publish.Deps{
		Bundles:    repo,
		Blueprints: blueprint.NewFakeRepository(),
		Authz:      publish.AllowAllAuthorizer{},
		Recorder:   &publish.FakeEventRecorder{},
		Logger:     logger,
	})

	handler := NewPublishHandler(wf, logger)
	mux := http.NewServeMux()
	handler.Register(mux)
	return mux, repo
}

func testDraftBundle(ns, name string) *aifv1.Bundle {
	return &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  ns,
			Name:       name,
			Generation: 5,
		},
		Spec: aifv1.BundleSpec{
			Title:           "Test",
			TargetBlueprint: "my-stack",
			UseCase:         "rag",
			Components:      []aifv1.ComponentRef{{Name: "llm"}},
		},
		Status: aifv1.BundleStatus{
			Phase: aifv1.BundlePhaseDraft,
		},
	}
}

func TestSubmitHandler_DraftToSubmitted(t *testing.T) {
	mux, _ := setupPublishTest(testDraftBundle("ns", "my-bundle"))

	body := `{"proposedVersion":"1.0.0","changeDescription":"initial"}`
	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/my-bundle/submit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "Submitted", resp["phase"])
}

func TestSubmitHandler_ChangesRequestedToSubmitted(t *testing.T) {
	b := testDraftBundle("ns", "my-bundle")
	b.Status.Phase = aifv1.BundlePhaseChangesRequested
	b.Status.Review = &aifv1.ReviewStatus{
		ReviewerComment: "fix it",
		ReviewedBy:      "bob",
		ReviewedAt:      metav1.Now(),
	}
	mux, _ := setupPublishTest(b)

	body := `{"proposedVersion":"1.0.1","changeDescription":"fixed"}`
	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/my-bundle/submit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSubmitHandler_AlreadySubmitted_Returns409(t *testing.T) {
	b := testDraftBundle("ns", "my-bundle")
	b.Status.Phase = aifv1.BundlePhaseSubmitted
	mux, _ := setupPublishTest(b)

	body := `{"proposedVersion":"1.0.0"}`
	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/my-bundle/submit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInvalidTransition, apiErr.Code)
}

func TestSubmitHandler_NotFound_Returns404(t *testing.T) {
	mux, _ := setupPublishTest()

	body := `{"proposedVersion":"1.0.0"}`
	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/missing/submit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSubmitHandler_InvalidVersion_Returns400(t *testing.T) {
	mux, _ := setupPublishTest(testDraftBundle("ns", "my-bundle"))

	body := `{"proposedVersion":"v1.0"}`
	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/my-bundle/submit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSubmitHandler_NoUser_Returns403(t *testing.T) {
	mux, _ := setupPublishTest(testDraftBundle("ns", "my-bundle"))

	body := `{"proposedVersion":"1.0.0"}`
	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/my-bundle/submit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	// No Impersonate-User header

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSubmitHandler_InvalidJSON_Returns400(t *testing.T) {
	mux, _ := setupPublishTest(testDraftBundle("ns", "my-bundle"))

	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/my-bundle/submit", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSubmitHandler_UnknownField_Returns400(t *testing.T) {
	mux, _ := setupPublishTest(testDraftBundle("ns", "my-bundle"))

	body := `{"proposedVerison":"1.0.0"}`
	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/my-bundle/submit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSubmitHandler_ConflictOnUpdate_Returns409(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(testDraftBundle("ns", "my-bundle"))
	repo.UpdateStatusErr = apierrors.NewConflict(
		schema.GroupResource{Group: "ai.suse.com", Resource: "bundles"},
		"my-bundle",
		errors.New("resourceVersion changed"),
	)

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	wf := publish.New(publish.Deps{
		Bundles:    repo,
		Blueprints: blueprint.NewFakeRepository(),
		Authz:      publish.AllowAllAuthorizer{},
		Recorder:   &publish.FakeEventRecorder{},
		Logger:     logger,
	})

	handler := NewPublishHandler(wf, logger)
	mux := http.NewServeMux()
	handler.Register(mux)

	body := `{"proposedVersion":"1.0.0"}`
	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/my-bundle/submit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodePublishConflict, apiErr.Code)
}

func TestSubmitHandler_RepositoryError_Returns500(t *testing.T) {
	repo := bundle.NewFakeRepository()
	repo.Seed(testDraftBundle("ns", "my-bundle"))
	repo.UpdateStatusErr = errors.New("etcd timeout")

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	wf := publish.New(publish.Deps{
		Bundles:    repo,
		Blueprints: blueprint.NewFakeRepository(),
		Authz:      publish.AllowAllAuthorizer{},
		Recorder:   &publish.FakeEventRecorder{},
		Logger:     logger,
	})

	handler := NewPublishHandler(wf, logger)
	mux := http.NewServeMux()
	handler.Register(mux)

	body := `{"proposedVersion":"1.0.0"}`
	req := httptest.NewRequest("POST", "/api/v1/bundles/ns/my-bundle/submit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Impersonate-User", "alice")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
