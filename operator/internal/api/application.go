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
	"fmt"
	"net/http"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplicationHandler exposes the logical Application definitions needed by
// the UI to render an Application-backed Blueprint. Mutation stays on the
// Kubernetes API so normal admission, RBAC, and GitOps workflows remain the
// source of truth.
type ApplicationHandler struct {
	client client.Client
}

// NewApplicationHandler constructs a read-only Application handler.
func NewApplicationHandler(c client.Client) *ApplicationHandler {
	return &ApplicationHandler{client: c}
}

// Register wires the Application read routes onto the mux.
func (h *ApplicationHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/applications", h.listApplications)
	mux.HandleFunc("GET /api/v1/applications/{name}", h.getApplication)
}

func (h *ApplicationHandler) listApplications(w http.ResponseWriter, r *http.Request) {
	var list aiplatformv1alpha1.ApplicationList
	if err := h.client.List(r.Context(), &list); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for i := range list.Items {
		list.Items[i].ManagedFields = nil
	}
	writeJSON(w, http.StatusOK, &list)
}

func (h *ApplicationHandler) getApplication(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var application aiplatformv1alpha1.Application
	if err := h.client.Get(r.Context(), client.ObjectKey{Name: name}, &application); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: application %q not found", ErrNotFound, name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	application.ManagedFields = nil
	writeJSON(w, http.StatusOK, &application)
}

var _ Handler = (*ApplicationHandler)(nil)
