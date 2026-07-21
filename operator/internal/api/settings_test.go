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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credcheck"
	"github.com/SUSE/aif-operator/internal/git"
	"github.com/go-git/go-git/v5/plumbing/transport"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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
			Namespace: "aif-operator",
		},
	}
}

// GET returns 200 with the current spec.
func TestSettingsGet_200(t *testing.T) {
	cr := sampleCR()
	cr.Spec.Fleet.RepoURL = "https://git.example.com"
	c := newSettingsFakeClient(t, cr)
	h := newSettingsHandler(c, "aif-operator")

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
	h := newSettingsHandler(c, "aif-operator")

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
	h := newSettingsHandler(c, "aif-operator")

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
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aif-operator", Name: "settings"}, &stored); err != nil {
		t.Fatalf("Get after PUT: %v", err)
	}
	if stored.Spec.Fleet.RepoURL != "https://git.example.com" {
		t.Errorf("stored fleet.repoURL=%q want https://git.example.com", stored.Spec.Fleet.RepoURL)
	}
}

// PUT with invalid JSON returns 400.
func TestSettingsPut_InvalidJSON_400(t *testing.T) {
	c := newSettingsFakeClient(t)
	h := newSettingsHandler(c, "aif-operator")

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
	h := newSettingsHandler(c, "aif-operator")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"spec":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
}

// PUT that omits appCatalog must NOT drop a previously configured remoteUrl:
// appCatalog is managed out-of-band, so the handler merges it from the existing
// spec into the object it applies. We intercept the Patch to assert on exactly
// what is applied — the fake client's apply does not reproduce real server-side
// field-ownership removal, so asserting on the stored object alone would not
// distinguish fixed from unfixed.
func TestSettingsPut_PreservesAppCatalog(t *testing.T) {
	cr := sampleCR()
	cr.Spec.AppCatalog.RemoteURL = "https://catalog.example.com/catalog.json"

	var applied *aiplatformv1alpha1.Settings
	c := fake.NewClientBuilder().
		WithScheme(newSettingsScheme(t)).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).
		WithObjects(cr).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if s, ok := obj.(*aiplatformv1alpha1.Settings); ok {
					applied = s.DeepCopy()
				}
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()
	h := newSettingsHandler(c, "aif-operator")

	// A typical Settings-page save that touches only its own fields.
	body := `{"spec":{"fleet":{"repoURL":"https://git.example.com","branch":"main"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	if applied == nil {
		t.Fatal("Patch was not called")
	}
	if applied.Spec.AppCatalog.RemoteURL != "https://catalog.example.com/catalog.json" {
		t.Errorf("applied appCatalog.remoteUrl=%q want it merged from existing", applied.Spec.AppCatalog.RemoteURL)
	}
	if applied.Spec.Fleet.RepoURL != "https://git.example.com" {
		t.Errorf("applied fleet.repoURL=%q want https://git.example.com", applied.Spec.Fleet.RepoURL)
	}
}

// PUT that explicitly sets appCatalog.remoteUrl still updates it.
func TestSettingsPut_SetsAppCatalog(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	body := `{"spec":{"appCatalog":{"remoteUrl":"https://new.example.com/c.json"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}

	var stored aiplatformv1alpha1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aif-operator", Name: "settings"}, &stored); err != nil {
		t.Fatalf("Get after PUT: %v", err)
	}
	if stored.Spec.AppCatalog.RemoteURL != "https://new.example.com/c.json" {
		t.Errorf("appCatalog.remoteUrl=%q want https://new.example.com/c.json", stored.Spec.AppCatalog.RemoteURL)
	}
}

