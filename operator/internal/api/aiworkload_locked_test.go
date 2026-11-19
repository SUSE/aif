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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestLockedValues_ChangeRejected(t *testing.T) {
	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "runai-1-0-0",
		},
		Spec: aiplatformv1alpha1.BlueprintSpec{
			Components: []aiplatformv1alpha1.BlueprintComponent{
				{
					ChartName: "runai",
					Values: &apixv1.JSON{
						Raw: []byte(`{"global":{"domain":"aif:auto-domain:runai"}}`),
					},
				},
			},
		},
	}
	wl := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wl",
			Namespace: "default",
		},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "Test",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint: &aiplatformv1alpha1.BlueprintSource{
					Name:    "runai",
					Version: "1.0.0",
				},
			},
			TargetNamespace: "runai",
			TargetClusters:  []string{"local"},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			ResolvedValues: []aiplatformv1alpha1.ResolvedValue{
				{
					ComponentName: "runai",
					Path:          "global.domain",
					Value:         "runai.10.0.0.12.sslip.io",
					Source:        "auto",
				},
			},
		},
	}

	scheme := aiworkloadTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bp, wl).WithStatusSubresource(wl).Build()
	h := NewAIWorkloadHandler(c)

	updateBody := aiWorkloadCreateBody{
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "Test",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint: &aiplatformv1alpha1.BlueprintSource{
					Name:    "runai",
					Version: "1.0.0",
				},
			},
			TargetNamespace: "runai",
			TargetClusters:  []string{"local"},
			ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{
				{
					ComponentName: "runai",
					Values: &apixv1.JSON{
						Raw: []byte(`{"global":{"domain":"other.example.com"}}`),
					},
				},
			},
		},
	}

	body, _ := json.Marshal(updateBody)
	req := httptest.NewRequest("PATCH", "/api/v1/namespaces/default/aiworkloads/test-wl", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("namespace", "default")
	req.SetPathValue("name", "test-wl")

	rec := httptest.NewRecorder()
	h.updateAIWorkload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	respBody := rec.Body.String()
	if !strings.Contains(respBody, "can't be changed after install") {
		t.Errorf("response missing expected error text: %s", respBody)
	}
	if !strings.Contains(respBody, "global.domain") {
		t.Errorf("response missing locked path: %s", respBody)
	}
}

