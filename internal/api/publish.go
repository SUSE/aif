package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

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

type submitResponse struct {
	Phase             string `json:"phase"`
	ProposedVersion   string `json:"proposedVersion"`
	ChangeDescription string `json:"changeDescription,omitempty"`
	SubmittedBy       string `json:"submittedBy"`
}

func (h *PublishHandler) submit(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, errors.New("authentication required: Impersonate-User header missing"))
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

	if result.Submission == nil {
		writeError(w, http.StatusInternalServerError, ErrInternal)
		return
	}

	LoggerFromContext(r.Context()).Info("bundle submitted",
		"namespace", ns, "name", name,
		"proposedVersion", body.ProposedVersion, "submittedBy", user)

	writeJSON(w, http.StatusOK, submitResponse{
		Phase:             string(result.Phase),
		ProposedVersion:   result.Submission.ProposedVersion,
		ChangeDescription: result.Submission.ChangeDescription,
		SubmittedBy:       result.Submission.SubmittedBy,
	})
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
	writeError(w, http.StatusNotImplemented, ErrNotImplemented)
}

func (h *PublishHandler) approve(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, ErrNotImplemented)
}

func (h *PublishHandler) requestChanges(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, ErrNotImplemented)
}
