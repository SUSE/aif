package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	settingsNamespace  = "aif"
	settingsName       = "settings"
	settingsFieldOwner = "aif-operator-api"
)

// SettingsApplier is the port for asynchronous engine-settings propagation.
// Implemented by controller.SettingsReconciler's engine bus in production.
// The handler MUST NOT call this — it exists only for future-proofing tests
// to catch accidental synchronous Apply calls if the handler is later refactored.
type SettingsApplier interface {
	Apply(ctx context.Context, s SettingsSnapshot) error
}

// SettingsSnapshot is a subset of Settings spec fields needed by engines.
// Matches controller.SettingsSnapshot; duplicated here to avoid importing internal/controller.
type SettingsSnapshot struct {
	// Extend as needed when engines consume more fields
}

// SettingsHandler serves GET /api/v1/settings and PUT /api/v1/settings.
// The handler reads and writes the singleton Settings CR directly.
// Credential resolution and engine propagation are asynchronous — driven by
// the SettingsReconciler on the next reconcile loop (ARCHITECTURE.md §8.2.1).
// No goroutines are started here.
type SettingsHandler struct {
	client   client.Client
	applier  SettingsApplier // MUST remain nil in production; exists only for test guards
}

// NewSettingsHandler constructs a SettingsHandler bound to the K8s client.
// The optional applier parameter exists only for test injection to guard against
// future refactors that might accidentally call Apply synchronously. In production,
// pass nil — engine propagation is async via SettingsReconciler (ARCHITECTURE.md §8.2.1).
// All request-scoped logging uses LoggerFromContext — no logger field needed.
func NewSettingsHandler(c client.Client, applier SettingsApplier) *SettingsHandler {
	return &SettingsHandler{client: c, applier: applier}
}

// Register wires GET /api/v1/settings and PUT /api/v1/settings onto the mux.
func (h *SettingsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings", h.getSettings)
	mux.HandleFunc("PUT /api/v1/settings", h.putSettings)
}

// getSettings serves GET /api/v1/settings. Returns the singleton Settings CR
// JSON on 200 OK, or 404 when the CR does not exist.
func (h *SettingsHandler) getSettings(w http.ResponseWriter, r *http.Request) {
	var settings aifv1.Settings
	key := types.NamespacedName{Namespace: settingsNamespace, Name: settingsName}
	if err := h.client.Get(r.Context(), key, &settings); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: settings CR not found", ErrNotFound))
			return
		}
		LoggerFromContext(r.Context()).Warn("settings handler: Get failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, fmt.Errorf("%w: failed to read settings", ErrInternal))
		return
	}
	writeJSON(w, http.StatusOK, &settings)
}

// settingsPutBody is the request body shape for PUT /api/v1/settings.
type settingsPutBody struct {
	Spec aifv1.SettingsSpec `json:"spec"`
}

// putSettings serves PUT /api/v1/settings. Parses { "spec": {...} } from the
// request body, constructs a Settings CR with fixed metadata, and applies it
// via Server-Side Apply. Returns the updated CR JSON on 200 OK.
// Does NOT call any SettingsApplier — engine propagation is asynchronous,
// driven by the SettingsReconciler on the next reconcile loop.
func (h *SettingsHandler) putSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var body settingsPutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: invalid JSON: %v", ErrInvalidInput, err))
		return
	}

	settings := &aifv1.Settings{}
	settings.APIVersion = "ai.suse.com/v1alpha1"
	settings.Kind = "Settings"
	settings.Name = settingsName
	settings.Namespace = settingsNamespace
	settings.Spec = body.Spec

	// TODO: migrate to client.Client.Apply() once controller-gen produces
	// ApplyConfiguration types for aifv1.Settings (controller-runtime v0.23.3
	// deprecates the client.Apply Patch constant in favour of the typed API).
	if err := h.client.Patch(
		r.Context(), settings, client.Apply,
		client.ForceOwnership,
		client.FieldOwner(settingsFieldOwner),
	); err != nil {
		LoggerFromContext(r.Context()).Warn("settings handler: Apply failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, fmt.Errorf("%w: failed to save settings", ErrInternal))
		return
	}

	writeJSON(w, http.StatusOK, settings)
}

// Compile-time guard: SettingsHandler satisfies api.Handler.
var _ Handler = (*SettingsHandler)(nil)
