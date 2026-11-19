package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/blueprintpolicy"
)

// Request annotation names — MUST match the controller package literals (Plan 2/4).
const (
	upgradeRequestAnnotation  = "ai-factory.suse.com/upgrade-request"
	rollbackRequestAnnotation = "ai-factory.suse.com/rollback-request"
	retryRequestAnnotation    = "ai-factory.suse.com/retry-request"
	operationAnnotation       = "ai-factory.suse.com/operation"
)

// newNonce returns a unique, URL-safe token.
func newNonce() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// operationInProgress reports whether an operation is currently non-terminal, checking both the
// projected status and the authoritative journal annotation (which may lead status by a beat).
func operationInProgress(w *aiplatformv1alpha1.AIWorkload) bool {
	if w.Status.ActiveOperation != nil && w.Status.ActiveOperation.State == aiplatformv1alpha1.OperationStateInProgress {
		return true
	}
	if s := w.Annotations[operationAnnotation]; s != "" {
		var op aiplatformv1alpha1.AIWorkloadOperation
		if err := json.Unmarshal([]byte(s), &op); err == nil && op.State == aiplatformv1alpha1.OperationStateInProgress {
			return true
		}
	}
	return false
}

// setRequestAnnotation fetches the workload and merge-patches a single request annotation.
// Returns the fetched workload (post-set) or writes the HTTP error itself and returns nil.
func (h *AIWorkloadHandler) setRequestAnnotation(w http.ResponseWriter, r *http.Request, annotation, value string) *aiplatformv1alpha1.AIWorkload {
	wl := h.getActionWorkload(w, r)
	if wl == nil {
		return nil
	}
	// Recovery operations (upgrade/rollback/retry) are Blueprint-only: startVersionOp cannot change
	// an App source and App reconcile never populates deployedSource, so an accepted App-sourced
	// operation would sit InProgress until timeout. Reject it at the edge instead.
	if wl.Spec.Source.SourceType != aiplatformv1alpha1.AIWorkloadSourceBlueprint {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: recovery operations are only supported for Blueprint-sourced workloads", ErrInvalidInput))
		return nil
	}
	base := wl.DeepCopy()
	if wl.Annotations == nil {
		wl.Annotations = map[string]string{}
	}
	wl.Annotations[annotation] = value
	if err := h.client.Patch(r.Context(), wl, client.MergeFrom(base)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return nil
	}
	return wl
}

func (h *AIWorkloadHandler) upgradeAIWorkload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TargetVersion string `json:"targetVersion"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}
	if body.TargetVersion == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: targetVersion is required", ErrInvalidInput))
		return
	}
	wl := h.getActionWorkload(w, r)
	if wl == nil {
		return
	}
	if src := wl.Spec.Source.Blueprint; src != nil && wl.Status.DeployedSource != nil {
		blocked, err := blueprintpolicy.RollbackBlocked(r.Context(), h.client, src.Name, wl.Status.DeployedSource.Version, body.TargetVersion)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if blocked {
			writeError(w, http.StatusBadRequest, rollbackBlockedError(body.TargetVersion, wl.Status.DeployedSource.Version))
			return
		}
	}
	if wl := h.setRequestAnnotation(w, r, upgradeRequestAnnotation, newNonce()+"|"+body.TargetVersion); wl != nil {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "targetVersion": body.TargetVersion})
	}
}

// getActionWorkload fetches the path's workload, or writes the HTTP error itself and returns nil.
func (h *AIWorkloadHandler) getActionWorkload(w http.ResponseWriter, r *http.Request) *aiplatformv1alpha1.AIWorkload {
	namespace, name := r.PathValue("namespace"), r.PathValue("name")
	if namespace == "" || name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: namespace and name are required", ErrInvalidInput))
		return nil
	}
	wl := &aiplatformv1alpha1.AIWorkload{}
	if err := h.client.Get(r.Context(), types.NamespacedName{Namespace: namespace, Name: name}, wl); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("deployment %q not found in namespace %q", name, namespace))
			return nil
		}
		writeError(w, http.StatusInternalServerError, err)
		return nil
	}
	return wl
}

func (h *AIWorkloadHandler) rollbackAIWorkload(w http.ResponseWriter, r *http.Request) {
	wl := h.getActionWorkload(w, r)
	if wl == nil {
		return
	}
	disabled := false
	var err error
	if src := wl.Spec.Source.Blueprint; src != nil {
		deployed := ""
		if wl.Status.DeployedSource != nil {
			deployed = wl.Status.DeployedSource.Version
		}
		disabled, err = blueprintpolicy.RollbackDisabled(r.Context(), h.client, src.Name, src.Version, deployed)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if disabled {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: rollback is disabled for this Blueprint", ErrInvalidInput))
		return
	}
	if wl := h.setRequestAnnotation(w, r, rollbackRequestAnnotation, newNonce()); wl != nil {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}

func (h *AIWorkloadHandler) retryAIWorkload(w http.ResponseWriter, r *http.Request) {
	wl := h.getActionWorkload(w, r)
	if wl == nil {
		return
	}
	if operationInProgress(wl) {
		writeError(w, http.StatusConflict, fmt.Errorf("%w: an operation is already in progress", ErrInvalidInput))
		return
	}
	if wl := h.setRequestAnnotation(w, r, retryRequestAnnotation, newNonce()); wl != nil {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}
