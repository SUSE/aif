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

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const blueprintFieldOwner = "aif-operator-api"

var nonAlphanumRE = regexp.MustCompile(`[^a-z0-9]+`)

// BlueprintHandler serves Blueprint CRUD endpoints.
type BlueprintHandler struct {
	client client.Client
}

// NewBlueprintHandler constructs a BlueprintHandler.
func NewBlueprintHandler(c client.Client) *BlueprintHandler {
	return &BlueprintHandler{client: c}
}

// Register wires the handler's routes onto the mux.
func (h *BlueprintHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/blueprints", h.listBlueprints)
	mux.HandleFunc("POST /api/v1/blueprints", h.createBlueprint)
	mux.HandleFunc("GET /api/v1/blueprints/{name}", h.getBlueprint)
	mux.HandleFunc("PUT /api/v1/blueprints/{name}", h.updateBlueprint)
	mux.HandleFunc("DELETE /api/v1/blueprints/{name}", h.deleteBlueprint)
}

func (h *BlueprintHandler) listBlueprints(w http.ResponseWriter, r *http.Request) {
	var list aiplatformv1alpha1.BlueprintList
	if err := h.client.List(r.Context(), &list); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for i := range list.Items {
		list.Items[i].ManagedFields = nil
	}
	writeJSON(w, http.StatusOK, &list)
}

type blueprintCreateBody struct {
	Spec aiplatformv1alpha1.BlueprintSpec `json:"spec"`
}

func (h *BlueprintHandler) createBlueprint(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidInput))
		return
	}

	var body blueprintCreateBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	if body.Spec.DisplayName == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: displayName is required", ErrInvalidInput))
		return
	}
	if body.Spec.Version == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: version is required", ErrInvalidInput))
		return
	}
	slug := slugifyBlueprintName(body.Spec.DisplayName)
	if slug == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: displayName must contain at least one alphanumeric character", ErrInvalidInput))
		return
	}
	crName := blueprintCRName(body.Spec.DisplayName, body.Spec.Version)

	bp := &aiplatformv1alpha1.Blueprint{}
	bp.APIVersion = "ai-platform.suse.com/v1alpha1"
	bp.Kind = "Blueprint"
	bp.Name = crName
	bp.Labels = map[string]string{
		aiplatformv1alpha1.BlueprintNameLabel:    slug,
		aiplatformv1alpha1.BlueprintVersionLabel: body.Spec.Version,
	}
	bp.Spec = body.Spec

	if err := h.client.Patch(
		r.Context(), bp, client.Apply,
		client.ForceOwnership,
		client.FieldOwner(blueprintFieldOwner),
	); err != nil {
		if errors.IsInvalid(err) {
			writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("%w: %v", ErrInvalidInput, err))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	bp.ManagedFields = nil
	writeJSON(w, http.StatusCreated, bp)
}

func (h *BlueprintHandler) getBlueprint(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var bp aiplatformv1alpha1.Blueprint
	if err := h.client.Get(r.Context(), client.ObjectKey{Name: name}, &bp); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: blueprint %q not found", ErrNotFound, name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	bp.ManagedFields = nil
	writeJSON(w, http.StatusOK, &bp)
}

func (h *BlueprintHandler) deleteBlueprint(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	bp := &aiplatformv1alpha1.Blueprint{}
	bp.Name = name
	if err := h.client.Delete(r.Context(), bp); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: blueprint %q not found", ErrNotFound, name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// blueprintCRName derives the CR name from display name and semver version.
// Build-metadata suffix (+...) is stripped since '+' is illegal in Kubernetes names.
// e.g. "My AI Stack", "1.0.0" → "my-ai-stack-1-0-0"
// e.g. "My AI Stack", "1.0.0+build.1" → "my-ai-stack-1-0-0"
func blueprintCRName(displayName, version string) string {
	// Strip build metadata (everything from '+' onward) before hyphenating.
	v := version
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	return slugifyBlueprintName(displayName) + "-" + strings.ReplaceAll(v, ".", "-")
}

// slugifyBlueprintName converts a display name to a DNS-safe slug.
func slugifyBlueprintName(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

type blueprintUpdateBody struct {
	Spec aiplatformv1alpha1.BlueprintSpec `json:"spec"`
}

func (h *BlueprintHandler) updateBlueprint(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidInput))
		return
	}

	var body blueprintUpdateBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	var bp aiplatformv1alpha1.Blueprint
	if err := h.client.Get(r.Context(), client.ObjectKey{Name: name}, &bp); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: blueprint %q not found", ErrNotFound, name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	bp.Spec = body.Spec
	if err := h.client.Update(r.Context(), &bp); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	bp.ManagedFields = nil
	writeJSON(w, http.StatusOK, &bp)
}

// Compile-time guard: BlueprintHandler satisfies Handler.
var _ Handler = (*BlueprintHandler)(nil)
