package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/SUSE/aif/pkg/apps"
)

// AppsHandler serves the /api/v1/apps* REST endpoints. It depends on
// apps.Catalog (the read-only port; NOT the bootstrap-time
// apps.Aggregator) — the handler reads + filters; it does not register
// sources or start ticker goroutines. Routes are registered against a
// caller-supplied *http.ServeMux via Register, conforming to the
// api.Handler interface.
type AppsHandler struct {
	catalog apps.Catalog
	logger  *slog.Logger
}

// NewAppsHandler constructs an AppsHandler bound to the catalog port.
func NewAppsHandler(catalog apps.Catalog, logger *slog.Logger) *AppsHandler {
	return &AppsHandler{catalog: catalog, logger: logger}
}

// Register wires this handler's routes onto the provided mux. Three
// patterns are registered (Go 1.22+ method-prefixed ServeMux). The
// `/categories` literal route MUST be registered before the
// `/{id...}` wildcard so it wins for that exact path.
func (h *AppsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/apps", h.list)
	// Layer 3 will add: GET /api/v1/apps/{id...}
	// Layer 4 will add: GET /api/v1/apps/categories
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
// query parameter. Any value other than the literal string "true"
// (case-sensitive — matches the documented enum) is treated as false.
// Absent param defaults to false per ARCHITECTURE.md §5.
func parseIncludeReferenceBlueprints(r *http.Request) bool {
	return r.URL.Query().Get("includeReferenceBlueprints") == "true"
}

// Compile-time guard: AppsHandler satisfies api.Handler.
var _ Handler = (*AppsHandler)(nil)

// _ is an intentional reference to context to silence the unused-import
// linter when only Layer 2 is wired; Layers 3/4 add real ctx use.
var _ = context.Background