// A transient (non-NotFound) error while reading the existing CR must fail the
// request rather than proceed — proceeding would apply an empty appCatalog and
// silently wipe a configured remoteUrl.
func TestSettingsPut_PreserveGetError_500(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(newSettingsScheme(t)).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*aiplatformv1alpha1.Settings); ok {
					return apierrors.NewServiceUnavailable("transient")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).
		Build()
	h := newSettingsHandler(c, "aif-operator")

	// Body omits appCatalog, so the handler reads the existing CR to preserve it.
	body := `{"spec":{"fleet":{"repoURL":"https://git.example.com"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500; body=%s", rec.Code, rec.Body)
	}
}

// First-ever save (no existing Settings CR) must succeed: the appCatalog merge
// GET returns NotFound, which is ignored, and the save proceeds.
func TestSettingsPut_FirstSave_NoExistingCR(t *testing.T) {
	c := newSettingsFakeClient(t) // no existing CR
	h := newSettingsHandler(c, "aif-operator")

	body := `{"spec":{"fleet":{"repoURL":"https://git.example.com"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; first save should succeed; body=%s", rec.Code, rec.Body)
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

// A credential supplied only as a well-known secret (no Settings spec refs) is
// resolved via EffectiveRefs and reported as configured — the pre-flight relies
// on this to match what the operator can actually create.
func TestGetRegistryCredentials_DiscoversWellKnownSecret(t *testing.T) {
	const ns = "aif-operator"

	// "appco" is a well-known application-collection secret name.
	acSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("u"), "password": []byte("p")},
	}
	// Settings CR exists but has NO applicationCollection refs.
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
	}

	c := newSettingsFakeClient(t, cr, acSecret)
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
		t.Fatalf("expected application-collection creds discovered from well-known secret, got nil")
	}
	if body.ApplicationCollection.Username != "u" || body.ApplicationCollection.Password != "p" {
		t.Errorf("unexpected discovered creds: %+v", body.ApplicationCollection)
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

func TestValidateCredentials_RegistryOKFromSaved(t *testing.T) {
	const ns = "aif-operator"
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "su-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("u")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "su-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("p")},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			SUSERegistry: aiplatformv1alpha1.SUSERegistrySettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "su-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "su-token", Key: "token"},
			},
		},
	}
	c := newSettingsFakeClient(t, cr, userSecret, tokenSecret)
	h := newSettingsHandler(c, ns)

	// Stub the probe: assert it receives the resolved creds + default host.
	orig := probeRegistryFn
	defer func() { probeRegistryFn = orig }()
	probeRegistryFn = func(_ context.Context, host, user, pass string) credcheck.Result {
		if host != "registry.suse.com" || user != "u" || pass != "p" {
			return credcheck.Result{Status: credcheck.StatusFailed, Message: "unexpected inputs"}
		}
		return credcheck.Result{Status: credcheck.StatusOK, Message: "authenticated"}
	}

	body := `{"targets":["suseRegistry"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Target != "suseRegistry" || resp.Results[0].Status != statusOK {
		t.Fatalf("unexpected results: %+v", resp.Results)
	}
	if resp.Results[0].Host != "registry.suse.com" {
		t.Errorf("host=%q want registry.suse.com", resp.Results[0].Host)
	}
}

func TestValidateCredentials_SkippedWhenUnconfigured(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	body := `{"targets":["nvidia"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].Status != statusSkipped {
		t.Fatalf("want skipped, got %+v", resp.Results)
	}
}

func TestValidateCredentials_OverrideRefsBeforeSave(t *testing.T) {
	const ns = "aif-operator"
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ov-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("ou")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ov-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("op")},
	}
	// Settings CR has NO nvidia refs; the override supplies them.
	c := newSettingsFakeClient(t, sampleCR(), userSecret, tokenSecret)
	h := newSettingsHandler(c, ns)

	orig := probeRegistryFn
	defer func() { probeRegistryFn = orig }()
	got := struct{ user, pass, host string }{}
	probeRegistryFn = func(_ context.Context, host, user, pass string) credcheck.Result {
		got.user, got.pass, got.host = user, pass, host
		return credcheck.Result{Status: credcheck.StatusOK, Message: "authenticated"}
	}

	body := `{"targets":["nvidia"],"overrides":{"nvidia":{"userSecretRef":{"name":"ov-user","key":"username"},"tokenSecretRef":{"name":"ov-token","key":"token"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got.user != "ou" || got.pass != "op" || got.host != "nvcr.io" {
		t.Fatalf("probe got %+v want ou/op/nvcr.io", got)
	}
}

func TestValidateCredentials_GitAuthClassification(t *testing.T) {
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{RepoURL: "https://git.example.com/repo.git", Branch: "main"},
		},
	}
	c := newSettingsFakeClient(t, cr)
	h := newSettingsHandler(c, ns)

	orig := gitCheckAuthFn
	defer func() { gitCheckAuthFn = orig }()
	gitCheckAuthFn = func(_ *git.Client, _ context.Context) error {
		return transport.ErrAuthenticationRequired
	}

	body := `{"targets":["gitops"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].Status != statusFailed {
		t.Fatalf("want failed (auth), got %+v", resp.Results)
	}
}

