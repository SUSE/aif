package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/SUSE/aif/pkg/apps"
	"github.com/SUSE/aif/pkg/helm"
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
	catalog   apps.Catalog
	inspector helm.ChartInspector
}

// NewAppsHandler constructs an AppsHandler bound to the catalog port
// and the chart-inspection port. The inspector backs
// GET /api/v1/apps/{id}/values; the catalog backs the rest.
func NewAppsHandler(catalog apps.Catalog, inspector helm.ChartInspector) *AppsHandler {
	return &AppsHandler{catalog: catalog, inspector: inspector}
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
	mux.HandleFunc("GET /api/v1/apps/{id}/values", h.values)
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
		Source:                     r.URL.Query().Get("source"),
		Category:                   r.URL.Query().Get("category"),
		IncludeReferenceBlueprints: parseIncludeReferenceBlueprints(r),
	}

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
	if all == nil {
		all = []apps.App{}
	}
	writeJSON(w, http.StatusOK, all)
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
// path segment (e.g. "nvidia.ngc.nim-llm:1.0.0"). Returns the single App
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

// values serves GET /api/v1/apps/{id}/values?version={v}. Returns the
// chart's published default values.yaml + optional questions.yaml so
// the App Install wizard's Configuration step can render an editable
// view of the chart-as-published.
//
//	{ "values": { ... }, "questions": { ... } | null }
//
// Error mapping:
//
//	missing ?version              → 400 INVALID_INPUT
//	apps.ErrAppNotFound          → 404 NOT_FOUND
//	apps.ErrUnknownSource        → 400 INVALID_INPUT
//	inspector failures           → 500 INTERNAL_ERROR
//
// The version query param is required even though the App carries a
// ChartRef.Version: the wizard lets the user pick from
// availableVersions[], and we want the user's choice to drive the pull
// rather than the catalog snapshot.
func (h *AppsHandler) values(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	version := r.URL.Query().Get("version")
	if version == "" {
		err := fmt.Errorf("%w: query param 'version' is required", ErrInvalidInput)
		writeError(w, errorStatus(err), err)
		return
	}

	app, err := h.catalog.Get(r.Context(), id)
	if err != nil {
		mapped := mapCatalogErr(err, id)
		h.logCatalogErr(r, "values.Get", err, "id", id, "version", version)
		writeError(w, errorStatus(mapped), mapped)
		return
	}

	// Engine.DefaultValues expects the bare host/path (no oci:// scheme)
	// — same contract as Render. App.ChartRef.Repo is stored with the
	// scheme attached (see pkg/apps/nvidia_source.go), so strip it here
	// at the integration boundary.
	repo := strings.TrimPrefix(app.ChartRef.Repo, "oci://")
	chart := app.ChartRef.Chart

	values, questions, err := h.inspector.DefaultValues(r.Context(), repo, chart, version)
	if err != nil {
		LoggerFromContext(r.Context()).Warn("apps handler: chart inspect failed",
			slog.String("op", "values.DefaultValues"),
			slog.String("id", id),
			slog.String("version", version),
			slog.String("repo", repo),
			slog.String("chart", chart),
			slog.Any("error", err),
		)
		// Wrap as ErrInternal so the writeError envelope gets the
		// INTERNAL_ERROR code instead of leaking the raw helm error
		// classification. The Warn log above carries the underlying err.
		writeError(w, http.StatusInternalServerError,
			fmt.Errorf("%w: failed to inspect chart %s/%s:%s", ErrInternal, repo, chart, version))
		return
	}

	// questions is optional and serialized as null when absent so the UI
	// has a uniform shape to check.
	writeJSON(w, http.StatusOK, struct {
		Values    map[string]any `json:"values"`
		Questions map[string]any `json:"questions"`
	}{Values: values, Questions: questions})
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
