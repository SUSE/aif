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

// VersionHandler serves GET /api/v1/version.
type VersionHandler struct {
	version      string
	commit       string
	chartVersion string
	labsEnabled  bool
}

// NewVersionHandler constructs a VersionHandler.
func NewVersionHandler(version, commit, chartVersion string) *VersionHandler {
	return &VersionHandler{version: version, commit: commit, chartVersion: chartVersion}
}

// WithLabs records whether the Lab controllers run (--enable-labs), so the UI
// can show Labs as turned off instead of guessing from cluster objects.
func (h *VersionHandler) WithLabs(enabled bool) *VersionHandler {
	h.labsEnabled = enabled
	return h
}

// Register wires the handler's route onto the mux.
func (h *VersionHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/version", h.getVersion)
}

func (h *VersionHandler) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":      h.version,
		"commit":       h.commit,
		"chartVersion": h.chartVersion,
		"labsEnabled":  h.labsEnabled,
	})
}

var _ Handler = (*VersionHandler)(nil)
