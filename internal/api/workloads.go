package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/workload"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
)

// semverRegex mirrors the CRD pattern on Blueprint.spec.version
// (^\d+\.\d+\.\d+$). Validated at the HTTP boundary so malformed input
// returns 400 INVALID_INPUT instead of the misleading 409 downgrade error
// that semver.Compare would otherwise emit for invalid strings.
var semverRegex = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// workloadMutator is consumer-defined (rather than reusing workload.Writer)
// to keep both ports ≤4 methods (ISP). Writer holds Patch only; this port
// adds Create + Delete needed by the HTTP CRUD handlers without growing
// Writer beyond its current shape (Update/UpdateStatus/Patch — already at
// 3 methods serving the reconciler + upgrader).
// Satisfied by *workload.k8sRepository and *workload.FakeRepository in tests.
type workloadMutator interface {
	Create(ctx context.Context, w *aifv1.Workload) error
	Delete(ctx context.Context, namespace, name string) error
	Patch(ctx context.Context, w, orig *aifv1.Workload) error
}

// WorkloadsHandler serves the /api/v1/workloads/{namespace}/{name}/* REST
// endpoints. Today the only route is POST .../upgrade (P5-3). Future
// lifecycle actions (operate, scale, …) plug in via additional methods on
// this handler.
//
// authMiddleware/checker may both be nil; when nil, SAR enforcement is
// skipped (useful for tests that drive the handler directly without
// authorization concerns). Production wiring in cmd/operator always supplies
// the SAR-backed checker.
type WorkloadsHandler struct {
	upgrader       workload.Upgrader
	reader         workload.Reader
	mutator        workloadMutator
	authMiddleware *AuthMiddleware
	checker        AuthChecker
	logger         *slog.Logger
}

// NewWorkloadsHandler constructs a WorkloadsHandler bound to the upgrader
// workflow port. The logger here is the server-level logger; request-scoped
// loggers come from LoggerFromContext.
//
// checker may be nil — see type doc. When non-nil, the handler wraps each
// CRUD route in a SAR check via RequireResource (and, for create, calls
// checker.CheckResource directly inside the handler since the namespace
// lives in the request body, not the URL).
func NewWorkloadsHandler(upgrader workload.Upgrader, reader workload.Reader, mutator workloadMutator, checker AuthChecker, logger *slog.Logger) *WorkloadsHandler {
	h := &WorkloadsHandler{
		upgrader: upgrader,
		reader:   reader,
		mutator:  mutator,
		checker:  checker,
		logger:   logger,
	}
	if checker != nil {
		h.authMiddleware = NewAuthMiddleware(checker)
	}
	return h
}

// Register wires this handler's routes onto the provided mux. Each CRUD
// route gets a SAR check; the create handler does its check inline since
// the namespace comes from the request body.
func (h *WorkloadsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workloads", h.guard("list", queryNamespace, h.list))
	mux.HandleFunc("GET /api/v1/workloads/{namespace}/{name}", h.guard("get", pathNamespace, h.getWorkload))
	mux.HandleFunc("POST /api/v1/workloads", h.createWorkload)
	mux.HandleFunc("DELETE /api/v1/workloads/{namespace}/{name}", h.guard("delete", pathNamespace, h.deleteWorkload))
	mux.HandleFunc("PUT /api/v1/workloads/{namespace}/{name}", h.guard("update", pathNamespace, h.putWorkload))
	mux.HandleFunc("POST /api/v1/workloads/{namespace}/{name}/upgrade", h.upgrade)
}

// guard wraps next in a SAR check for verb on "workloads" using selector to
// derive the namespace. When the handler has no checker (test setups), the
// wrapper is a no-op — handlers still self-check that Impersonate-User is
// present so the existing 403-on-missing-user contract is preserved.
func (h *WorkloadsHandler) guard(verb string, selector ResourceSelector, next http.HandlerFunc) http.HandlerFunc {
	if h.authMiddleware == nil {
		return next
	}
	return h.authMiddleware.RequireResource(verb, "workloads", selector, next)
}

// pathNamespace pulls the namespace from the {namespace} URL path segment.
func pathNamespace(r *http.Request) string { return r.PathValue("namespace") }

// queryNamespace pulls the namespace from the ?namespace= query parameter.
// Empty string means cluster-wide list — the SAR for "list workloads with
// empty namespace" is the right shape for that case.
func queryNamespace(r *http.Request) string { return r.URL.Query().Get("namespace") }

