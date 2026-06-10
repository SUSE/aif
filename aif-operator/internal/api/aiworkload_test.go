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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newAIWorkloadHandler(t *testing.T) http.Handler {
	t.Helper()
	s := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).
		Build()
	mux := http.NewServeMux()
	NewAIWorkloadHandler(c).Register(mux)
	return mux
}

func TestListAIWorkloads_Empty(t *testing.T) {
	h := newAIWorkloadHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/aiworkloads", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAIWorkload(t *testing.T) {
	h := newAIWorkloadHandler(t)
	body := map[string]any{
		"metadata": map[string]any{"name": "my-workload"},
		"spec": map[string]any{
			"displayName":     "My Workload",
			"targetNamespace": "my-ns",
			"deployStrategy":  "Helm",
			"source": map[string]any{
				"sourceType": "App",
				"app": map[string]any{
					"chartRepo":    "suse-ai",
					"chartName":    "ollama",
					"chartVersion": "1.0.0",
					"release":      "ollama",
				},
			},
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads",
		bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	_ = context.Background() // suppress unused import
}
