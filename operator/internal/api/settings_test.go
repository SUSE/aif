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
	"github.com/SUSE/aif-operator/internal/credentials"
	"github.com/SUSE/aif-operator/internal/git"
	"github.com/go-git/go-git/v5/plumbing/transport"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	mux := newTestMux(NewSettingsHandler(c, ns))
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

// Preserve deprecated configuration for upgrades and cached UIs. The controller
// does not read any of these Secret references or use the legacy connection.
func TestSettingsPut_RoundTripsRancherCatalog(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	body := `{"spec":{"rancherCatalog":{` +
		`"url":"https://rancher.cattle-system.svc",` +
		`"tokenSecretRef":{"name":"rancher-catalog-token","key":"token"},` +
		`"caBundleSecretRef":{"name":"rancher-catalog-ca","key":"ca.crt"},` +
		`"insecureSkipVerify":true}}}`
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
	rc := stored.Spec.RancherCatalog
	if rc.URL != "https://rancher.cattle-system.svc" {
		t.Errorf("rancherCatalog.url=%q want https://rancher.cattle-system.svc", rc.URL)
	}
	if rc.TokenSecretRef == nil || rc.TokenSecretRef.Name != "rancher-catalog-token" || rc.TokenSecretRef.Key != "token" {
		t.Errorf("rancherCatalog.tokenSecretRef=%+v want {rancher-catalog-token token}", rc.TokenSecretRef)
	}
	if rc.CABundleSecretRef == nil || rc.CABundleSecretRef.Name != "rancher-catalog-ca" || rc.CABundleSecretRef.Key != "ca.crt" {
		t.Errorf("rancherCatalog.caBundleSecretRef=%+v want {rancher-catalog-ca ca.crt}", rc.CABundleSecretRef)
	}
	if !rc.InsecureSkipVerify {
		t.Errorf("rancherCatalog.insecureSkipVerify=false want true")
	}
}

func TestSettingsPut_RoundTripsRegistryCABundleRefs(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	body := `{"spec":{` +
		`"fleet":{"caBundleSecretRef":{"name":"git-ca","key":"ca.crt"}},` +
		`"applicationCollection":{"caBundleSecretRef":{"name":"appco-ca","key":"ca.crt"}},` +
		`"suseRegistry":{"caBundleSecretRef":{"name":"suse-ca","key":"bundle.pem"}},` +
		`"nvidia":{"caBundleSecretRef":{"name":"nvidia-ca","key":"tls-ca"}}}}`
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
	cases := []struct {
		name string
		got  *aiplatformv1alpha1.SecretKeyRef
		want aiplatformv1alpha1.SecretKeyRef
	}{
		{"Fleet Git", stored.Spec.Fleet.CABundleSecretRef, aiplatformv1alpha1.SecretKeyRef{Name: "git-ca", Key: "ca.crt"}},
		{"application collection", stored.Spec.ApplicationCollection.CABundleSecretRef, aiplatformv1alpha1.SecretKeyRef{Name: "appco-ca", Key: "ca.crt"}},
		{"SUSE registry", stored.Spec.SUSERegistry.CABundleSecretRef, aiplatformv1alpha1.SecretKeyRef{Name: "suse-ca", Key: "bundle.pem"}},
		{"NVIDIA", stored.Spec.Nvidia.CABundleSecretRef, aiplatformv1alpha1.SecretKeyRef{Name: "nvidia-ca", Key: "tls-ca"}},
	}
	for _, tc := range cases {
		if tc.got == nil || *tc.got != tc.want {
			t.Errorf("%s caBundleSecretRef=%+v want %+v", tc.name, tc.got, tc.want)
		}
	}
}

