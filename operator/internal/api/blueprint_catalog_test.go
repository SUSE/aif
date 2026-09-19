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
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestListCatalogs(t *testing.T) {
	s := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	suseDefault := &aiplatformv1alpha1.Catalog{}
	suseDefault.Name = "suse-default"
	suseDefault.Spec = aiplatformv1alpha1.CatalogSpec{
		DisplayName: "SUSE Default",
		Blueprints: []aiplatformv1alpha1.CatalogMember{
			{Name: "my-ai-stack", Featured: true},
			{Name: "rag-stack"},
		},
	}

	partnerAcme := &aiplatformv1alpha1.Catalog{}
	partnerAcme.Name = "partner-acme"
	partnerAcme.Spec = aiplatformv1alpha1.CatalogSpec{
		DisplayName: "Partner ACME",
		Blueprints: []aiplatformv1alpha1.CatalogMember{
			{Name: "acme-rag"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(suseDefault, partnerAcme).Build()
	mux := http.NewServeMux()
	NewBlueprintCatalogHandler(c).Register(mux)

	req := httptest.NewRequest("GET", "/api/v1/catalogs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var list aiplatformv1alpha1.CatalogList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 catalogs, got %d", len(list.Items))
	}

	names := map[string]aiplatformv1alpha1.Catalog{}
	for _, item := range list.Items {
		names[item.Name] = item
	}
	if _, ok := names["suse-default"]; !ok {
		t.Errorf("expected suse-default catalog in items, got %+v", names)
	}
	if _, ok := names["partner-acme"]; !ok {
		t.Errorf("expected partner-acme catalog in items, got %+v", names)
	}
	if got := names["suse-default"].Spec.Blueprints; len(got) != 2 {
		t.Errorf("expected suse-default membership list of 2, got %+v", got)
	}

	// Sanity check the raw body also carries the membership names, guarding
	// against a serialization regression that json.Unmarshal alone wouldn't catch.
	if !strings.Contains(w.Body.String(), "my-ai-stack") {
		t.Errorf("expected response body to contain membership name my-ai-stack, got %s", w.Body.String())
	}
}

func TestListCatalogs_Empty(t *testing.T) {
	s := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(s).Build()
	mux := http.NewServeMux()
	NewBlueprintCatalogHandler(c).Register(mux)

	req := httptest.NewRequest("GET", "/api/v1/catalogs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
