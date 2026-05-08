package api

import (
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

func (h *PublishHandler) submit(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, ErrNotImplemented)
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
