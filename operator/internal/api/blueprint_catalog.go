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
	"net/http"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BlueprintCatalogHandler serves the Catalog CRs (kind Catalog) as blueprint catalogs.
type BlueprintCatalogHandler struct {
	client client.Client
}

// NewBlueprintCatalogHandler constructs a BlueprintCatalogHandler.
func NewBlueprintCatalogHandler(c client.Client) *BlueprintCatalogHandler {
	return &BlueprintCatalogHandler{client: c}
}

// Register wires the handler's routes onto the mux.
func (h *BlueprintCatalogHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/catalogs", h.listCatalogs)
}

func (h *BlueprintCatalogHandler) listCatalogs(w http.ResponseWriter, r *http.Request) {
	var list aiplatformv1alpha1.CatalogList
	if err := h.client.List(r.Context(), &list); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for i := range list.Items {
		list.Items[i].ManagedFields = nil
	}
	writeJSON(w, http.StatusOK, &list)
}

// Compile-time guard: BlueprintCatalogHandler satisfies Handler.
var _ Handler = (*BlueprintCatalogHandler)(nil)
