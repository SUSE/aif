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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetVersion(t *testing.T) {
	mux := http.NewServeMux()
	NewVersionHandler("1.2.3", "abc1234", "0.1.0-dev.3").Register(mux)

	req := httptest.NewRequest("GET", "/api/v1/version", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["version"] != "1.2.3" {
		t.Errorf("expected version %q, got %q", "1.2.3", body["version"])
	}
	if body["commit"] != "abc1234" {
		t.Errorf("expected commit %q, got %q", "abc1234", body["commit"])
	}
	if body["chartVersion"] != "0.1.0-dev.3" {
		t.Errorf("expected chartVersion %q, got %q", "0.1.0-dev.3", body["chartVersion"])
	}
}
