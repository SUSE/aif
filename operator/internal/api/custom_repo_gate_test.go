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
	"time"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credcheck"
	"github.com/SUSE/aif-operator/internal/git"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// savedReposCR returns a Settings CR with one saved helm and one saved git repo.
func savedReposCR() *aiplatformv1alpha1.Settings {
	cr := sampleCR()
	cr.Spec.CustomRepos = []aiplatformv1alpha1.CustomRepoSpec{
		{Name: "mirror", Type: "helm", URL: "https://charts.internal/"},
		{Name: "gitmirror", Type: "git", GitRepo: "https://10.0.0.5/repo.git", GitBranch: "main"},
	}
	return cr
}

func postValidateCredentials(t *testing.T, h http.Handler, body string) (string, []validateResult) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body)
	}
	return rec.Body.String(), resp.Results
}

func TestValidateCustomRepo_PrivateNetworksOnlyForSavedRepo(t *testing.T) {
	h := newSettingsHandler(newSettingsFakeClient(t, savedReposCR()), "aif-operator")
	orig := probeHelmIndexFn
	defer func() { probeHelmIndexFn = orig }()
	var denied bool
	probeHelmIndexFn = func(ctx context.Context, _, _, _ string, _ []byte, _ bool) credcheck.Result {
		denied = credcheck.PrivateNetworksDenied(ctx)
		return credcheck.Result{Status: credcheck.StatusOK}
	}
	cases := []struct {
		name, override string
		wantDenied     bool
	}{
		{"saved name and url", `"name":"mirror","url":"https://charts.internal"`, false},
		{"no name", `"url":"https://charts.internal/"`, true},
		{"saved name, other url", `"name":"mirror","url":"https://10.0.0.9/"`, true},
		{"unknown name", `"name":"other","url":"https://charts.internal/"`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			denied = false
			body := `{"targets":["customRepo"],"overrides":{"customRepo":{"type":"helm",` + tc.override + `}}}`
			if _, results := postValidateCredentials(t, h, body); len(results) != 1 || results[0].Status != statusOK {
				t.Fatalf("results=%+v", results)
			}
			if denied != tc.wantDenied {
				t.Fatalf("private networks denied=%v want %v", denied, tc.wantDenied)
			}
		})
	}
}

func TestValidateCustomRepo_GitHostCheckedUpFront(t *testing.T) {
	h := newSettingsHandler(newSettingsFakeClient(t, savedReposCR()), "aif-operator")
	orig := gitCheckAuthFn
	defer func() { gitCheckAuthFn = orig }()
	var called bool
	gitCheckAuthFn = func(*git.Client, context.Context) error {
		called = true
		return nil
	}

	unsaved := `{"targets":["customRepo"],"overrides":{"customRepo":{"type":"git","gitRepo":"https://10.0.0.5/repo.git","branch":"main"}}}`
	_, results := postValidateCredentials(t, h, unsaved)
	if len(results) != 1 || results[0].Status != statusError || results[0].Message != msgRepoUnreachable || called {
		t.Fatalf("unsaved private git repo was probed: called=%v results=%+v", called, results)
	}

	saved := `{"targets":["customRepo"],"overrides":{"customRepo":{"type":"git","name":"gitmirror","gitRepo":"https://10.0.0.5/repo.git","branch":"main"}}}`
	_, results = postValidateCredentials(t, h, saved)
	if len(results) != 1 || results[0].Status != statusOK || !called {
		t.Fatalf("saved private git repo was not probed: called=%v results=%+v", called, results)
	}
}

func TestValidateCustomRepo_RefusesCredentialsOverHTTP(t *testing.T) {
	const ns = "aif-operator"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cred", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("u"), "token": []byte("t")},
	}
	h := newSettingsHandler(newSettingsFakeClient(t, sampleCR(), secret), ns)
	origHelm, origGit := probeHelmIndexFn, gitCheckAuthFn
	defer func() { probeHelmIndexFn, gitCheckAuthFn = origHelm, origGit }()
	probeHelmIndexFn = func(context.Context, string, string, string, []byte, bool) credcheck.Result {
		t.Fatal("helm probe must not run")
		return credcheck.Result{}
	}
	gitCheckAuthFn = func(*git.Client, context.Context) error {
		t.Fatal("git probe must not run")
		return nil
	}
	for name, override := range map[string]string{
		"helm": `"type":"helm","url":"http://charts.example.com","userSecretRef":{"name":"cred","key":"user"},"tokenSecretRef":{"name":"cred","key":"token"}`,
		"git":  `"type":"git","gitRepo":"http://git.example.com/repo.git","branch":"main","credSecretRef":{"name":"cred","key":"token"}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"targets":["customRepo"],"overrides":{"customRepo":{` + override + `}}}`
			_, results := postValidateCredentials(t, h, body)
			if len(results) != 1 || results[0].Status != statusError || results[0].Message != errCleartextCredentials {
				t.Fatalf("results=%+v", results)
			}
		})
	}
}

func TestValidateCustomRepo_LatencyOnlyOnSuccess(t *testing.T) {
	h := newSettingsHandler(newSettingsFakeClient(t, sampleCR()), "aif-operator")
	orig := probeHelmIndexFn
	defer func() { probeHelmIndexFn = orig }()
	status := credcheck.StatusError
	probeHelmIndexFn = func(context.Context, string, string, string, []byte, bool) credcheck.Result {
		time.Sleep(2 * time.Millisecond)
		return credcheck.Result{Status: status}
	}
	body := `{"targets":["customRepo"],"overrides":{"customRepo":{"type":"helm","url":"https://charts.example.com"}}}`
	if raw, _ := postValidateCredentials(t, h, body); strings.Contains(raw, "latencyMs") {
		t.Fatalf("error result carries latency: %s", raw)
	}
	status = credcheck.StatusOK
	if raw, _ := postValidateCredentials(t, h, body); !strings.Contains(raw, "latencyMs") {
		t.Fatalf("ok result lost latency: %s", raw)
	}
}

func TestChartAccessCustomRepoPrivateNetworksOnlyForSavedRepo(t *testing.T) {
	h := NewSettingsHandler(newSettingsFakeClient(t, savedReposCR()), "aif-operator")
	original := probeChartFn
	t.Cleanup(func() { probeChartFn = original })
	var denied bool
	probeChartFn = func(ctx context.Context, endpoint, chart, _, _ string, _ []byte) credcheck.ChartResult {
		denied = credcheck.PrivateNetworksDenied(ctx)
		return credcheck.ChartResult{RepositoryURL: endpoint, ChartName: chart, Status: statusError, LatencyMs: 42}
	}
	for _, tc := range []struct {
		name, config string
		wantDenied   bool
	}{
		{"saved", `{"name":"mirror","url":"https://charts.internal"}`, false},
		{"unsaved", `{"url":"https://charts.internal"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			body := `{"target":"customRepo","chartName":"x","configuration":` + tc.config + `}`
			h.validateChartAccess(w, httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-chart-access", strings.NewReader(body)))
			if denied != tc.wantDenied {
				t.Fatalf("private networks denied=%v want %v", denied, tc.wantDenied)
			}
			var response struct {
				Results []credcheck.ChartResult `json:"results"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Results) != 1 || response.Results[0].LatencyMs != 0 {
				t.Fatalf("error result carries latency: %+v", response.Results)
			}
		})
	}
}
