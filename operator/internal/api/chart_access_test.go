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
	"sync"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credcheck"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestChartAccessUsesExplicitMirrorAndClearedReferences(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = aiplatformv1alpha1.AddToScheme(scheme)
	saved := &aiplatformv1alpha1.Settings{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "aif-operator"}}
	saved.Spec.Nvidia.UserSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "upstream-key", Key: "username"}
	saved.Spec.Nvidia.TokenSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "upstream-key", Key: "password"}
	saved.Spec.Nvidia.CABundleSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "old-ca", Key: "ca.crt"}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(saved).Build()
	h := NewSettingsHandler(client, "aif-operator")
	original := probeChartFn
	t.Cleanup(func() { probeChartFn = original })
	var calls int
	probeChartFn = func(_ context.Context, endpoint, chart, user, password string, ca []byte) credcheck.ChartResult {
		calls++
		if endpoint != "oci://harbor.internal/custom/nvidia" || chart != "mirrored-chart" || user != "" || password != "" || len(ca) != 0 {
			t.Errorf("probe did not use the explicit anonymous mirror")
		}
		return credcheck.ChartResult{RepositoryURL: endpoint, Status: "failed", Reason: "accessDenied"}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-chart-access", strings.NewReader(`{"target":"nvidia","chartName":"mirrored-chart","configuration":{"url":"oci://harbor.internal/custom/nvidia","userSecretRef":null,"tokenSecretRef":null,"caBundleSecretRef":null}}`))
	w := httptest.NewRecorder()
	h.validateChartAccess(w, r)
	if w.Code != http.StatusOK || calls != 1 {
		t.Fatalf("code=%d calls=%d body=%s", w.Code, calls, w.Body.String())
	}
}

func TestChartAccessNvidiaMatchesControllerAuthPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ngc", Namespace: "aif-operator"}, Data: map[string][]byte{"username": []byte("$oauthtoken"), "password": []byte("key")}}
	h := NewSettingsHandler(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(), "aif-operator")
	original := probeChartFn
	t.Cleanup(func() { probeChartFn = original })
	var mutex sync.Mutex
	seen := map[string]bool{}
	probeChartFn = func(_ context.Context, endpoint, chart, user, password string, _ []byte) credcheck.ChartResult {
		mutex.Lock()
		defer mutex.Unlock()
		seen[endpoint] = user == "$oauthtoken" && password == "key"
		return credcheck.ChartResult{RepositoryURL: endpoint, Status: "ok"}
	}
	w := httptest.NewRecorder()
	h.validateChartAccess(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"target":"nvidia","configuration":{"url":"","userSecretRef":{"name":"ngc","key":"username"},"tokenSecretRef":{"name":"ngc","key":"password"}}}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("body=%s", w.Body.String())
	}
	for _, public := range []string{"https://helm.ngc.nvidia.com/nvidia", "https://helm.ngc.nvidia.com/nvidia/blueprint"} {
		if authenticated, exists := seen[public]; !exists || authenticated {
			t.Errorf("public repo auth: %s = %v, exists=%v", public, authenticated, exists)
		}
	}
	if !seen["https://helm.ngc.nvidia.com/nvidia/runai"] {
		t.Error("runai must use the selected NGC key")
	}
	for endpoint := range seen {
		if !strings.HasPrefix(endpoint, "https://helm.ngc.nvidia.com/") {
			t.Errorf("unexpected token recipient: %s", endpoint)
		}
	}
}

func TestChartAccessUnreadableSecretDoesNotProbe(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	h := NewSettingsHandler(fake.NewClientBuilder().WithScheme(scheme).Build(), "aif-operator")
	original := probeChartFn
	t.Cleanup(func() { probeChartFn = original })
	probeChartFn = func(context.Context, string, string, string, string, []byte) credcheck.ChartResult {
		t.Error("network probe must not run")
		return credcheck.ChartResult{}
	}
	w := httptest.NewRecorder()
	h.validateChartAccess(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"target":"suseRegistry","configuration":{"url":"oci://mirror.internal/charts","userSecretRef":{"name":"missing","key":"username"},"tokenSecretRef":{"name":"missing","key":"password"}}}`)))
	var response struct {
		Results []credcheck.ChartResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Reason != "credentialsUnreadable" {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestChartAccessUsesSelectedMirrorCredentialsAndCA(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "mirror", Namespace: "aif-operator"}, Data: map[string][]byte{
		"user": []byte("mirror-user"), "token": []byte("mirror-key"), "ca": []byte("private-ca"),
	}}
	h := NewSettingsHandler(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(), "aif-operator")
	original := probeChartFn
	t.Cleanup(func() { probeChartFn = original })
	var called bool
	probeChartFn = func(_ context.Context, endpoint, chart, user, password string, ca []byte) credcheck.ChartResult {
		called = true
		if endpoint != "oci://mirror.internal/different-prefix" || chart != "qdrant" || user != "mirror-user" || password != "mirror-key" || string(ca) != "private-ca" {
			t.Error("probe must use the selected mirror path, credentials and CA")
		}
		return credcheck.ChartResult{RepositoryURL: endpoint, Status: "ok"}
	}
	w := httptest.NewRecorder()
	h.validateChartAccess(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"target":"suseRegistry","configuration":{"url":"oci://mirror.internal/different-prefix","userSecretRef":{"name":"mirror","key":"user"},"tokenSecretRef":{"name":"mirror","key":"token"},"caBundleSecretRef":{"name":"mirror","key":"ca"}}}`)))
	if w.Code != http.StatusOK || !called {
		t.Fatalf("probe did not run: code=%d", w.Code)
	}
}

func TestChartAccessUnreadableNGCKeyDoesNotBlockPublicSources(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	h := NewSettingsHandler(fake.NewClientBuilder().WithScheme(scheme).Build(), "aif-operator")
	original := probeChartFn
	t.Cleanup(func() { probeChartFn = original })
	probeChartFn = func(_ context.Context, endpoint, _ string, user, password string, _ []byte) credcheck.ChartResult {
		if user != "" || password != "" {
			t.Error("public sources must remain anonymous")
		}
		return credcheck.ChartResult{RepositoryURL: endpoint, Status: "ok"}
	}
	w := httptest.NewRecorder()
	h.validateChartAccess(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"target":"nvidia","configuration":{"userSecretRef":{"name":"missing","key":"user"},"tokenSecretRef":{"name":"missing","key":"token"}}}`)))
	var response struct {
		Results []credcheck.ChartResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	seen := map[string]credcheck.ChartResult{}
	for _, result := range response.Results {
		seen[result.RepositoryURL] = result
	}
	if seen["https://helm.ngc.nvidia.com/nvidia"].Status != "ok" || seen["https://helm.ngc.nvidia.com/nvidia/runai"].Reason != "credentialsUnreadable" {
		t.Fatalf("public and gated sources were not distinguished: %s", w.Body.String())
	}
}