// PUT that omits a built-in registry or Rancher catalog secret ref clears it even
// when another field manager owns it. Server-side apply alone only drops fields the applier solely
// owns, so an omitted ref co-owned by the settings controller or Helm would survive
// (or be left as a partial ref missing its required name). The handler must null
// omitted refs with a merge patch that ignores field ownership.
func TestSettingsPut_ClearsOmittedSecretRefs(t *testing.T) {
	cr := sampleCR()
	cr.Spec.ApplicationCollection.UserSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "username"}
	cr.Spec.ApplicationCollection.TokenSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "password"}
	cr.Spec.SUSERegistry.UserSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "suse-registry", Key: "username"}
	cr.Spec.SUSERegistry.TokenSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "suse-registry", Key: "password"}
	cr.Spec.Nvidia.CABundleSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "nvidia-ca", Key: "ca.crt"}
	cr.Spec.RancherCatalog.TokenSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "rancher-token", Key: "token"}

	var mergePatches []string
	c := fake.NewClientBuilder().
		WithScheme(newSettingsScheme(t)).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).
		WithObjects(cr).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if patch.Type() == types.MergePatchType {
					data, err := patch.Data(obj)
					if err != nil {
						return err
					}
					mergePatches = append(mergePatches, string(data))
				}
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()
	h := newSettingsHandler(c, "aif-operator")

	// Application Collection, the NVIDIA CA and the Rancher catalog token are set to
	// None; SUSE Registry keeps its refs.
	body := `{"spec":{"suseRegistry":{` +
		`"userSecretRef":{"name":"suse-registry","key":"username"},` +
		`"tokenSecretRef":{"name":"suse-registry","key":"password"}}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	want := `{"spec":{"applicationCollection":{"tokenSecretRef":null,"userSecretRef":null},` +
		`"nvidia":{"caBundleSecretRef":null},"rancherCatalog":{"tokenSecretRef":null}}}`
	if len(mergePatches) != 1 || mergePatches[0] != want {
		t.Fatalf("merge patches=%q want [%q]", mergePatches, want)
	}

	var stored aiplatformv1alpha1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aif-operator", Name: "settings"}, &stored); err != nil {
		t.Fatalf("Get after PUT: %v", err)
	}
	if stored.Spec.ApplicationCollection.UserSecretRef != nil || stored.Spec.ApplicationCollection.TokenSecretRef != nil {
		t.Errorf("applicationCollection refs=%+v/%+v want cleared",
			stored.Spec.ApplicationCollection.UserSecretRef, stored.Spec.ApplicationCollection.TokenSecretRef)
	}
	if stored.Spec.Nvidia.CABundleSecretRef != nil {
		t.Errorf("nvidia caBundleSecretRef=%+v want cleared", stored.Spec.Nvidia.CABundleSecretRef)
	}
	if stored.Spec.RancherCatalog.TokenSecretRef != nil {
		t.Errorf("rancherCatalog tokenSecretRef=%+v want cleared", stored.Spec.RancherCatalog.TokenSecretRef)
	}
	if stored.Spec.SUSERegistry.UserSecretRef == nil || stored.Spec.SUSERegistry.TokenSecretRef == nil {
		t.Errorf("suseRegistry refs cleared; want kept")
	}
}

// PUT that omits refs the existing Settings never had issues no merge patch.
func TestSettingsPut_NoMergePatchWhenNothingToClear(t *testing.T) {
	merged := false
	c := fake.NewClientBuilder().
		WithScheme(newSettingsScheme(t)).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).
		WithObjects(sampleCR()).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if patch.Type() == types.MergePatchType {
					merged = true
				}
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()
	h := newSettingsHandler(c, "aif-operator")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"spec":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	if merged {
		t.Error("unexpected merge patch when no refs needed clearing")
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

func TestSettingsPut_InvalidCustomRepo_400(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	// Invalid: git repo without branch
	body := `{"spec":{"customRepos":[{"name":"test","type":"git","gitRepo":"https://git.example.com/repo"}]}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 for invalid custom repo; body=%s", rec.Code, rec.Body)
	}
	var apiErr APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal APIError: %v", err)
	}
	if apiErr.Code != ErrCodeInvalidInput {
		t.Errorf("error.code=%q want %q", apiErr.Code, ErrCodeInvalidInput)
	}

	// Verify CR was not updated
	var stored aiplatformv1alpha1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aif-operator", Name: "settings"}, &stored); err != nil {
		t.Fatalf("Get after rejected PUT: %v", err)
	}
	if len(stored.Spec.CustomRepos) > 0 {
		t.Errorf("invalid custom repo should not have been persisted, got %d repos", len(stored.Spec.CustomRepos))
	}
}

var testClusterRepoGVK = schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"}

// newClusterRepo returns a ClusterRepo with the given labels.
func newClusterRepo(name string, labels map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(testClusterRepoGVK)
	u.SetName(name)
	u.SetLabels(labels)
	return u
}

// newSettingsFakeClientWithRepos is newSettingsFakeClient with the ClusterRepo
// kind registered, so a test can seed existing ClusterRepos.
func newSettingsFakeClientWithRepos(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	s := newSettingsScheme(t)
	s.AddKnownTypeWithName(testClusterRepoGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(testClusterRepoGVK.GroupVersion().WithKind("ClusterRepoList"), &unstructured.UnstructuredList{})
	return fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).
		WithObjects(objects...).
		Build()
}

func putCustomRepo(t *testing.T, h http.Handler, name string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"spec":{"customRepos":[{"name":"` + name + `","type":"helm","url":"https://charts.example.com"}]}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A custom repo's name is used verbatim as its ClusterRepo name, so saving one
// whose name matches a ClusterRepo the custom-repo feature doesn't own must be
// refused rather than taking that repo over.
func TestSettingsPut_CustomRepoNameTakenByOtherClusterRepo_400(t *testing.T) {
	foreign := newClusterRepo("partner-charts", map[string]string{"owner": "someone-else"})
	c := newSettingsFakeClientWithRepos(t, sampleCR(), foreign)
	h := newSettingsHandler(c, "aif-operator")

	rec := putCustomRepo(t, h, "partner-charts")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 for a name taken by another ClusterRepo; body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "already used by another chart repository") {
		t.Errorf("body=%s, want the name-taken message", rec.Body)
	}

	var stored aiplatformv1alpha1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aif-operator", Name: "settings"}, &stored); err != nil {
		t.Fatalf("Get after rejected PUT: %v", err)
	}
	if len(stored.Spec.CustomRepos) > 0 {
		t.Errorf("custom repo with a taken name should not have been persisted, got %d repos", len(stored.Spec.CustomRepos))
	}
}