func TestValidateCredentials_InvalidJSON400(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", rec.Code)
	}
}

func TestValidateCredentials_PartialOverrideFallsBack(t *testing.T) {
	const ns = "aif-operator"
	savedUser := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "saved-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("saved-u")},
	}
	savedToken := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "saved-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("saved-p")},
	}
	overrideUser := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "override-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("override-u")},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "saved-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "saved-token", Key: "token"},
			},
		},
	}
	c := newSettingsFakeClient(t, cr, savedUser, savedToken, overrideUser)
	h := newSettingsHandler(c, ns)

	orig := probeRegistryFn
	defer func() { probeRegistryFn = orig }()
	got := struct{ user, pass, host string }{}
	probeRegistryFn = func(_ context.Context, host, user, pass string) credcheck.Result {
		got.user, got.pass, got.host = user, pass, host
		return credcheck.Result{Status: credcheck.StatusOK, Message: "authenticated"}
	}

	// Override ONLY userSecretRef; tokenSecretRef must fall back to saved.
	body := `{"targets":["nvidia"],"overrides":{"nvidia":{"userSecretRef":{"name":"override-user","key":"username"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got.user != "override-u" {
		t.Errorf("user=%q want override-u", got.user)
	}
	if got.pass != "saved-p" {
		t.Errorf("pass=%q want saved-p", got.pass)
	}
	if got.host != "nvcr.io" {
		t.Errorf("host=%q want nvcr.io", got.host)
	}
}

func TestValidateCredentials_GitNetworkError(t *testing.T) {
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{RepoURL: "https://git.example.com/repo.git", Branch: "main"},
		},
	}
	c := newSettingsFakeClient(t, cr)
	h := newSettingsHandler(c, ns)

	orig := gitCheckAuthFn
	defer func() { gitCheckAuthFn = orig }()
	gitCheckAuthFn = func(_ *git.Client, _ context.Context) error {
		return fmt.Errorf("dial tcp: no route to host")
	}

	body := `{"targets":["gitops"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != statusError {
		t.Fatalf("want status=error for network error, got %+v", resp.Results)
	}
}

// A ref with an empty key (form still being filled in) is "not configured",
// not an authentication failure.
func TestValidateCredentials_IncompleteRefIsSkipped(t *testing.T) {
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-token", Key: ""}, // key not yet selected
			},
		},
	}
	c := newSettingsFakeClient(t, cr)
	h := newSettingsHandler(c, ns)

	body := `{"targets":["nvidia"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != statusSkipped {
		t.Fatalf("want status=skipped for incomplete ref, got %+v", resp.Results)
	}
}

// A complete ref whose secret cannot be resolved (deleted/rotated) is a config
// error, not the registry rejecting credentials.
func TestValidateCredentials_UnresolvableSecretIsError(t *testing.T) {
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "missing-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "missing-token", Key: "token"},
			},
		},
	}
	c := newSettingsFakeClient(t, cr) // secrets intentionally absent
	h := newSettingsHandler(c, ns)

	body := `{"targets":["nvidia"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != statusError {
		t.Fatalf("want status=error for unresolvable secret, got %+v", resp.Results)
	}
}
