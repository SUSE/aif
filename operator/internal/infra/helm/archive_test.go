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

package helm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testArchive = []byte("fake-chart-archive-bytes")

func trustServer(srv *httptest.Server) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{RootCAs: pool}
}

// helmRepoServer serves index.yaml listing demo-1.0.0 at archiveURL (relative or
// absolute) and the archive at /charts/demo-1.0.0.tgz. With user set, every
// request must carry those basic-auth credentials.
func helmRepoServer(t *testing.T, archiveURL func(base string) string, user, pass string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != pass {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		switch r.URL.Path {
		case "/index.yaml":
			_, _ = fmt.Fprintf(w, "apiVersion: v1\nentries:\n  demo:\n  - name: demo\n    version: 1.0.0\n    urls:\n    - %s\n", archiveURL(srv.URL))
		case "/charts/demo-1.0.0.tgz":
			_, _ = w.Write(testArchive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func relativeArchive(string) string { return "charts/demo-1.0.0.tgz" }

func TestPullChartArchive_HTTPResolvesRelativeURL(t *testing.T) {
	srv := helmRepoServer(t, relativeArchive, "", "")
	got, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: srv.URL, ChartName: "demo", Version: "1.0.0", TLS: trustServer(srv), MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("PullChartArchive: %v", err)
	}
	if !bytes.Equal(got, testArchive) {
		t.Fatalf("archive = %q, want %q", got, testArchive)
	}
}

func TestPullChartArchive_HTTPBasicAuth(t *testing.T) {
	srv := helmRepoServer(t, relativeArchive, "u", "p")
	req := ChartArchiveRequest{RepoURL: srv.URL + "/", ChartName: "demo", Version: "1.0.0", TLS: trustServer(srv), MaxBytes: 1 << 20}
	if _, err := PullChartArchive(context.Background(), req); err == nil {
		t.Fatalf("want an error without credentials")
	}
	req.Auth = &RegistryAuth{Username: "u", Password: "p"}
	if _, err := PullChartArchive(context.Background(), req); err != nil {
		t.Fatalf("with credentials: %v", err)
	}
}

func TestPullChartArchive_DoesNotSendCredentialsToOtherHost(t *testing.T) {
	var sawAuth bool
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, sawAuth = r.BasicAuth()
		_, _ = w.Write(testArchive)
	}))
	t.Cleanup(other.Close)
	srv := helmRepoServer(t, func(string) string { return other.URL + "/demo-1.0.0.tgz" }, "u", "p")

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	pool.AddCert(other.Certificate())
	got, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: srv.URL, ChartName: "demo", Version: "1.0.0", Auth: &RegistryAuth{Username: "u", Password: "p"},
		TLS: &tls.Config{RootCAs: pool}, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("PullChartArchive: %v", err)
	}
	if !bytes.Equal(got, testArchive) {
		t.Fatalf("archive = %q", got)
	}
	if sawAuth {
		t.Fatalf("repository credentials were sent to a different host")
	}
}

func TestPullChartArchive_UnknownVersionErrors(t *testing.T) {
	srv := helmRepoServer(t, relativeArchive, "", "")
	_, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: srv.URL, ChartName: "demo", Version: "9.9.9", TLS: trustServer(srv), MaxBytes: 1 << 20,
	})
	if err == nil || !strings.Contains(err.Error(), "9.9.9") {
		t.Fatalf("want an error naming the missing version, got %v", err)
	}
}

func TestPullChartArchive_ArchiveOverCapErrors(t *testing.T) {
	srv := helmRepoServer(t, relativeArchive, "", "")
	_, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: srv.URL, ChartName: "demo", Version: "1.0.0", TLS: trustServer(srv), MaxBytes: 8,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want a size-cap error, got %v", err)
	}
}

func TestPullChartArchive_UntrustedCertFails(t *testing.T) {
	srv := helmRepoServer(t, relativeArchive, "", "")
	_, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: srv.URL, ChartName: "demo", Version: "1.0.0", MaxBytes: 1 << 20,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "certificate") {
		t.Fatalf("want a certificate error, got %v", err)
	}
}