// Re-saving an existing custom repo must succeed: its ClusterRepo carries the
// custom-repo label, so it is not a collision.
func TestSettingsPut_CustomRepoNameOwnedByCustomRepo_200(t *testing.T) {
	own := newClusterRepo("partner-charts", map[string]string{
		credentials.ManagedRepoLabel: credentials.LabelValueTrue,
		credentials.CustomRepoLabel:  credentials.LabelValueTrue,
	})
	c := newSettingsFakeClientWithRepos(t, sampleCR(), own)
	h := newSettingsHandler(c, "aif-operator")

	if rec := putCustomRepo(t, h, "partner-charts"); rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 when the ClusterRepo is already a custom repo; body=%s", rec.Code, rec.Body)
	}
}

func TestSettingsPut_CustomRepoNewName_200(t *testing.T) {
	c := newSettingsFakeClientWithRepos(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	if rec := putCustomRepo(t, h, "partner-charts"); rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 for an unused name; body=%s", rec.Code, rec.Body)
	}
}

// The chart a user enters to Test a custom repo is saved with the repo, so the
// Test form shows it again after a reload instead of starting empty.
func TestSettingsPut_CustomRepoTestChartPersists(t *testing.T) {
	c := newSettingsFakeClientWithRepos(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	body := `{"spec":{"customRepos":[{"name":"partner-charts","type":"oci","url":"oci://registry.example.com/charts","testChart":"demo"}]}}`
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
	if len(stored.Spec.CustomRepos) != 1 || stored.Spec.CustomRepos[0].TestChart != "demo" {
		t.Errorf("stored custom repos = %+v, want testChart \"demo\" persisted", stored.Spec.CustomRepos)
	}
}

func getRegistryStatus(t *testing.T, h http.Handler) (*httptest.ResponseRecorder, RegistryStatus) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/settings/registry-status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body RegistryStatus
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	return w, body
}

func TestGetRegistryStatus_NoSettings(t *testing.T) {
	c := newSettingsFakeClient(t)
	h := newSettingsHandler(c, "suse-ai-system")

	w, _ := getRegistryStatus(t, h)

	const want = `{"applicationCollection":false,"suseRegistry":false,"nvidia":false,` +
		`"usable":{"applicationCollection":false,"suseRegistry":false,"nvidia":false}}`
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestGetRegistryStatus_Nvidia(t *testing.T) {
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

	w, body := getRegistryStatus(t, h)

	if !body.Nvidia || body.ApplicationCollection || body.SUSERegistry {
		t.Errorf("status = %+v, want only nvidia configured", body)
	}
	raw := w.Body.String()
	for _, s := range []string{"nvapi-secret", "$oauthtoken", "password", "username"} {
		if strings.Contains(raw, s) {
			t.Errorf("registry-status must return only booleans, found %q in %s", s, raw)
		}
	}
}

// A credential supplied only as a well-known secret (no Settings spec refs) is
// resolved via EffectiveRefs and reported as configured — the pre-flight relies
// on this to match what the operator can actually create.
func TestGetRegistryStatus_WellKnownSecretNames(t *testing.T) {
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

	_, body := getRegistryStatus(t, h)

	if !body.ApplicationCollection {
		t.Errorf("expected applicationCollection configured via well-known secret, got %+v", body)
	}
	if body.SUSERegistry || body.Nvidia {
		t.Errorf("expected only applicationCollection configured, got %+v", body)
	}
}

// A user secret without its token secret is not a usable pair, so the
// registry is reported as not configured.
func TestGetRegistryStatus_PartialPair(t *testing.T) {
	const ns = "suse-ai-system"

	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "su-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("u")},
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

	c := newSettingsFakeClient(t, cr, userSecret)
	h := newSettingsHandler(c, ns)

	_, body := getRegistryStatus(t, h)

	if body.SUSERegistry {
		t.Errorf("expected suseRegistry not configured when token secret is missing, got %+v", body)
	}
}

// usable mirrors the Settings controller: credentials resolve, or (NVIDIA
// only) a mirror endpoint is set with no credential refs. configured keeps
// meaning "credentials resolve" for the pull-secret consumers.
func TestGetRegistryStatus_Usable(t *testing.T) {
	const ns = "aif-operator"
	nvUser := &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-user", Key: "username"}
	nvToken := &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-token", Key: "token"}
	creds := []client.Object{
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ngc-user", Namespace: ns},
			Data: map[string][]byte{"username": []byte("$oauthtoken")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ngc-token", Namespace: ns},
			Data: map[string][]byte{"token": []byte("nvapi-secret")}},
	}
	mirrors := &aiplatformv1alpha1.RegistryEndpointsSettings{
		ApplicationCollection: "oci://mirror.example.com/ac",
		SUSERegistry:          "oci://mirror.example.com/sr",
		Nvidia:                "oci://mirror.example.com/nvidia",
	}

	tests := []struct {
		name           string
		endpoints      *aiplatformv1alpha1.RegistryEndpointsSettings
		refs           bool
		secrets        []client.Object
		wantConfigured bool
		wantUsable     bool
	}{
		{"credentials only", nil, true, creds, true, true},
		{"anonymous mirror", mirrors, false, nil, false, true},
		{"mirror with credentials", mirrors, true, creds, true, true},
		{"mirror with unreadable refs", mirrors, true, nil, false, false},
		{"neither", nil, false, nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &aiplatformv1alpha1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
				Spec:       aiplatformv1alpha1.SettingsSpec{RegistryEndpoints: tt.endpoints},
			}
			if tt.refs {
				cr.Spec.Nvidia.UserSecretRef, cr.Spec.Nvidia.TokenSecretRef = nvUser, nvToken
			}
			h := newSettingsHandler(newSettingsFakeClient(t, append([]client.Object{cr}, tt.secrets...)...), ns)

			_, body := getRegistryStatus(t, h)

			if body.Nvidia != tt.wantConfigured || body.Usable.Nvidia != tt.wantUsable {
				t.Errorf("nvidia configured=%v usable=%v, want %v/%v", body.Nvidia, body.Usable.Nvidia, tt.wantConfigured, tt.wantUsable)
			}
			// Application Collection and SUSE Registry mirrors still need credentials.
			if body.ApplicationCollection || body.SUSERegistry || body.Usable.ApplicationCollection || body.Usable.SUSERegistry {
				t.Errorf("status = %+v, want application collection and SUSE registry unusable", body)
			}
		})
	}
}