func TestLockedValues_SameValueAllowed(t *testing.T) {
	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "runai-1-0-0",
		},
		Spec: aiplatformv1alpha1.BlueprintSpec{
			Components: []aiplatformv1alpha1.BlueprintComponent{
				{
					ChartName: "runai",
					Values: &apixv1.JSON{
						Raw: []byte(`{"global":{"domain":"aif:auto-domain:runai"}}`),
					},
				},
			},
		},
	}
	wl := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wl",
			Namespace: "default",
		},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "Test",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint: &aiplatformv1alpha1.BlueprintSource{
					Name:    "runai",
					Version: "1.0.0",
				},
			},
			TargetNamespace: "runai",
			TargetClusters:  []string{"local"},
			ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{
				{
					ComponentName: "runai",
					Values: &apixv1.JSON{
						Raw: []byte(`{"global":{"domain":"runai.10.0.0.12.sslip.io"}}`),
					},
				},
			},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			ResolvedValues: []aiplatformv1alpha1.ResolvedValue{
				{
					ComponentName: "runai",
					Path:          "global.domain",
					Value:         "runai.10.0.0.12.sslip.io",
					Source:        "user",
				},
			},
		},
	}

	scheme := aiworkloadTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bp, wl).WithStatusSubresource(wl).Build()
	h := NewAIWorkloadHandler(c)

	updateBody := aiWorkloadCreateBody{
		Spec: wl.Spec,
	}

	body, _ := json.Marshal(updateBody)
	req := httptest.NewRequest("PATCH", "/api/v1/namespaces/default/aiworkloads/test-wl", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("namespace", "default")
	req.SetPathValue("name", "test-wl")

	rec := httptest.NewRecorder()
	h.updateAIWorkload(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLockedValues_RemovingOverrideKeepsPin(t *testing.T) {
	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "runai-1-0-0",
		},
		Spec: aiplatformv1alpha1.BlueprintSpec{
			Components: []aiplatformv1alpha1.BlueprintComponent{
				{
					ChartName: "runai",
					Values: &apixv1.JSON{
						Raw: []byte(`{"global":{"domain":"aif:auto-domain:runai"}}`),
					},
				},
			},
		},
	}
	wl := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wl",
			Namespace: "default",
		},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "Test",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint: &aiplatformv1alpha1.BlueprintSource{
					Name:    "runai",
					Version: "1.0.0",
				},
			},
			TargetNamespace: "runai",
			TargetClusters:  []string{"local"},
			ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{
				{
					ComponentName: "runai",
					Values: &apixv1.JSON{
						Raw: []byte(`{"global":{"domain":"custom.example.com"}}`),
					},
				},
			},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			ResolvedValues: []aiplatformv1alpha1.ResolvedValue{
				{
					ComponentName: "runai",
					Path:          "global.domain",
					Value:         "custom.example.com",
					Source:        "user",
				},
			},
		},
	}

	scheme := aiworkloadTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bp, wl).WithStatusSubresource(wl).Build()
	h := NewAIWorkloadHandler(c)

	updateBody := aiWorkloadCreateBody{
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "Test",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint: &aiplatformv1alpha1.BlueprintSource{
					Name:    "runai",
					Version: "1.0.0",
				},
			},
			TargetNamespace: "runai",
			TargetClusters:  []string{"local"},
		},
	}

	body, _ := json.Marshal(updateBody)
	req := httptest.NewRequest("PATCH", "/api/v1/namespaces/default/aiworkloads/test-wl", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("namespace", "default")
	req.SetPathValue("name", "test-wl")

	rec := httptest.NewRecorder()
	h.updateAIWorkload(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLockedValues_NoPinsNoCheck(t *testing.T) {
	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "runai-1-0-0",
		},
		Spec: aiplatformv1alpha1.BlueprintSpec{
			Components: []aiplatformv1alpha1.BlueprintComponent{
				{
					ChartName: "runai",
					Values: &apixv1.JSON{
						Raw: []byte(`{"global":{"domain":"aif:auto-domain:runai"}}`),
					},
				},
			},
		},
	}
	wl := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wl",
			Namespace: "default",
		},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "Test",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint: &aiplatformv1alpha1.BlueprintSource{
					Name:    "runai",
					Version: "1.0.0",
				},
			},
			TargetNamespace: "runai",
			TargetClusters:  []string{"local"},
		},
	}

	scheme := aiworkloadTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bp, wl).Build()
	h := NewAIWorkloadHandler(c)

	updateBody := aiWorkloadCreateBody{
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "Test",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint: &aiplatformv1alpha1.BlueprintSource{
					Name:    "runai",
					Version: "1.0.0",
				},
			},
			TargetNamespace: "runai",
			TargetClusters:  []string{"local"},
			ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{
				{
					ComponentName: "runai",
					Values: &apixv1.JSON{
						Raw: []byte(`{"global":{"domain":"any.example.com"}}`),
					},
				},
			},
		},
	}

	body, _ := json.Marshal(updateBody)
	req := httptest.NewRequest("PATCH", "/api/v1/namespaces/default/aiworkloads/test-wl", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("namespace", "default")
	req.SetPathValue("name", "test-wl")

	rec := httptest.NewRecorder()
	h.updateAIWorkload(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLockedValues_BlueprintNotFound(t *testing.T) {
	wl := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wl",
			Namespace: "default",
		},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "Test",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint: &aiplatformv1alpha1.BlueprintSource{
					Name:    "missing",
					Version: "1.0.0",
				},
			},
			TargetNamespace: "runai",
			TargetClusters:  []string{"local"},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			ResolvedValues: []aiplatformv1alpha1.ResolvedValue{
				{
					ComponentName: "runai",
					Path:          "global.domain",
					Value:         "runai.10.0.0.12.sslip.io",
					Source:        "auto",
				},
			},
		},
	}

	scheme := aiworkloadTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wl).Build()
	h := NewAIWorkloadHandler(c)

	updateBody := aiWorkloadCreateBody{
		Spec: wl.Spec,
	}

	body, _ := json.Marshal(updateBody)
	req := httptest.NewRequest("PATCH", "/api/v1/namespaces/default/aiworkloads/test-wl", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("namespace", "default")
	req.SetPathValue("name", "test-wl")

	rec := httptest.NewRecorder()
	h.updateAIWorkload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	respBody := rec.Body.String()
	if !strings.Contains(respBody, "blueprint") || !strings.Contains(respBody, "not found") {
		t.Errorf("expected blueprint not found error, got: %s", respBody)
	}
}

func TestLockedValues_BlueprintGetErrorIs500(t *testing.T) {
	wl := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "Test",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "runai", Version: "1.0.0"},
			},
			TargetNamespace: "runai",
			TargetClusters:  []string{"local"},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			ResolvedValues: []aiplatformv1alpha1.ResolvedValue{
				{ComponentName: "runai", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto"},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(aiworkloadTestScheme(t)).WithObjects(wl).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*aiplatformv1alpha1.Blueprint); ok {
					return errors.New("apiserver unavailable")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).Build()
	h := NewAIWorkloadHandler(c)

	body, _ := json.Marshal(aiWorkloadCreateBody{Spec: wl.Spec})
	req := httptest.NewRequest("PATCH", "/api/v1/namespaces/default/aiworkloads/test-wl", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("namespace", "default")
	req.SetPathValue("name", "test-wl")

	rec := httptest.NewRecorder()
	h.updateAIWorkload(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func aiworkloadTestScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}
