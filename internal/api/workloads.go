package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/SUSE/aif/pkg/workload"
)

// semverRegex mirrors the CRD pattern on Blueprint.spec.version
// (^\d+\.\d+\.\d+$). Validated at the HTTP boundary so malformed input
// returns 400 INVALID_INPUT instead of the misleading 409 downgrade error
// that semver.Compare would otherwise emit for invalid strings.
var semverRegex = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// WorkloadsHandler serves the /api/v1/workloads/{namespace}/{name}/* REST
// endpoints. Today the only route is POST .../upgrade (P5-3). Future
// lifecycle actions (operate, scale, …) plug in via additional methods on
// this handler.
type WorkloadsHandler struct {
	upgrader workload.Upgrader
	logger   *slog.Logger
}

// NewWorkloadsHandler constructs a WorkloadsHandler bound to the upgrader
// workflow port. The logger here is the server-level logger; request-scoped
// loggers come from LoggerFromContext.
func NewWorkloadsHandler(upgrader workload.Upgrader, logger *slog.Logger) *WorkloadsHandler {
	return &WorkloadsHandler{upgrader: upgrader, logger: logger}
}

// Register wires this handler's routes onto the provided mux.
func (h *WorkloadsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/workloads/{namespace}/{name}/upgrade", h.upgrade)
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
	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	// Audit only — Impersonate-User is recorded in logs but not enforced.
	user, _ := ExtractUser(r)

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
