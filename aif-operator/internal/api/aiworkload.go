/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// aif-operator/internal/api/aiworkload.go
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const aiWorkloadFieldOwner = "aif-operator-api"

// AIWorkloadHandler serves AIWorkload CRUD endpoints.
type AIWorkloadHandler struct {
	client client.Client
}

// NewAIWorkloadHandler constructs an AIWorkloadHandler.
func NewAIWorkloadHandler(c client.Client) *AIWorkloadHandler {
	return &AIWorkloadHandler{client: c}
}

// Register wires the handler's routes onto the mux.
func (h *AIWorkloadHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/aiworkloads", h.listAIWorkloads)
	mux.HandleFunc("POST /api/v1/namespaces/{namespace}/aiworkloads", h.createAIWorkload)
	mux.HandleFunc("PATCH /api/v1/namespaces/{namespace}/aiworkloads/{name}", h.updateAIWorkload)
	mux.HandleFunc("DELETE /api/v1/namespaces/{namespace}/aiworkloads/{name}", h.deleteAIWorkload)
}

func (h *AIWorkloadHandler) listAIWorkloads(w http.ResponseWriter, r *http.Request) {
	var list aiplatformv1alpha1.AIWorkloadList
	if err := h.client.List(r.Context(), &list); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for i := range list.Items {
		list.Items[i].ManagedFields = nil
	}
	writeJSON(w, http.StatusOK, &list)
}

type aiWorkloadCreateBody struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec   aiplatformv1alpha1.AIWorkloadSpec    `json:"spec"`
	Status *aiplatformv1alpha1.AIWorkloadStatus `json:"status,omitempty"`
}

func (h *AIWorkloadHandler) createAIWorkload(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	if namespace == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: namespace is required", ErrInvalidInput))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidInput))
		return
	}

	var body aiWorkloadCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	// Ensure the target namespace exists before creating the namespaced AIWorkload CR.
	ns := &corev1.Namespace{}
	ns.APIVersion = "v1"
	ns.Kind = "Namespace"
	ns.Name = namespace
	if err := h.client.Patch(
		r.Context(), ns, client.Apply,
		client.ForceOwnership,
		client.FieldOwner(aiWorkloadFieldOwner),
	); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("failed to ensure namespace %q: %w", namespace, err))
		return
	}

	wl := &aiplatformv1alpha1.AIWorkload{}
	wl.APIVersion = "ai-platform.suse.com/v1alpha1"
	wl.Kind = "AIWorkload"
	wl.Name = body.Metadata.Name
	wl.Namespace = namespace
	wl.Spec = body.Spec

	if err := h.client.Create(r.Context(), wl); err != nil {
		if errors.IsAlreadyExists(err) {
			writeError(w, http.StatusConflict, fmt.Errorf(
				"deployment %q already exists in namespace %q — choose a different instance name or use Manage to update it",
				wl.Name, namespace,
			))
			return
		}
		if errors.IsInvalid(err) {
			writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("%w: %v", ErrInvalidInput, err))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if body.Status != nil {
		wl.Status = *body.Status
		if err := h.client.Status().Update(r.Context(), wl); err != nil {
			log.Printf("api: failed to set initial AIWorkload status %s/%s: %v", namespace, body.Metadata.Name, err)
		}
	}

	wl.ManagedFields = nil
	writeJSON(w, http.StatusCreated, wl)
}

func (h *AIWorkloadHandler) updateAIWorkload(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")
	if namespace == "" || name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: namespace and name are required", ErrInvalidInput))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidInput))
		return
	}

	var body aiWorkloadCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	existing := &aiplatformv1alpha1.AIWorkload{}
	if err := h.client.Get(r.Context(), types.NamespacedName{Namespace: namespace, Name: name}, existing); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("deployment %q not found in namespace %q", name, namespace))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	wl := &aiplatformv1alpha1.AIWorkload{}
	wl.APIVersion = "ai-platform.suse.com/v1alpha1"
	wl.Kind = "AIWorkload"
	wl.Name = name
	wl.Namespace = namespace
	wl.Spec = body.Spec

	if err := h.client.Patch(
		r.Context(), wl, client.Apply,
		client.ForceOwnership,
		client.FieldOwner(aiWorkloadFieldOwner),
	); err != nil {
		if errors.IsInvalid(err) {
			writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("%w: %v", ErrInvalidInput, err))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if body.Status != nil {
		wl.Status = *body.Status
		if err := h.client.Status().Update(r.Context(), wl); err != nil {
			log.Printf("api: failed to update AIWorkload status %s/%s: %v", namespace, name, err)
		}
	}

	wl.ManagedFields = nil
	writeJSON(w, http.StatusOK, wl)
}

func (h *AIWorkloadHandler) deleteAIWorkload(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")
	if namespace == "" || name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: namespace and name are required", ErrInvalidInput))
		return
	}

	wl := &aiplatformv1alpha1.AIWorkload{}
	wl.Name = name
	wl.Namespace = namespace

	if err := h.client.Delete(r.Context(), wl); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("deployment %q not found in namespace %q", name, namespace))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Compile-time guard.
var _ Handler = (*AIWorkloadHandler)(nil)
