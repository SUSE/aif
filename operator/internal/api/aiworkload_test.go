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
	corev1 "k8s.io/api/core/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testWorkloadNamespace = "aif-workloads"

func newAIWorkloadHandlerWithClient(t *testing.T) (http.Handler, client.Client) {
	t.Helper()
	s := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).
		Build()
	mux := http.NewServeMux()
	NewAIWorkloadHandler(c, testWorkloadNamespace).Register(mux)
	return mux, c
}

func newAIWorkloadHandler(t *testing.T) http.Handler {
	t.Helper()
	h, _ := newAIWorkloadHandlerWithClient(t)
	return h
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

func aiWorkloadBody(name, targetNS string) []byte {
	body := map[string]any{
		"metadata": map[string]any{"name": name},
		"spec": map[string]any{
			"displayName":     "My Workload",
			"targetNamespace": targetNS,
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
	return b
}

// TestCreateAIWorkload_StoredInWorkloadNamespace verifies the CR lands in the
// configured workload namespace regardless of the namespace in the request path,
// while Spec.TargetNamespace still carries the deployment target.
func TestCreateAIWorkload_StoredInWorkloadNamespace(t *testing.T) {
	h, c := newAIWorkloadHandlerWithClient(t)
	req := httptest.NewRequest("POST", "/api/v1/namespaces/some-target-ns/aiworkloads",
		bytes.NewReader(aiWorkloadBody("my-workload", "some-target-ns")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	got := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(),
		types.NamespacedName{Namespace: testWorkloadNamespace, Name: "my-workload"}, got); err != nil {
		t.Fatalf("expected CR in %q namespace: %v", testWorkloadNamespace, err)
	}
	if got.Spec.TargetNamespace != "some-target-ns" {
		t.Fatalf("Spec.TargetNamespace = %q, want %q", got.Spec.TargetNamespace, "some-target-ns")
	}

	// The path namespace must NOT hold the CR.
	stray := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(),
		types.NamespacedName{Namespace: "some-target-ns", Name: "my-workload"}, stray); err == nil {
		t.Fatalf("CR unexpectedly created in path namespace %q", "some-target-ns")
	}
}

// TestCreateAIWorkload_NamespaceAgnosticRoute verifies the new path-less create
// route also stores the CR in the workload namespace.
func TestCreateAIWorkload_NamespaceAgnosticRoute(t *testing.T) {
	h, c := newAIWorkloadHandlerWithClient(t)
	req := httptest.NewRequest("POST", "/api/v1/aiworkloads",
		bytes.NewReader(aiWorkloadBody("route-workload", "team-a")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	got := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(),
		types.NamespacedName{Namespace: testWorkloadNamespace, Name: "route-workload"}, got); err != nil {
		t.Fatalf("expected CR in %q namespace: %v", testWorkloadNamespace, err)
	}
}