func (h *WorkloadsHandler) list(w http.ResponseWriter, r *http.Request) {
	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}
	ns := r.URL.Query().Get("namespace")
	selStr := r.URL.Query().Get("labelSelector")
	var selector labels.Selector
	if selStr != "" {
		parsed, err := labels.Parse(selStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%w: invalid labelSelector: %v", ErrInvalidInput, err))
			return
		}
		selector = parsed
	}
	items, err := h.reader.List(r.Context(), ns, selector)
	if err != nil {
		LoggerFromContext(r.Context()).Error("list workloads failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *WorkloadsHandler) getWorkload(w http.ResponseWriter, r *http.Request) {
	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}
	ns := r.PathValue("namespace")
	name := r.PathValue("name")
	wl, err := h.reader.Get(r.Context(), ns, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, ErrNotFound)
			return
		}
		LoggerFromContext(r.Context()).Error("get workload failed", "ns", ns, "name", name, "error", err)
		writeError(w, http.StatusInternalServerError, ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, wl)
}

func (h *WorkloadsHandler) deleteWorkload(w http.ResponseWriter, r *http.Request) {
	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}
	ns := r.PathValue("namespace")
	name := r.PathValue("name")
	if err := h.mutator.Delete(r.Context(), ns, name); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, ErrNotFound)
			return
		}
		LoggerFromContext(r.Context()).Error("delete workload failed", "ns", ns, "name", name, "error", err)
		writeError(w, http.StatusInternalServerError, ErrInternal)
		return
	}
	LoggerFromContext(r.Context()).Info("workload deleted", "namespace", ns, "name", name, "user", user)
	w.WriteHeader(http.StatusNoContent)
}

type createWorkloadRequest struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec aifv1.WorkloadSpec `json:"spec"`
}

func (h *WorkloadsHandler) createWorkload(w http.ResponseWriter, r *http.Request) {
	user, groups := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req createWorkloadRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: invalid request body: %v", ErrInvalidInput, err))
		return
	}
	if req.Metadata.Name == "" || req.Metadata.Namespace == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: metadata.name and metadata.namespace are required", ErrInvalidInput))
		return
	}

	// SAR check happens inline (rather than via RequireResource middleware)
	// because the namespace lives in the request body, not the URL path.
	if h.checker != nil {
		allowed, err := h.checker.CheckResource(r.Context(), user, groups, req.Metadata.Namespace, "create", "workloads")
		if err != nil {
			LoggerFromContext(r.Context()).Error("create workload SAR failed", "error", err)
			writeError(w, http.StatusInternalServerError, ErrInternal)
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, errInsufficientPermissions)
			return
		}
	}

	wl := &aifv1.Workload{}
	wl.Name = req.Metadata.Name
	wl.Namespace = req.Metadata.Namespace
	wl.Spec = req.Spec
	// WorkloadSpec.Name is a required display-name field (MinLength=1). Default
	// to metadata.name if the caller omits it.
	if wl.Spec.Name == "" {
		wl.Spec.Name = req.Metadata.Name
	}

	if err := h.mutator.Create(r.Context(), wl); err != nil {
		if apierrors.IsAlreadyExists(err) {
			writeError(w, http.StatusConflict, ErrConflict)
			return
		}
		LoggerFromContext(r.Context()).Error("create workload failed", "name", wl.Name, "namespace", wl.Namespace, "error", err)
		writeError(w, http.StatusInternalServerError, ErrInternal)
		return
	}

	LoggerFromContext(r.Context()).Info("workload created", "namespace", wl.Namespace, "name", wl.Name, "user", user)
	writeJSON(w, http.StatusCreated, wl)
}

type putWorkloadRequest struct {
	Spec aifv1.WorkloadSpec `json:"spec"`
}

