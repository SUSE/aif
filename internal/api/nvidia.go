package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/SUSE/aif/pkg/nvidia"
)

// nimProfile is a placeholder until P4-4 populates profile data.
type nimProfile struct{}

type refreshResponse struct {
	Count       int       `json:"count"`
	LastRefresh time.Time `json:"lastRefresh"`
}

// NIMHandler serves the /api/v1/nvidia/* REST endpoints. It depends on
// nvidia.Discovery (the port interface); the handler reads + filters
// but does not start ticker goroutines. Routes are registered against a
// caller-supplied *http.ServeMux via Register, conforming to the
// api.Handler interface.
//
// Logger note: this handler does NOT hold a constructor-injected logger.
// All request-scoped logging goes through LoggerFromContext(r.Context()),
// which retrieves the request_id-decorated child logger built by
// LoggingMiddleware (per CLAUDE.md "structured logging with request_id").
type NIMHandler struct {
	discovery nvidia.Discovery
}

// NewNIMHandler constructs a NIMHandler bound to the discovery port.
func NewNIMHandler(discovery nvidia.Discovery) *NIMHandler {
	return &NIMHandler{discovery: discovery}
}

// Register wires this handler's routes onto the provided mux. Go 1.22+
// ServeMux precedence resolves more specific patterns over wildcards
// independently of registration order, so /profiles wins over
// /{id} for that exact path.
func (h *NIMHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/nvidia/nims", h.list)
	mux.HandleFunc("GET /api/v1/nvidia/nims/{id}/profiles", h.profiles)
	mux.HandleFunc("GET /api/v1/nvidia/nims/{id}", h.get)
	mux.HandleFunc("POST /api/v1/nvidia/refresh", h.refresh)
}

// list serves GET /api/v1/nvidia/nims. Query params:
//
//	?type=all|llm|vlm   optional; default "all"
//
// Returns 200 + []NIMEntry JSON. Empty list is serialized as `[]` not `null`.
func (h *NIMHandler) list(w http.ResponseWriter, r *http.Request) {
	typeFilter := r.URL.Query().Get("type")
	if typeFilter == "" {
		typeFilter = "all"
	}
	if typeFilter != "all" && !isValidNIMType(typeFilter) {
		valid := validNIMTypeNames()
		writeError(w, errorStatus(ErrInvalidInput), fmt.Errorf("%w: type must be one of: all, %s", ErrInvalidInput, strings.Join(valid, ", ")))
		return
	}

	entries, err := h.discovery.Index(r.Context())
	if err != nil {
		h.logDiscoveryErr(r, "Index", err)
		mapped := mapNIMErr(err, "")
		writeError(w, errorStatus(mapped), mapped)
		return
	}

	out := make([]nvidia.NIMEntry, 0, len(entries))
	for _, e := range entries {
		if typeFilter == "all" || string(e.Type) == typeFilter {
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// get serves GET /api/v1/nvidia/nims/{id}. Returns a single NIMEntry.
func (h *NIMHandler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry, err := h.discovery.Get(r.Context(), id)
	if err != nil {
		h.logDiscoveryErr(r, "Get", err, "id", id)
		mapped := mapNIMErr(err, id)
		writeError(w, errorStatus(mapped), mapped)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

// mapNIMErr translates pkg/nvidia sentinels into the api package's
// sentinels so writeError + errorStatus + errorCode classify them
// correctly. The original discovery error is intentionally NOT wrapped
// — the visible API message stays clean; logDiscoveryErr records the
// underlying err for server-side debugging. Unknown errors fall through
// unchanged (default → 500).
func mapNIMErr(err error, id string) error {
	switch {
	case errors.Is(err, nvidia.ErrNIMNotFound):
		return fmt.Errorf("%w: NIM %q", ErrNotFound, id)
	case errors.Is(err, nvidia.ErrNotConfigured):
		return fmt.Errorf("%w: NIM discovery not configured (Settings not yet reconciled)", ErrUnavailable)
	default:
		return err
	}
}

// profiles serves GET /api/v1/nvidia/nims/{id}/profiles. Returns profile metadata.
func (h *NIMHandler) profiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := h.discovery.Get(r.Context(), id)
	if err != nil {
		h.logDiscoveryErr(r, "Get", err, "id", id)
		mapped := mapNIMErr(err, id)
		writeError(w, errorStatus(mapped), mapped)
		return
	}
	writeJSON(w, http.StatusOK, make([]nimProfile, 0))
}

// refresh serves POST /api/v1/nvidia/refresh. Forces an immediate NIM catalog refresh.
func (h *NIMHandler) refresh(w http.ResponseWriter, r *http.Request) {
	if err := h.discovery.Refresh(r.Context()); err != nil {
		h.logDiscoveryErr(r, "Refresh", err)
		mapped := mapNIMErr(err, "")
		writeError(w, errorStatus(mapped), mapped)
		return
	}

	entries, err := h.discovery.Index(r.Context())
	if err != nil {
		h.logDiscoveryErr(r, "Index", err)
		mapped := mapNIMErr(err, "")
		writeError(w, errorStatus(mapped), mapped)
		return
	}

	// LastRefresh reflects the time this manual refresh completed, not a
	// value from Discovery. A future Discovery.LastRefresh() method would
	// be more accurate when background tickers also refresh the cache.
	writeJSON(w, http.StatusOK, refreshResponse{
		Count:       len(entries),
		LastRefresh: time.Now().UTC(),
	})
}

// isValidNIMType checks whether s is a known nvidia.Type value. The
// handler delegates to the domain package so it doesn't hardcode the
// type universe.
func isValidNIMType(s string) bool {
	for _, t := range nvidia.ValidTypes() {
		if string(t) == s {
			return true
		}
	}
	return false
}

// validNIMTypeNames returns the string names of all known NIM types for
// use in error messages.
func validNIMTypeNames() []string {
	types := nvidia.ValidTypes()
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = string(t)
	}
	return names
}

// logDiscoveryErr emits a single Warn line per discovery-boundary error.
// Mirrors logCatalogErr in apps.go. Uses LoggerFromContext to propagate
// the request_id attribute set by LoggingMiddleware.
func (h *NIMHandler) logDiscoveryErr(r *http.Request, op string, err error, kv ...any) {
	args := []any{
		"op", op,
		"path", r.URL.Path,
		slog.Any("error", err),
	}
	args = append(args, kv...)
	LoggerFromContext(r.Context()).Warn("nim handler: discovery call failed", args...)
}

// Compile-time guard: NIMHandler satisfies api.Handler.
var _ Handler = (*NIMHandler)(nil)
