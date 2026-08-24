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

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newApplicationHandler(t *testing.T, objects ...client.Object) http.Handler {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	mux := http.NewServeMux()
	NewApplicationHandler(c).Register(mux)
	return mux
}

func testApplication(name, chart, source string) *aiplatformv1alpha1.Application {
	return &aiplatformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: aiplatformv1alpha1.ApplicationSpec{
			Chart:             aiplatformv1alpha1.ApplicationChart{Name: chart, SourceRef: source},
			CredentialProfile: aiplatformv1alpha1.ComponentVendorSUSE,
		},
	}
}

func TestListApplications(t *testing.T) {
	h := newApplicationHandler(t, testApplication("suse.ollama", "ollama", "application-collection"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	response := httptest.NewRecorder()

	h.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var list aiplatformv1alpha1.ApplicationList
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "suse.ollama" {
		t.Fatalf("unexpected applications: %#v", list.Items)
	}
}

func TestGetApplication(t *testing.T) {
	h := newApplicationHandler(t, testApplication("suse.ollama", "ollama", "application-collection"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/suse.ollama", nil)
	response := httptest.NewRecorder()

	h.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var application aiplatformv1alpha1.Application
	if err := json.Unmarshal(response.Body.Bytes(), &application); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if application.Spec.Chart.Name != "ollama" || application.Spec.Chart.SourceRef != "application-collection" {
		t.Fatalf("unexpected application: %#v", application.Spec)
	}
}

func TestGetApplicationNotFound(t *testing.T) {
	h := newApplicationHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/missing", nil)
	response := httptest.NewRecorder()

	h.ServeHTTP(response, req)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", response.Code, response.Body.String())
	}
}
