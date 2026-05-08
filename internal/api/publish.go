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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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

	writeJSON(w, http.StatusOK, submitResponse{
		Phase:             string(result.Phase),
		ProposedVersion:   result.Submission.ProposedVersion,
		ChangeDescription: result.Submission.ChangeDescription,
		SubmittedBy:       result.Submission.SubmittedBy,
	})
}

// writePublishError maps domain sentinel errors from pkg/publish to the
// internal/api error codes and HTTP statuses. This explicit mapping is needed
// because pkg/publish defines its own sentinels (hexagonal layering — domain
// can't import internal/api), so errorCode() wouldn't match them. The handler
// is the adapter that bridges the two error vocabularies.
func writePublishError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, publish.ErrBundleNotFound):
		writeError(w, http.StatusNotFound, fmt.Errorf("%s: %w", err.Error(), ErrNotFound))
	case errors.Is(err, publish.ErrInvalidTransition):
		writeError(w, http.StatusConflict, fmt.Errorf("%s: %w", err.Error(), ErrInvalidTransition))
	case errors.Is(err, publish.ErrInvalidVersion):
		writeError(w, http.StatusBadRequest, fmt.Errorf("%s: %w", err.Error(), ErrInvalidInput))
	case errors.Is(err, publish.ErrPublisherRequired):
		writeError(w, http.StatusForbidden, fmt.Errorf("%s: %w", err.Error(), ErrForbidden))
	case errors.Is(err, publish.ErrPublishConflict):
		writeError(w, http.StatusConflict, fmt.Errorf("%s: %w", err.Error(), ErrPublishConflict))
	case errors.Is(err, publish.ErrUserRequired):
		writeError(w, http.StatusForbidden, fmt.Errorf("%s: %w", err.Error(), ErrForbidden))
	default:
		writeError(w, http.StatusInternalServerError, ErrInternal)
	}
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
