package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"

	"github.com/SUSE/aif/pkg/apps"
)

// AppsHandler serves the /api/v1/apps* REST endpoints. It depends on
// apps.Catalog (the read-only port; NOT the bootstrap-time
// apps.Aggregator) — the handler reads + filters; it does not register
// sources or start ticker goroutines. Routes are registered against a
// caller-supplied *http.ServeMux via Register, conforming to the
// api.Handler interface.
//
// Logger note: this handler does NOT hold a constructor-injected logger.
// All request-scoped logging goes through LoggerFromContext(r.Context()),
// which retrieves the request_id-decorated child logger built by
// LoggingMiddleware (per CLAUDE.md "structured logging with request_id").
type AppsHandler struct {
	catalog apps.Catalog
}

// NewAppsHandler constructs an AppsHandler bound to the catalog port.
func NewAppsHandler(catalog apps.Catalog) *AppsHandler {
	return &AppsHandler{catalog: catalog}
}

// Register wires this handler's routes onto the provided mux. App IDs
// are dot-namespaced single tokens (e.g. `nvidia.nim-llm:1.0.0`) so the
// per-app route is a plain `{id}` path-segment pattern — no trailing
// wildcard needed. Go 1.22+ ServeMux precedence still gives /categories
// priority over /{id} for that exact path.
func (h *AppsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/apps", h.list)
	mux.HandleFunc("GET /api/v1/apps/categories", h.categories)
	mux.HandleFunc("GET /api/v1/apps/{id}", h.get)
}

// list serves GET /api/v1/apps. Query params:
//
//	?source=nvidia|suse              optional; forwarded to apps.ListOpts.Source
//	?category=<exact>                optional; forwarded to apps.ListOpts.Category
//	?includeReferenceBlueprints=...  default false; when false, apps with
//	                                 ReferenceBlueprint=true are filtered out
//	                                 of the response (per ARCHITECTURE.md §5).
//
// Returns 200 + []App JSON. Empty list is serialized as `[]` not `null`.
func (h *AppsHandler) list(w http.ResponseWriter, r *http.Request) {
	opts := apps.ListOpts{
		Source:   r.URL.Query().Get("source"),
		Category: r.URL.Query().Get("category"),
	}
	includeRBs := parseIncludeReferenceBlueprints(r)

	all, err := h.catalog.List(r.Context(), opts)
	if err != nil {
		h.logCatalogErr(r, "List", err, "source", opts.Source, "category", opts.Category)
		// catalog.List has no mappable sentinels today (stale-but-good
		// design: partial source failures are logged and absorbed by
		// the catalog itself — see P2-3 §catalog behavior). This branch
		// is a defensive net for future error paths; until then it
		// falls through to a generic 500.
		writeError(w, errorStatus(err), err)
		return
	}

	// Always return a non-nil slice so JSON emits `[]` not `null`.
	out := make([]apps.App, 0, len(all))
	for _, a := range all {
		if !includeRBs && a.ReferenceBlueprint {
			continue
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, out)
}

// parseIncludeReferenceBlueprints parses the `includeReferenceBlueprints`
// query parameter via strconv.ParseBool, which accepts "1", "t", "T",
// "TRUE", "true", "True", "0", "f", "F", "FALSE", "false", "False".
// Absent or unparseable values default to false (per ARCHITECTURE.md
// §5: "default false"). The forgiving parser was chosen so frontend
// devs aren't surprised by a strict case-sensitive match.
func parseIncludeReferenceBlueprints(r *http.Request) bool {
	raw := r.URL.Query().Get("includeReferenceBlueprints")
	if raw == "" {
		return false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return v
}

// get serves GET /api/v1/apps/{id}. The dot-namespaced ID is a single
// path segment (e.g. "nvidia.nim-llm:1.0.0"). Returns the single App
// regardless of the includeReferenceBlueprints flag (per
// ARCHITECTURE.md §5: "Single app (returned regardless of
// referenceBlueprint flag)").
//
// Error mapping (catalog → API):
//
//	apps.ErrAppNotFound    → 404 NOT_FOUND
//	apps.ErrUnknownSource  → 400 INVALID_INPUT
//	other                  → 500 INTERNAL_ERROR
func (h *AppsHandler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	app, err := h.catalog.Get(r.Context(), id)
	if err != nil {
		mapped := mapCatalogErr(err, id)
		h.logCatalogErr(r, "Get", err, "id", id)
		writeError(w, errorStatus(mapped), mapped)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

// mapCatalogErr translates pkg/apps sentinels into the api package's
// sentinels so writeError + errorStatus + errorCode classify them
// correctly. The original catalog error is intentionally NOT wrapped
// — the visible API message stays clean ("not found: app \"x\""), and
// the handler's logCatalogErr call records the underlying err for
// server-side debugging. Unknown errors fall through unchanged
// (default → 500).
func mapCatalogErr(err error, id string) error {
	switch {
	case errors.Is(err, apps.ErrAppNotFound):
		return fmt.Errorf("%w: app %q", ErrNotFound, id)
	case errors.Is(err, apps.ErrUnknownSource):
		return fmt.Errorf("%w: id %q has unknown source prefix", ErrInvalidInput, id)
	default:
		return err
	}
}

// categories serves GET /api/v1/apps/categories. Returns a
// deduplicated, sorted []string of every category present in the
// unfiltered catalog. Empty list serialized as `[]` not `null`.
func (h *AppsHandler) categories(w http.ResponseWriter, r *http.Request) {
	all, err := h.catalog.List(r.Context(), apps.ListOpts{})
	if err != nil {
		h.logCatalogErr(r, "categories.List", err)
		// Same defensive guard as list above — catalog.List has no
		// mappable sentinels today; future error paths fall through
		// to a generic 500.
		writeError(w, errorStatus(err), err)
		return
	}

	seen := make(map[string]struct{})
	for _, a := range all {
		for _, c := range a.Categories {
			seen[c] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	writeJSON(w, http.StatusOK, out)
}

// logCatalogErr emits a single Warn line per catalog-boundary error.
// CLAUDE.md mandates HTTP handlers log with slog + request_id. The
// request_id-decorated child logger is built by LoggingMiddleware and
// stashed in the request context via ContextWithLogger; this helper
// pulls it back with LoggerFromContext so the emitted record actually
// carries request_id. LoggerFromContext falls back to slog.Default()
// when no logger is in context (e.g. direct ServeHTTP calls in tests
// that bypass the middleware), so no nil guard is needed here.
func (h *AppsHandler) logCatalogErr(r *http.Request, op string, err error, kv ...any) {
	args := []any{
		"op", op,
		"path", r.URL.Path,
		slog.Any("error", err),
	}
	args = append(args, kv...)
	LoggerFromContext(r.Context()).Warn("apps handler: catalog call failed", args...)
}

// Compile-time guard: AppsHandler satisfies api.Handler.
var _ Handler = (*AppsHandler)(nil)
