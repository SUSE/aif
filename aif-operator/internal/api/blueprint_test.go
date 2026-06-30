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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newBlueprintHandler(t *testing.T) http.Handler {
	t.Helper()
	s := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(s).Build()
	mux := http.NewServeMux()
	NewBlueprintHandler(c).Register(mux)
	return mux
}

func TestListBlueprints_Empty(t *testing.T) {
	h := newBlueprintHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/blueprints", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateBlueprint(t *testing.T) {
	h := newBlueprintHandler(t)
	body := map[string]any{
		"spec": map[string]any{
			"displayName": "My AI Stack",
			"version":     "1.0.0",
			"description": "Test blueprint",
			"components": []any{
				map[string]any{
					"chartRepo":    "suse-ai",
					"chartName":    "ollama",
					"chartVersion": "1.0.0",
				},
			},
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var bp aiplatformv1alpha1.Blueprint
	if err := json.Unmarshal(w.Body.Bytes(), &bp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if bp.Name != "my-ai-stack-1-0-0" {
		t.Errorf("expected name my-ai-stack-1-0-0, got %q", bp.Name)
	}
	if bp.Labels[aiplatformv1alpha1.BlueprintNameLabel] != "my-ai-stack" {
		t.Errorf("missing blueprint-name label")
	}
	if bp.Labels[aiplatformv1alpha1.BlueprintVersionLabel] != "1.0.0" {
		t.Errorf("missing blueprint-version label")
	}
}

func TestGetBlueprint(t *testing.T) {
	h := newBlueprintHandler(t)

	// Create first
	body := map[string]any{
		"spec": map[string]any{
			"displayName": "Stack",
			"version":     "2.0.0",
			"components": []any{
				map[string]any{"chartRepo": "r", "chartName": "c", "chartVersion": "1.0.0"},
			},
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Get by derived name
	req2 := httptest.NewRequest("GET", "/api/v1/blueprints/stack-2-0-0", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestDeleteBlueprint(t *testing.T) {
	h := newBlueprintHandler(t)

	// Create first
	body := map[string]any{
		"spec": map[string]any{
			"displayName": "Stack",
			"version":     "3.0.0",
			"components": []any{
				map[string]any{"chartRepo": "r", "chartName": "c", "chartVersion": "1.0.0"},
			},
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Delete
	req2 := httptest.NewRequest("DELETE", "/api/v1/blueprints/stack-3-0-0", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestGetBlueprint_NotFound(t *testing.T) {
	h := newBlueprintHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/blueprints/does-not-exist", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteBlueprint_NotFound(t *testing.T) {
	h := newBlueprintHandler(t)
	req := httptest.NewRequest("DELETE", "/api/v1/blueprints/does-not-exist", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateBlueprint_WrongContentType(t *testing.T) {
	h := newBlueprintHandler(t)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateBlueprint_InvalidJSON(t *testing.T) {
	h := newBlueprintHandler(t)
	req := httptest.NewRequest("POST", "/api/v1/blueprints", strings.NewReader(`{"unknown_field": true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