// putWorkload replaces the spec wholesale (PUT semantics). Callers MUST send
// the complete spec; omitted fields are zeroed out (except spec.name which
// falls back to the existing display name for ergonomics). For partial
// updates, use a dedicated PATCH endpoint when one is added.
func (h *WorkloadsHandler) putWorkload(w http.ResponseWriter, r *http.Request) {
	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}

	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req putWorkloadRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: invalid request body: %v", ErrInvalidInput, err))
		return
	}

	orig, err := h.reader.Get(r.Context(), ns, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, ErrNotFound)
			return
		}
		LoggerFromContext(r.Context()).Error("get workload failed", "ns", ns, "name", name, "error", err)
		writeError(w, http.StatusInternalServerError, ErrInternal)
		return
	}

	patched := orig.DeepCopy()
	patched.Spec = req.Spec
	// Preserve the required display-name field — callers sending a partial spec
	// should not accidentally zero it out.
	if patched.Spec.Name == "" {
		patched.Spec.Name = orig.Spec.Name
	}

	if err := h.mutator.Patch(r.Context(), patched, orig); err != nil {
		if apierrors.IsConflict(err) {
			writeError(w, http.StatusConflict, ErrConflict)
			return
		}
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, ErrNotFound)
			return
		}
		LoggerFromContext(r.Context()).Error("put workload failed", "ns", ns, "name", name, "error", err)
		writeError(w, http.StatusInternalServerError, ErrInternal)
		return
	}

	LoggerFromContext(r.Context()).Info("workload updated", "namespace", ns, "name", name, "user", user)
	writeJSON(w, http.StatusOK, patched)
}

type upgradeRequest struct {
	ToBlueprintVersion string `json:"toBlueprintVersion"`
}

type upgradeResponse struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	BlueprintName string `json:"blueprintName"`
	OldVersion    string `json:"oldVersion"`
	NewVersion    string `json:"newVersion"`
}

func (h *WorkloadsHandler) upgrade(w http.ResponseWriter, r *http.Request) {
	// Path values pass through verbatim to the upgrader; malformed
	// namespace/name strings surface as apierrors at the K8s boundary
	// (ErrInternal/500 via mapUpgradeErr's default arm). Mirrors the
	// publish handler's behavior.
	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body upgradeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: invalid request body", ErrInvalidInput))
		return
	}
	if !semverRegex.MatchString(body.ToBlueprintVersion) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: toBlueprintVersion must match \\d+\\.\\d+\\.\\d+", ErrInvalidInput))
		return
	}

	result, err := h.upgrader.Upgrade(r.Context(), ns, name, body.ToBlueprintVersion, user)
	if err != nil {
		mapped := mapUpgradeErr(err)
		LoggerFromContext(r.Context()).Warn("workload upgrade failed",
			"namespace", ns, "name", name,
			"toBlueprintVersion", body.ToBlueprintVersion,
			"user", user,
			"error", err,
		)
		writeError(w, errorStatus(mapped), &APIError{
			Code:    errorCode(mapped),
			Message: err.Error(),
		})
		return
	}

	LoggerFromContext(r.Context()).Info("workload upgraded",
		"namespace", ns, "name", name,
		"oldVersion", result.OldVersion, "newVersion", result.NewVersion,
		"user", user,
	)

	writeJSON(w, http.StatusOK, upgradeResponse{
		Namespace:     result.Namespace,
		Name:          result.Name,
		BlueprintName: result.BlueprintName,
		OldVersion:    result.OldVersion,
		NewVersion:    result.NewVersion,
	})
}

// mapUpgradeErr translates a pkg/workload upgrade sentinel into the
// corresponding internal/api sentinel. The original error is preserved as
// the message so AC-verbatim strings ("Cross-lineage upgrade not allowed",
// "Cannot upgrade to a Withdrawn Blueprint version", "Upgrade must target a
// higher version (downgrade is not supported in v1)") reach the caller.
// Status comes from errorStatus(mapped) — no duplication.
func mapUpgradeErr(err error) error {
	switch {
	case errors.Is(err, workload.ErrWorkloadNotFound):
		return ErrNotFound
	case errors.Is(err, workload.ErrSourceNotBlueprint):
		return ErrInvalidInput
	case errors.Is(err, workload.ErrBlueprintVersionNotFound):
		return ErrNotFound
	case errors.Is(err, workload.ErrCrossLineageUpgrade):
		return ErrInvalidInput
	case errors.Is(err, workload.ErrTargetWithdrawn):
		return ErrInvalidTransition
	case errors.Is(err, workload.ErrDowngradeNotSupported):
		return ErrInvalidTransition
	case errors.Is(err, workload.ErrUpgradeConflict):
		return ErrConflict
	default:
		return ErrInternal
	}
}

var _ Handler = (*WorkloadsHandler)(nil)
