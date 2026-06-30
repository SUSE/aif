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
}

// NewVersionHandler constructs a VersionHandler.
func NewVersionHandler(version, commit, chartVersion string) *VersionHandler {
	return &VersionHandler{version: version, commit: commit, chartVersion: chartVersion}
}

// Register wires the handler's route onto the mux.
func (h *VersionHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/version", h.getVersion)
}

func (h *VersionHandler) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version":      h.version,
		"commit":       h.commit,
		"chartVersion": h.chartVersion,
	})
}

var _ Handler = (*VersionHandler)(nil)
