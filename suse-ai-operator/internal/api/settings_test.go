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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/suse-ai-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newSettingsScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	s := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	return s
}

func newSettingsFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(newSettingsScheme(t)).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).
		WithObjects(objects...).
		Build()
}

func newSettingsHandler(c client.Client, ns string) http.Handler {
	mux := http.NewServeMux()
	NewSettingsHandler(c, ns).Register(mux)
	return mux
}

func sampleCR() *aiplatformv1alpha1.Settings {
	return &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "settings",
			Namespace: "suse-ai-operator",
		},
	}
}

// GET returns 200 with the current spec.
func TestSettingsGet_200(t *testing.T) {
	cr := sampleCR()
	cr.Spec.Fleet.RepoURL = "https://git.example.com"
	c := newSettingsFakeClient(t, cr)
	h := newSettingsHandler(c, "suse-ai-operator")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type=%q want application/json", ct)
	}
	var got aiplatformv1alpha1.Settings
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Spec.Fleet.RepoURL != "https://git.example.com" {
		t.Errorf("fleet.repoURL=%q want https://git.example.com", got.Spec.Fleet.RepoURL)
	}
}

// GET returns 404 when no CR exists.
func TestSettingsGet_404(t *testing.T) {
	c := newSettingsFakeClient(t)
	h := newSettingsHandler(c, "suse-ai-operator")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404; body=%s", rec.Code, rec.Body)
	}
	var apiErr APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal APIError: %v; body=%s", err, rec.Body)
	}
	if apiErr.Code != ErrCodeNotFound {
		t.Errorf("error.code=%q want %q", apiErr.Code, ErrCodeNotFound)
	}
}

// PUT returns 200 and updates the CR.
func TestSettingsPut_200(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "suse-ai-operator")

	body := `{"spec":{"fleet":{"repoURL":"https://git.example.com","branch":"main"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	var resp aiplatformv1alpha1.Settings
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Spec.Fleet.RepoURL != "https://git.example.com" {
		t.Errorf("response fleet.repoURL=%q want https://git.example.com", resp.Spec.Fleet.RepoURL)
	}

	// Verify CR is updated in cluster.
	var stored aiplatformv1alpha1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "suse-ai-operator", Name: "settings"}, &stored); err != nil {
		t.Fatalf("Get after PUT: %v", err)
	}
	if stored.Spec.Fleet.RepoURL != "https://git.example.com" {
		t.Errorf("stored fleet.repoURL=%q want https://git.example.com", stored.Spec.Fleet.RepoURL)
	}
}

// PUT with invalid JSON returns 400.
func TestSettingsPut_InvalidJSON_400(t *testing.T) {
	c := newSettingsFakeClient(t)
	h := newSettingsHandler(c, "suse-ai-operator")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", rec.Code, rec.Body)
	}
	var apiErr APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal APIError: %v", err)
	}
	if apiErr.Code != ErrCodeInvalidInput {
		t.Errorf("error.code=%q want %q", apiErr.Code, ErrCodeInvalidInput)
	}
}

// PUT with empty spec clears settings (zero-value overwrite is intentional).
func TestSettingsPut_EmptySpec_200(t *testing.T) {
	cr := sampleCR()
	cr.Spec.Fleet.RepoURL = "https://git.example.com"
	c := newSettingsFakeClient(t, cr)
	h := newSettingsHandler(c, "suse-ai-operator")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"spec":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
}

func TestGetRegistryCredentials_NoSettings(t *testing.T) {
	c := newSettingsFakeClient(t)
	h := newSettingsHandler(c, "suse-ai-system")

	req := httptest.NewRequest("GET", "/api/v1/settings/registry-credentials", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["applicationCollection"] != nil || body["suseRegistry"] != nil || body["nvidia"] != nil {
		t.Errorf("expected empty credentials when settings not found, got %v", body)
	}
}

func TestGetRegistryCredentials_Nvidia(t *testing.T) {
	const ns = "suse-ai-system"

	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("$oauthtoken")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("nvapi-secret")},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-token", Key: "token"},
			},
		},
	}

	c := newSettingsFakeClient(t, cr, userSecret, tokenSecret)
	h := newSettingsHandler(c, ns)

	req := httptest.NewRequest("GET", "/api/v1/settings/registry-credentials", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body RegistryCredentials
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body.Nvidia == nil {
		t.Fatalf("expected nvidia creds, got nil")
	}
	if body.Nvidia.Username != "$oauthtoken" || body.Nvidia.Password != "nvapi-secret" {
		t.Errorf("unexpected creds: %+v", body.Nvidia)
	}
	if body.Nvidia.RegistryHost != "nvcr.io" {
		t.Errorf("expected host nvcr.io, got %q", body.Nvidia.RegistryHost)
	}
}

func TestGetRegistryCredentials_AppCollectionHostFromOCIURL(t *testing.T) {
	const ns = "suse-ai-system"

	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ac-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("u")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ac-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("p")},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{
				ApplicationCollection: "oci://registry.example.com/charts",
			},
			ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ac-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ac-token", Key: "token"},
			},
		},
	}

	c := newSettingsFakeClient(t, cr, userSecret, tokenSecret)
	h := newSettingsHandler(c, ns)

	req := httptest.NewRequest("GET", "/api/v1/settings/registry-credentials", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body RegistryCredentials
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body.ApplicationCollection == nil {
		t.Fatalf("expected applicationCollection creds, got nil")
	}
	// The endpoint override is a full OCI chart-repo URL; the image-pull-secret
	// host must be just the registry host, not the whole URL.
	if body.ApplicationCollection.RegistryHost != "registry.example.com" {
		t.Errorf("expected host registry.example.com (base of OCI URL), got %q", body.ApplicationCollection.RegistryHost)
	}
}