// ociRegistry is a minimal OCI distribution server for one Helm chart tag:
// manifest, config and chart layer, addressed by their real sha256 digests.
func ociRegistry(t *testing.T, repoPath, tag string, user, pass string) *httptest.Server {
	t.Helper()
	digest := func(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
	config := []byte(`{"name":"demo","version":"1.0.0","apiVersion":"v2"}`)
	manifest, _ := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"mediaType": "application/vnd.cncf.helm.config.v1+json", "digest": digest(config), "size": len(config)},
		"layers": []any{map[string]any{
			"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": digest(testArchive), "size": len(testArchive),
		}},
	})
	blobs := map[string][]byte{digest(config): config, digest(testArchive): testArchive, digest(manifest): manifest}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			if u, p, ok := r.BasicAuth(); !ok || u != user || p != pass {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		prefix := "/v2/" + repoPath + "/"
		switch {
		case r.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == prefix+"manifests/"+tag || r.URL.Path == prefix+"manifests/"+digest(manifest):
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.Header().Set("Docker-Content-Digest", digest(manifest))
			w.Header().Set("Content-Length", fmt.Sprint(len(manifest)))
			if r.Method != http.MethodHead {
				_, _ = w.Write(manifest)
			}
		case strings.HasPrefix(r.URL.Path, prefix+"blobs/"):
			b, ok := blobs[strings.TrimPrefix(r.URL.Path, prefix+"blobs/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(b)))
			if r.Method != http.MethodHead {
				_, _ = w.Write(b)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPullChartArchive_OCI(t *testing.T) {
	srv := ociRegistry(t, "charts/demo", "1.0.0", "u", "p")
	ref := "oci://" + strings.TrimPrefix(srv.URL, "http://") + "/charts/demo"
	got, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: ref, Version: "1.0.0", Auth: &RegistryAuth{Username: "u", Password: "p"}, PlainHTTP: true, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("PullChartArchive: %v", err)
	}
	if !bytes.Equal(got, testArchive) {
		t.Fatalf("archive = %q, want %q", got, testArchive)
	}
}

func TestPullChartArchive_OCIOverCapErrors(t *testing.T) {
	srv := ociRegistry(t, "charts/demo", "1.0.0", "", "")
	ref := "oci://" + strings.TrimPrefix(srv.URL, "http://") + "/charts/demo"
	_, err := PullChartArchive(context.Background(), ChartArchiveRequest{RepoURL: ref, Version: "1.0.0", PlainHTTP: true, MaxBytes: 8})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want a size-cap error, got %v", err)
	}
}

func TestPullChartArchive_LargeIndexWithinIndexCap(t *testing.T) {
	srv := helmRepoServer(t, relativeArchive, "", "")
	// The index is larger than the archive cap but within the separate index cap.
	got, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: srv.URL, ChartName: "demo", Version: "1.0.0", TLS: trustServer(srv), MaxBytes: int64(len(testArchive)), MaxIndexBytes: 1 << 20,
	})
	if err != nil || !bytes.Equal(got, testArchive) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPullChartArchive_ArchiveOverCapIsErrArchiveTooLarge(t *testing.T) {
	srv := helmRepoServer(t, relativeArchive, "", "")
	_, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: srv.URL, ChartName: "demo", Version: "1.0.0", TLS: trustServer(srv), MaxBytes: 8, MaxIndexBytes: 1 << 20,
	})
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("want ErrArchiveTooLarge, got %v", err)
	}
}

func TestPullChartArchive_RefusesHTTPSToHTTPRedirect(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(testArchive) }))
	t.Cleanup(plain.Close)
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			_, _ = fmt.Fprintf(w, "apiVersion: v1\nentries:\n  demo:\n  - name: demo\n    version: 1.0.0\n    urls:\n    - %s/charts/demo-1.0.0.tgz\n", srv.URL)
		default:
			http.Redirect(w, r, plain.URL+"/demo-1.0.0.tgz", http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)
	_, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: srv.URL, ChartName: "demo", Version: "1.0.0", Auth: &RegistryAuth{Username: "u", Password: "p"}, TLS: trustServer(srv), MaxBytes: 1 << 20,
	})
	if err == nil || !strings.Contains(err.Error(), "https to http") {
		t.Fatalf("want a refused downgrade redirect, got %v", err)
	}
}

func TestPullChartArchive_TimesOut(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	start := time.Now()
	_, err := PullChartArchive(context.Background(), ChartArchiveRequest{
		RepoURL: srv.URL, ChartName: "demo", Version: "1.0.0", TLS: trustServer(srv), MaxBytes: 1 << 20, Timeout: 200 * time.Millisecond,
	})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("want a timeout error quickly, got %v after %v", err, time.Since(start))
	}
}
