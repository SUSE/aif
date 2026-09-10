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

package credcheck

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testChartManifest = `{"schemaVersion":2,"config":{"mediaType":"application/vnd.cncf.helm.config.v1+json"},"layers":[{"mediaType":"application/vnd.cncf.helm.chart.content.v1.tar+gzip"}]}`

func testChartCA(server *httptest.Server) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
}

func TestProbeChartOCIUsesScopedAccessAndPrivatePrefix(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.URL.Path)
				switch r.URL.Path {
				case "/v2/":
					w.WriteHeader(http.StatusOK) // Root success must never imply chart access.
				case "/v2/mirrors/custom-prefix/qdrant/tags/list":
					if r.URL.Query().Get("n") != "1" {
						t.Error("expected a bounded tag sample")
					}
					if status != http.StatusOK {
						w.WriteHeader(status)
						return
					}
					_, _ = w.Write([]byte(`{"name":"mirrors/custom-prefix/qdrant","tags":["1.2.3"]}`))
				case "/v2/mirrors/custom-prefix/qdrant/manifests/1.2.3":
					w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
					_, _ = w.Write([]byte(testChartManifest))
				default:
					t.Errorf("unexpected request %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			endpoint := "oci://" + hostOf(t, server.URL) + "/mirrors/custom-prefix"
			result := ProbeChart(context.Background(), endpoint, "qdrant", "user", "bad-key", testChartCA(server))
			if result.RepositoryURL != endpoint {
				t.Fatalf("endpoint=%q", result.RepositoryURL)
			}
			if status == http.StatusOK {
				if result.Status != "ok" || result.Check != "manifest" || result.Version != "1.2.3" {
					t.Fatalf("result=%+v", result)
				}
			} else {
				wantReason := "accessDenied"
				if status == http.StatusNotFound {
					wantReason = "notFound"
				}
				if result.Status != "failed" || result.Reason != wantReason || result.HTTPStatus != status {
					t.Fatalf("result=%+v", result)
				}
			}
			for _, path := range requests {
				if !strings.HasPrefix(path, "/v2/mirrors/custom-prefix/qdrant/") {
					t.Errorf("request escaped mirror: %s", path)
				}
			}
		})
	}
}

func TestProbeChartOCIRejectsUnentitledBearerToken(t *testing.T) {
	var server *httptest.Server
	var scopedTokenRequested bool
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/ai/charts/qdrant/tags/list":
			if r.Header.Get("Authorization") == "Bearer root-only-token" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"errors":[{"code":"DENIED","message":"secret-should-not-appear"}]}`))
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+server.URL+`/token",service="registry",scope="repository:ai/charts/qdrant:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/token":
			if !strings.Contains(r.URL.Query().Get("scope"), "repository:ai/charts/qdrant:pull") {
				t.Error("missing chart pull scope")
			}
			scopedTokenRequested = true
			_, _ = w.Write([]byte(`{"token":"root-only-token"}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer server.Close()
	result := ProbeChart(context.Background(), "oci://"+hostOf(t, server.URL)+"/ai/charts", "qdrant", "user", "key", testChartCA(server))
	if !scopedTokenRequested || result.Status != "failed" || result.Reason != "accessDenied" || result.HTTPStatus != 403 {
		t.Fatalf("result=%+v", result)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "secret-should-not-appear") {
		t.Fatal("remote error body exposed")
	}
}

func TestProbeChartHTTPSChecksFileAfterPublicIndex(t *testing.T) {
	for _, fileStatus := range []int{http.StatusOK, http.StatusPartialContent, http.StatusForbidden} {
		t.Run(http.StatusText(fileStatus), func(t *testing.T) {
			var fileChecked bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/team/index.yaml":
					_, _ = w.Write([]byte("apiVersion: v1\nentries:\n  runai:\n    - version: 1.2.3\n      urls: [runai-1.2.3.tgz]\n"))
				case "/team/runai-1.2.3.tgz":
					if r.Method == http.MethodHead {
						w.WriteHeader(http.StatusOK)
						return // A public HEAD must not mask a denied download.
					}
					fileChecked = true
					user, password, _ := r.BasicAuth()
					if user != "$oauthtoken" || password != "key" || r.Method != http.MethodGet || r.Header.Get("Range") != "bytes=0-0" {
						t.Error("chart credential/method mismatch")
					}
					w.WriteHeader(fileStatus)
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
			}))
			defer server.Close()
			result := ProbeChart(context.Background(), server.URL+"/team", "", "$oauthtoken", "key", testChartCA(server))
			if !fileChecked || result.ChartName != "runai" || result.Version != "1.2.3" || result.Check != "chartFile" {
				t.Fatalf("result=%+v", result)
			}
			if fileStatus == http.StatusForbidden && result.Reason != "accessDenied" {
				t.Fatalf("result=%+v", result)
			}
			if fileStatus != http.StatusForbidden && result.Status != "ok" {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestProbeChartHTTPSExplainsInvalidAndDeniedIndexes(t *testing.T) {
	for _, status := range []int{200, 403} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"no API version specified"}`))
		}))
		result := ProbeChart(context.Background(), server.URL+"/nvidia/runai", "", "user", "wrong", testChartCA(server))
		server.Close()
		want := "invalidIndex"
		if status == 403 {
			want = "accessDenied"
		}
		if result.Reason != want || result.Status == "ok" {
			t.Fatalf("result=%+v", result)
		}
	}
}

func TestProbeChartNeverFollowsMirrorIndexToPublicSource(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("apiVersion: v1\nentries:\n  qdrant:\n    - version: 1.2.3\n      urls: [https://upstream.invalid/public/chart.tgz]\n"))
	}))
	defer server.Close()
	result := ProbeChart(context.Background(), server.URL+"/private", "qdrant", "user", "private-key", testChartCA(server))
	if result.Status == "ok" || result.Reason != "outsideRepository" {
		t.Fatalf("result=%+v", result)
	}
}

func TestProbeChartTrustTimeoutAndInputErrors(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	result := ProbeChart(context.Background(), server.URL, "sample", "", "", nil)
	if result.Reason != "tls" {
		t.Fatalf("untrusted certificate: %+v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = ProbeChart(ctx, server.URL, "sample", "", "", testChartCA(server))
	if result.Reason != "timeout" {
		t.Fatalf("cancelled probe: %+v", result)
	}
	for _, chart := range []string{"../qdrant", "other/qdrant", "qdrant?token=key"} {
		result = ProbeChart(context.Background(), "oci://example.invalid/private", chart, "", "", nil)
		if result.Reason != "configuration" {
			t.Fatalf("invalid chart: %+v", result)
		}
	}
}
