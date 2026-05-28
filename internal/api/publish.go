package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/publish"
)

type PublishHandler struct {
	workflow publish.Workflow
	logger   *slog.Logger
}

func NewPublishHandler(w publish.Workflow, logger *slog.Logger) *PublishHandler {
	return &PublishHandler{workflow: w, logger: logger}
}

func (h *PublishHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/bundles/{namespace}/{name}/submit", h.submit)
	mux.HandleFunc("POST /api/v1/bundles/{namespace}/{name}/withdraw", h.withdraw)
	mux.HandleFunc("POST /api/v1/bundles/{namespace}/{name}/approve", h.approve)
	mux.HandleFunc("POST /api/v1/bundles/{namespace}/{name}/request-changes", h.requestChanges)
}

type submitRequest struct {
	ProposedVersion   string `json:"proposedVersion"`
	ChangeDescription string `json:"changeDescription"`
}

type bundleResponse struct {
	Namespace         string                       `json:"namespace"`
	Name              string                       `json:"name"`
	Phase             string                       `json:"phase"`
	Title             string                       `json:"title"`
	TargetBlueprint   string                       `json:"targetBlueprint"`
	UseCase           string                       `json:"useCase"`
	Components        []aifv1.ComponentRef          `json:"components"`
	Submission        *aifv1.SubmissionStatus       `json:"submission,omitempty"`
	Review            *aifv1.ReviewStatus           `json:"review,omitempty"`
	PublishedVersions []aifv1.PublishedVersionRef   `json:"publishedVersions,omitempty"`
}

func newBundleResponse(b bundle.Bundle) bundleResponse {
	components := b.Components
	if components == nil {
		components = []aifv1.ComponentRef{}
	}
	versions := b.PublishedVersions
	if versions == nil {
		versions = []aifv1.PublishedVersionRef{}
	}
	return bundleResponse{
		Namespace:         b.Namespace,
		Name:              b.Name,
		Phase:             string(b.Phase),
		Title:             b.Title,
		TargetBlueprint:   b.TargetBlueprint,
		UseCase:           b.UseCase,
		Components:        components,
		Submission:        b.Submission,
		Review:            b.Review,
		PublishedVersions: versions,
	}
}

func (h *PublishHandler) submit(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body submitRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", ErrInvalidInput))
		return
	}

	result, err := h.workflow.Submit(r.Context(), ns, name, publish.SubmitRequest{
		User:              user,
		ProposedVersion:   body.ProposedVersion,
		ChangeDescription: body.ChangeDescription,
	})
	if err != nil {
		LoggerFromContext(r.Context()).Warn("submit failed",
			"namespace", ns, "name", name, "error", err)
		writePublishError(w, err)
		return
	}

	LoggerFromContext(r.Context()).Info("bundle submitted",
		"namespace", ns, "name", name,
		"proposedVersion", body.ProposedVersion, "submittedBy", user)

	writeJSON(w, http.StatusOK, newBundleResponse(result))
}

// mapPublishErr translates a pkg/publish domain sentinel into the corresponding
// internal/api sentinel. The HTTP status is derived from errorStatus() — no
// duplication. P3-3..P3-6 handlers reuse this mapping.
func mapPublishErr(err error) error {
	switch {
	case errors.Is(err, publish.ErrBundleNotFound):
		return ErrNotFound
	case errors.Is(err, publish.ErrInvalidTransition):
		return ErrInvalidTransition
	case errors.Is(err, publish.ErrInvalidVersion):
		return ErrInvalidInput
	case errors.Is(err, publish.ErrPublisherRequired):
		return ErrForbidden
	case errors.Is(err, publish.ErrPublishConflict):
		return ErrPublishConflict
	case errors.Is(err, publish.ErrUserRequired):
		return ErrForbidden
	default:
		return ErrInternal
	}
}

func writePublishError(w http.ResponseWriter, err error) {
	apiSentinel := mapPublishErr(err)
	writeError(w, errorStatus(apiSentinel), &APIError{
		Code:    errorCode(apiSentinel),
		Message: err.Error(),
	})
}

func (h *PublishHandler) withdraw(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}

	result, err := h.workflow.Withdraw(r.Context(), ns, name, user)
	if err != nil {
		LoggerFromContext(r.Context()).Warn("withdraw failed",
			"namespace", ns, "name", name, "error", err)
		writePublishError(w, err)
		return
	}

	LoggerFromContext(r.Context()).Info("bundle withdrawn",
		"namespace", ns, "name", name, "withdrawnBy", user)

	writeJSON(w, http.StatusOK, newBundleResponse(result))
}

func (h *PublishHandler) approve(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, ErrNotImplemented)
}

func (h *PublishHandler) requestChanges(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, ErrNotImplemented)
}
