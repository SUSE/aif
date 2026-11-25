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

import "net/http"

// CapabilitiesHandler serves GET /api/v1/capabilities: what the caller may
// do beyond the route permissions, so the UI can offer only that.
type CapabilitiesHandler struct {
	authz *Authorizer
}

// NewCapabilitiesHandler constructs a CapabilitiesHandler.
func NewCapabilitiesHandler() *CapabilitiesHandler {
	return &CapabilitiesHandler{}
}

// Register wires the handler's route onto the mux.
func (h *CapabilitiesHandler) Register(a *Authorizer) {
	h.authz = a
	a.HandleFunc("GET /api/v1/capabilities", h.getCapabilities)
}

func (h *CapabilitiesHandler) getCapabilities(w http.ResponseWriter, r *http.Request) {
	advanced, err := h.authz.CanCustomizeAdvanced(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errAuthorizationCheckFailed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"advancedCustomization": advanced})
}

var _ Handler = (*CapabilitiesHandler)(nil)