// Application Collection credentials make that registry usable too.
func TestGetRegistryStatus_UsableFollowsCredentials(t *testing.T) {
	const ns = "aif-operator"
	acSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("u"), "password": []byte("p")},
	}
	cr := &aiplatformv1alpha1.Settings{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns}}
	h := newSettingsHandler(newSettingsFakeClient(t, cr, acSecret), ns)

	_, body := getRegistryStatus(t, h)

	if !body.Usable.ApplicationCollection || body.Usable.SUSERegistry || body.Usable.Nvidia {
		t.Errorf("usable = %+v, want only applicationCollection", body.Usable)
	}
}

func TestRegistryHost(t *testing.T) {
	h := &SettingsHandler{}
	mirrored := &aiplatformv1alpha1.Settings{Spec: aiplatformv1alpha1.SettingsSpec{
		RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{
			ApplicationCollection: "oci://ac.mirror.test/charts",
			SUSERegistry:          "oci://sr.mirror.test/charts",
			Nvidia:                "oci://nv.mirror.test/nvidia",
		},
	}}
	tests := []struct {
		target, override string
		s                *aiplatformv1alpha1.Settings
		want             string
	}{
		{"applicationCollection", "", &aiplatformv1alpha1.Settings{}, defaultAppCollectionHost},
		{"suseRegistry", "", &aiplatformv1alpha1.Settings{}, defaultSUSERegistryHost},
		{"nvidia", "", &aiplatformv1alpha1.Settings{}, defaultNvidiaHost},
		{"applicationCollection", "", mirrored, "ac.mirror.test"},
		{"suseRegistry", "", mirrored, "sr.mirror.test"},
		{"nvidia", "", mirrored, "nv.mirror.test"},
		{"nvidia", "oci://override.test/x", mirrored, "override.test"},
		{"unknown", "", mirrored, ""},
	}
	for _, tt := range tests {
		if got := h.registryHost(tt.target, tt.s, tt.override); got != tt.want {
			t.Errorf("registryHost(%q, override %q) = %q, want %q", tt.target, tt.override, got, tt.want)
		}
	}
}

