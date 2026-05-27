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

// workloadReader is the consumer-defined read port. Satisfied by
// *workload.k8sRepository and *workload.FakeRepository in tests.
// ≤4 methods (ISP).
type workloadReader interface {
	List(ctx context.Context, namespace string, selector labels.Selector) ([]aifv1.Workload, error)
	Get(ctx context.Context, namespace, name string) (*aifv1.Workload, error)
}

// workloadMutator is the consumer-defined write port. Satisfied by
// *workload.k8sRepository (after Task F-2 adds Create/Delete) and
// *workload.FakeRepository in tests.
// ≤4 methods (ISP).
type workloadMutator interface {
	Create(ctx context.Context, w *aifv1.Workload) error
	Delete(ctx context.Context, namespace, name string) error
	Patch(ctx context.Context, w, orig *aifv1.Workload) error
}

// WorkloadsHandler serves the /api/v1/workloads/{namespace}/{name}/* REST
// endpoints. Today the only route is POST .../upgrade (P5-3). Future
// lifecycle actions (operate, scale, …) plug in via additional methods on
// this handler.
type WorkloadsHandler struct {
	upgrader workload.Upgrader
	reader   workloadReader
	mutator  workloadMutator
	logger   *slog.Logger
}

// NewWorkloadsHandler constructs a WorkloadsHandler bound to the upgrader
// workflow port. The logger here is the server-level logger; request-scoped
// loggers come from LoggerFromContext.
func NewWorkloadsHandler(upgrader workload.Upgrader, reader workloadReader, mutator workloadMutator, logger *slog.Logger) *WorkloadsHandler {
	return &WorkloadsHandler{upgrader: upgrader, reader: reader, mutator: mutator, logger: logger}
}

// Register wires this handler's routes onto the provided mux.
func (h *WorkloadsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workloads", h.list)
	mux.HandleFunc("GET /api/v1/workloads/{namespace}/{name}", h.getWorkload)
	mux.HandleFunc("POST /api/v1/workloads", h.createWorkload)
	mux.HandleFunc("DELETE /api/v1/workloads/{namespace}/{name}", h.deleteWorkload)
	mux.HandleFunc("PATCH /api/v1/workloads/{namespace}/{name}", h.patchWorkload)
	mux.HandleFunc("POST /api/v1/workloads/{namespace}/{name}/upgrade", h.upgrade)
}

func (h *WorkloadsHandler) list(w http.ResponseWriter, r *http.Request) {
	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}
	items, err := h.reader.List(r.Context(), "", nil)
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
	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req createWorkloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: invalid request body: %v", ErrInvalidInput, err))
		return
	}
	if req.Metadata.Name == "" || req.Metadata.Namespace == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: metadata.name and metadata.namespace are required", ErrInvalidInput))
		return
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

type patchWorkloadRequest struct {
	Spec aifv1.WorkloadSpec `json:"spec"`
}

func (h *WorkloadsHandler) patchWorkload(w http.ResponseWriter, r *http.Request) {
	user, _ := ExtractUser(r)
	if user == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%w: Impersonate-User header missing", ErrForbidden))
		return
	}

	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req patchWorkloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
		LoggerFromContext(r.Context()).Error("patch workload failed", "ns", ns, "name", name, "error", err)
		writeError(w, http.StatusInternalServerError, ErrInternal)
		return
	}

	LoggerFromContext(r.Context()).Info("workload patched", "namespace", ns, "name", name, "user", user)
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