// registry-credentials was replaced by registry-status.
func TestRegistryCredentialsRouteRemoved(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	req := httptest.NewRequest("GET", "/api/v1/settings/registry-credentials", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestValidateCredentials_RegistryOKFromSaved(t *testing.T) {
	const ns = "aif-operator"
	const caBundle = "test private registry CA"
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "su-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("u")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "su-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("p")},
	}
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "su-ca", Namespace: ns},
		Data:       map[string][]byte{"ca.crt": []byte(caBundle)},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			SUSERegistry: aiplatformv1alpha1.SUSERegistrySettings{
				UserSecretRef:     &aiplatformv1alpha1.SecretKeyRef{Name: "su-user", Key: "username"},
				TokenSecretRef:    &aiplatformv1alpha1.SecretKeyRef{Name: "su-token", Key: "token"},
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "su-ca", Key: "ca.crt"},
			},
		},
	}
	c := newSettingsFakeClient(t, cr, userSecret, tokenSecret, caSecret)
	h := newSettingsHandler(c, ns)

	// Stub the probe: assert it receives the resolved creds + default host.
	orig := probeRegistryFn
	defer func() { probeRegistryFn = orig }()
	probeRegistryFn = func(_ context.Context, host, user, pass string, caPEM []byte) credcheck.Result {
		if host != "registry.suse.com" || user != "u" || pass != "p" || string(caPEM) != caBundle {
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

func TestValidateCredentials_RegistryMissingCAIsConfigurationError(t *testing.T) {
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
				UserSecretRef:     &aiplatformv1alpha1.SecretKeyRef{Name: "su-user", Key: "username"},
				TokenSecretRef:    &aiplatformv1alpha1.SecretKeyRef{Name: "su-token", Key: "token"},
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "missing-ca", Key: "ca.crt"},
			},
		},
	}
	c := newSettingsFakeClient(t, cr, userSecret, tokenSecret)
	h := newSettingsHandler(c, ns)

	orig := probeRegistryFn
	defer func() { probeRegistryFn = orig }()
	called := false
	probeRegistryFn = func(_ context.Context, _, _, _ string, _ []byte) credcheck.Result {
		called = true
		return credcheck.Result{Status: credcheck.StatusOK}
	}

	body := `{"targets":["suseRegistry"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if called {
		t.Fatal("registry probe must not run when the configured CA cannot be read")
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != statusError || !strings.Contains(resp.Results[0].Message, "missing-ca") {
		t.Fatalf("want missing CA configuration error, got %+v", resp.Results)
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
	probeRegistryFn = func(_ context.Context, host, user, pass string, _ []byte) credcheck.Result {
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

func TestValidateCredentials_EndpointOverrideBeforeSave(t *testing.T) {
	const ns = "aif-operator"
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ov-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("ou")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ov-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("op")},
	}
	c := newSettingsFakeClient(t, sampleCR(), userSecret, tokenSecret)
	h := newSettingsHandler(c, ns)

	orig := probeRegistryFn
	defer func() { probeRegistryFn = orig }()
	gotHost := ""
	probeRegistryFn = func(_ context.Context, host, _, _ string, _ []byte) credcheck.Result {
		gotHost = host
		return credcheck.Result{Status: credcheck.StatusOK, Message: "authenticated"}
	}

	body := `{"targets":["suseRegistry"],"overrides":{"suseRegistry":{"url":"oci://harbor.airgap.test/charts","userSecretRef":{"name":"ov-user","key":"username"},"tokenSecretRef":{"name":"ov-token","key":"token"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	if gotHost != "harbor.airgap.test" {
		t.Fatalf("probe host=%q want harbor.airgap.test", gotHost)
	}
}

func TestValidateCredentials_GitAuthClassification(t *testing.T) {
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://git.example.com/repo.git", Branch: "main"}},
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

func TestValidateCredentials_GitHTTPSCredentialOverrideWithoutUsername(t *testing.T) {
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
	}
	credential := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-credential", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("secret-token")},
	}
	c := newSettingsFakeClient(t, cr, credential)
	h := newSettingsHandler(c, ns)

	orig := gitCheckAuthFn
	defer func() { gitCheckAuthFn = orig }()
	gitCheckAuthFn = func(_ *git.Client, _ context.Context) error { return nil }

	body := `{"targets":["gitops"],"overrides":{"gitops":{"repoURL":"https://git.example.com/repo.git","branch":"main","credSecretRef":{"name":"git-credential","key":"token"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != statusOK {
		t.Fatalf("want ok, got %+v", resp.Results)
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
	probeRegistryFn = func(_ context.Context, host, user, pass string, _ []byte) credcheck.Result {
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
			Fleet: aiplatformv1alpha1.FleetSettings{GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://git.example.com/repo.git", Branch: "main"}},
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

func TestValidateCustomRepo_HelmOK(t *testing.T) {
	const ns = "aif-operator"
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("cu")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("ct")},
	}
	c := newSettingsFakeClient(t, sampleCR(), userSecret, tokenSecret)
	h := newSettingsHandler(c, ns)

	orig := probeHelmIndexFn
	defer func() { probeHelmIndexFn = orig }()
	var got struct{ user, pass, url string }
	probeHelmIndexFn = func(_ context.Context, repoURL, user, pass string, _ []byte, _ bool) credcheck.Result {
		got.user, got.pass, got.url = user, pass, repoURL
		return credcheck.Result{Status: credcheck.StatusOK, Message: "index reachable"}
	}

	body := `{"targets":["customRepo"],"overrides":{"customRepo":{"type":"helm","url":"https://charts.example.com","userSecretRef":{"name":"custom-user","key":"username"},"tokenSecretRef":{"name":"custom-token","key":"token"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != statusOK {
		t.Fatalf("want status=ok, got %+v", resp.Results)
	}
	if got.user != "cu" || got.pass != "ct" || got.url != "https://charts.example.com" {
		t.Fatalf("probe got %+v want cu/ct/https://charts.example.com", got)
	}
}

func TestValidateCustomRepo_GitOK(t *testing.T) {
	const ns = "aif-operator"
	credSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-git-cred", Namespace: ns},
		Data:       map[string][]byte{"ssh-key": []byte("fake-key")},
	}
	c := newSettingsFakeClient(t, sampleCR(), credSecret)
	h := newSettingsHandler(c, ns)

	orig := gitCheckAuthFn
	defer func() { gitCheckAuthFn = orig }()
	gitCheckAuthFn = func(_ *git.Client, _ context.Context) error {
		return nil
	}

	body := `{"targets":["customRepo"],"overrides":{"customRepo":{"type":"git","gitRepo":"https://git.example.com/repo.git","branch":"main","credSecretRef":{"name":"custom-git-cred","key":"ssh-key"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != statusOK {
		t.Fatalf("want status=ok, got %+v", resp.Results)
	}
}

func TestValidateCustomRepo_NotConfigured(t *testing.T) {
	const ns = "aif-operator"
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, ns)

	// No URL or gitRepo provided
	body := `{"targets":["customRepo"],"overrides":{"customRepo":{"type":"oci"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != statusSkipped {
		t.Fatalf("want status=skipped, got %+v", resp.Results)
	}
}

func TestValidateCustomRepo_AnonymousProbe(t *testing.T) {
	const ns = "aif-operator"
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, ns)

	orig := probeRegistryWithInsecureFn
	defer func() { probeRegistryWithInsecureFn = orig }()
	got := struct{ user, pass, host string }{}
	probeRegistryWithInsecureFn = func(_ context.Context, host, user, pass string, _ []byte, _ bool) credcheck.Result {
		got.user, got.pass, got.host = user, pass, host
		return credcheck.Result{Status: credcheck.StatusOK, Message: "anonymous access"}
	}

	// URL set, NO credential refs → should probe anonymously (not skipped)
	body := `{"targets":["customRepo"],"overrides":{"customRepo":{"type":"oci","url":"oci://public.example.com/charts"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != statusOK {
		t.Fatalf("want status=ok for anonymous probe, got %+v", resp.Results)
	}
	if got.user != "" || got.pass != "" {
		t.Fatalf("probe should be called with empty creds for anonymous, got user=%q pass=%q", got.user, got.pass)
	}
	if got.host != "public.example.com" {
		t.Fatalf("probe got host=%q, want public.example.com", got.host)
	}
}

func TestUnsafeProbeTarget(t *testing.T) {
	blocked := []string{
		"https://user:pass@repo.example.com",
		"https://localhost/charts",
		"https://127.0.0.1/charts",
		"https://[::1]/charts",
		"https://[::ffff:127.0.0.1]/charts",
		"http://169.254.169.254/latest/meta-data",
		"https://[fe80::1]/charts",
	}
	for _, u := range blocked {
		if unsafeProbeTarget(u) == "" {
			t.Errorf("unsafeProbeTarget(%q) allowed, want rejected", u)
		}
	}
	// Private address space is allowed: on-prem and air-gap mirrors live there.
	// The credcheck dial guard keeps a public repository out of it.
	allowed := []string{
		"",
		"https://charts.example.com/stable",
		"oci://10.0.0.5/charts",
		"https://192.168.1.10:8443/charts",
		"https://172.16.0.1/charts",
		"https://[fd00::1]/charts",
	}
	for _, u := range allowed {
		if msg := unsafeProbeTarget(u); msg != "" {
			t.Errorf("unsafeProbeTarget(%q)=%q, want allowed", u, msg)
		}
	}
}

// helmManagedCR returns a Settings CR as the 2.3.1 chart renders it: one
// Helm-declared catalog plus the annotation naming it.
func helmManagedCR() *aiplatformv1alpha1.Settings {
	cr := sampleCR()
	cr.Annotations = map[string]string{
		aiplatformv1alpha1.SettingsHelmManagedAnnotation: `{"blueprintCatalogs":["partner-blueprints"]}`,
	}
	cr.Spec.BlueprintCatalogs = []aiplatformv1alpha1.BlueprintCatalogSource{{
		Name:          "partner-blueprints",
		Paths:         []string{"partners"},
		GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://github.com/suse/partner-blueprints.git", Branch: "main"},
	}}
	return cr
}

// capturingClient records the object handed to Patch so tests can assert on
// exactly what the API applied.
func capturingClient(t *testing.T, applied **aiplatformv1alpha1.Settings, objects ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(newSettingsScheme(t)).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).
		WithObjects(objects...).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if s, ok := obj.(*aiplatformv1alpha1.Settings); ok {
					*applied = s.DeepCopy()
				}
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()
}

func putJSON(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A Settings page that sends back the Helm entry unchanged (any 2.3.0 UI does)
// must not apply it, otherwise aif-operator-api co-owns it.
func TestSettingsPut_HelmCatalogRoundTripIsNotApplied(t *testing.T) {
	var applied *aiplatformv1alpha1.Settings
	h := newSettingsHandler(capturingClient(t, &applied, helmManagedCR()), "aif-operator")

	rec := putJSON(h, `{"spec":{"blueprintCatalogs":[
		{"name":"partner-blueprints","repoURL":"https://github.com/suse/partner-blueprints.git","branch":"main","paths":["partners"]},
		{"name":"team-a","repoURL":"https://git.example.com/a.git","branch":"main"}]}}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	if applied == nil {
		t.Fatal("Patch was not called")
	}
	if n := len(applied.Spec.BlueprintCatalogs); n != 1 || applied.Spec.BlueprintCatalogs[0].Name != "team-a" {
		t.Fatalf("applied catalogs=%+v; want only team-a", applied.Spec.BlueprintCatalogs)
	}
}

// Review Focus 1: the 2.3.0 page sends branch "main" for a Helm entry whose
// branch is empty; that must still save.
func TestSettingsPut_HelmCatalogEmptyBranchRoundTrip_200(t *testing.T) {
	cr := helmManagedCR()
	cr.Spec.BlueprintCatalogs[0].Branch = ""
	var applied *aiplatformv1alpha1.Settings
	h := newSettingsHandler(capturingClient(t, &applied, cr), "aif-operator")

	rec := putJSON(h, `{"spec":{"blueprintCatalogs":[
		{"name":"partner-blueprints","repoURL":"https://github.com/suse/partner-blueprints.git","branch":"main","paths":["partners"]}]}}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	if len(applied.Spec.BlueprintCatalogs) != 0 {
		t.Fatalf("applied catalogs=%+v; want none", applied.Spec.BlueprintCatalogs)
	}
}

func TestSettingsPut_HelmCatalogChanged_400(t *testing.T) {
	h := newSettingsHandler(newSettingsFakeClient(t, helmManagedCR()), "aif-operator")

	rec := putJSON(h, `{"spec":{"blueprintCatalogs":[
		{"name":"partner-blueprints","repoURL":"https://github.com/suse/partner-blueprints.git","branch":"dev","paths":["partners"]}]}}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", rec.Code, rec.Body)
	}
	var apiErr APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal APIError: %v", err)
	}
	if apiErr.Code != ErrCodeInvalidInput {
		t.Errorf("code=%q want %q", apiErr.Code, ErrCodeInvalidInput)
	}
	if !strings.Contains(apiErr.Message, `"partner-blueprints" is managed by Helm values; change it with helm upgrade`) {
		t.Errorf("message=%q", apiErr.Message)
	}
}

// Review Focus 5: a UI entry reusing a Helm-declared name before Helm's entry
// exists in the live spec is rejected, not silently co-owned.
func TestSettingsPut_NewEntryWithHelmName_400(t *testing.T) {
	cr := helmManagedCR()
	cr.Spec.BlueprintCatalogs = nil
	h := newSettingsHandler(newSettingsFakeClient(t, cr), "aif-operator")

	rec := putJSON(h, `{"spec":{"blueprintCatalogs":[{"name":"partner-blueprints","repoURL":"https://fork.example.com/p.git"}]}}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", rec.Code, rec.Body)
	}
}

// Review Focus 4: a broken annotation fails loudly instead of applying Helm values.
func TestSettingsPut_InvalidHelmManagedAnnotation_500(t *testing.T) {
	cr := helmManagedCR()
	cr.Annotations[aiplatformv1alpha1.SettingsHelmManagedAnnotation] = `{"blueprintCatalogs":`
	var applied *aiplatformv1alpha1.Settings
	h := newSettingsHandler(capturingClient(t, &applied, cr), "aif-operator")

	rec := putJSON(h, `{"spec":{"fleet":{"repoURL":"https://git.example.com"}}}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500; body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "helm upgrade --reuse-values --force-conflicts") {
		t.Errorf("body=%s; want recovery hint", rec.Body)
	}
	if applied != nil {
		t.Error("Patch must not be called when the annotation cannot be read")
	}
}

// Review Focus 3: without the annotation every entry is the UI's, as before.
func TestSettingsPut_NoAnnotationAppliesAllCatalogs(t *testing.T) {
	cr := helmManagedCR()
	cr.Annotations = nil
	var applied *aiplatformv1alpha1.Settings
	h := newSettingsHandler(capturingClient(t, &applied, cr), "aif-operator")

	rec := putJSON(h, `{"spec":{"blueprintCatalogs":[
		{"name":"partner-blueprints","repoURL":"https://github.com/suse/partner-blueprints.git","branch":"dev","paths":["partners"]}]}}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	if len(applied.Spec.BlueprintCatalogs) != 1 || applied.Spec.BlueprintCatalogs[0].Branch != "dev" {
		t.Fatalf("applied catalogs=%+v; want the edited entry", applied.Spec.BlueprintCatalogs)
	}
}

// Every secret ref in Settings must name both a secret and a key; a ref is either
// complete or omitted (None).
func TestIncompleteSecretRefs(t *testing.T) {
	ref := func(name, key string) *aiplatformv1alpha1.SecretKeyRef {
		return &aiplatformv1alpha1.SecretKeyRef{Name: name, Key: key}
	}
	cases := []struct {
		name string
		set  func(*aiplatformv1alpha1.SettingsSpec)
		want []string
	}{
		{"complete and omitted refs pass", func(s *aiplatformv1alpha1.SettingsSpec) {
			s.Nvidia.UserSecretRef = ref("nv", "user")
			s.Nvidia.TokenSecretRef = ref("nv", "token")
		}, nil},
		{"fleet", func(s *aiplatformv1alpha1.SettingsSpec) {
			s.Fleet.CredSecretRef = ref("git", "")
			s.Fleet.CABundleSecretRef = ref("", "ca.crt")
		}, []string{"spec.fleet.credSecretRef", "spec.fleet.caBundleSecretRef"}},
		{"built-in registries", func(s *aiplatformv1alpha1.SettingsSpec) {
			s.ApplicationCollection.UserSecretRef = ref("appco", "")
			s.SUSERegistry.TokenSecretRef = ref("suse", "")
			s.Nvidia.CABundleSecretRef = ref("nv-ca", "")
		}, []string{
			"spec.applicationCollection.userSecretRef",
			"spec.suseRegistry.tokenSecretRef",
			"spec.nvidia.caBundleSecretRef",
		}},
		{"rancher catalog", func(s *aiplatformv1alpha1.SettingsSpec) {
			s.RancherCatalog.TokenSecretRef = ref("rancher", "")
		}, nil},
		{"blueprint catalogs and custom repos", func(s *aiplatformv1alpha1.SettingsSpec) {
			s.BlueprintCatalogs = []aiplatformv1alpha1.BlueprintCatalogSource{{Name: "a"}, {Name: "b"}}
			s.BlueprintCatalogs[1].CredSecretRef = ref("git", "")
			s.CustomRepos = []aiplatformv1alpha1.CustomRepoSpec{{Name: "r"}}
			s.CustomRepos[0].SSHKeySecretRef = ref("ssh", "")
		}, []string{"spec.blueprintCatalogs[1].credSecretRef", "spec.customRepos[0].sshKeySecretRef"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var spec aiplatformv1alpha1.SettingsSpec
			tc.set(&spec)
			if got := incompleteSecretRefs(&spec); fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("incompleteSecretRefs=%v want %v", got, tc.want)
			}
		})
	}
}

// errOnGet returns a settings fake client whose Get fails for the object named
// name with a non-NotFound error, as a timed-out or unavailable API would.
func errOnGet(t *testing.T, name string, objects ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(newSettingsScheme(t)).
		WithObjects(objects...).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if key.Name == name {
					return apierrors.NewServiceUnavailable("apiserver unavailable")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()
}

// A Settings read error is a 500, not "nothing configured": the Blueprint
// install pre-flight would otherwise block an install over configured
// credentials, while an error makes it skip the pre-flight.
func TestGetRegistryStatus_SettingsReadErrorIs500(t *testing.T) {
	h := newSettingsHandler(errOnGet(t, "settings"), "suse-ai-system")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/settings/registry-status", nil))

	assertErrorCode(t, w, http.StatusInternalServerError, ErrCodeInternal)
}

// Likewise for a credential Secret that exists but cannot be read; a missing
// Secret stays "not configured" (TestGetRegistryStatus_PartialPair).
func TestGetRegistryStatus_SecretReadErrorIs500(t *testing.T) {
	const ns = "suse-ai-system"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-token", Key: "token"},
			},
		},
	}
	h := newSettingsHandler(errOnGet(t, "ngc-user", cr), ns)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/settings/registry-status", nil))

	assertErrorCode(t, w, http.StatusInternalServerError, ErrCodeInternal)
}

func TestValidateCredentials_RancherCatalogIsRetired(t *testing.T) {
	h := NewSettingsHandler(fake.NewClientBuilder().WithScheme(newSettingsScheme(t)).Build(), testNS)
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.RancherCatalog.TokenSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "does-not-exist", Key: "token"}
	got := h.validateRancherCatalog(context.Background(), s, validateOverride{URL: "https://must-not-be-contacted.invalid"})
	if got.Status != statusSkipped {
		t.Fatalf("retired token path: %+v", got)
	}
}
