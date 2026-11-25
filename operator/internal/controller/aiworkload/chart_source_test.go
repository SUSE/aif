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

package aiworkload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

type fakeCatalogFetcher struct{ calls int }

func (f *fakeCatalogFetcher) FetchChart(_ context.Context, _, _, _, _ string) ([]byte, error) {
	f.calls++
	return []byte("from-catalog"), nil
}

func chartSourceReconciler(t *testing.T, puller chartArchivePuller, fetcher rancher.ChartFetcher) *AIWorkloadReconciler {
	t.Helper()
	scheme := gitRepoTestScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "repo-auth", Namespace: "cattle-system"},
		Data:       map[string][]byte{"username": []byte("u"), "password": []byte("p")},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	return &AIWorkloadReconciler{Client: cl, Scheme: scheme, ChartFetcher: fetcher, ChartPuller: puller}
}

var demoComponent = aiplatformv1alpha1.BlueprintComponent{ChartRepo: "repo", ChartName: "demo", ChartVersion: "1.0.0"}

func TestFetchChartArchive_GitUsesCatalog(t *testing.T) {
	cat := &fakeCatalogFetcher{}
	puller := func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) {
		t.Fatal("direct puller must not be used for a git-backed repo")
		return nil, nil
	}
	r := chartSourceReconciler(t, puller, cat)
	got, err := r.fetchChartArchive(context.Background(), demoComponent, clusterRepoInfo{Name: "repo", Kind: repoKindGit})
	if err != nil || string(got) != "from-catalog" || cat.calls != 1 {
		t.Fatalf("got %q, %v (catalog calls %d)", got, err, cat.calls)
	}
}

func TestFetchChartArchive_GitWithoutChartFetcher(t *testing.T) {
	r := chartSourceReconciler(t, nil, nil)
	_, err := r.fetchChartArchive(context.Background(), demoComponent, clusterRepoInfo{Name: "repo", Kind: repoKindGit})
	if !errors.Is(err, errGitChartFetcherUnavailable) {
		t.Fatalf("want errGitChartFetcherUnavailable, got %v", err)
	}
}

func TestFetchChartArchive_HTTPUsesDirectPullWithRepoCredentials(t *testing.T) {
	var got infrahelm.ChartArchiveRequest
	r := chartSourceReconciler(t, func(_ context.Context, req infrahelm.ChartArchiveRequest) ([]byte, error) {
		got = req
		return []byte("direct"), nil
	}, nil)
	info := clusterRepoInfo{Name: "repo", Kind: repoKindHTTP, URL: "https://charts.example.com", ClientSecret: "repo-auth", ClientSecretNS: "cattle-system"}
	archive, err := r.fetchChartArchive(context.Background(), demoComponent, info)
	if err != nil || string(archive) != "direct" {
		t.Fatalf("got %q, %v", archive, err)
	}
	if got.RepoURL != info.URL || got.ChartName != "demo" || got.Version != demoComponent.ChartVersion {
		t.Fatalf("request = %+v", got)
	}
	if got.Auth == nil || got.Auth.Username != "u" || got.Auth.Password != "p" {
		t.Fatalf("auth = %+v, want the ClusterRepo clientSecret credentials", got.Auth)
	}
	if got.MaxBytes != maxChartArchiveBytes {
		t.Fatalf("MaxBytes = %d, want maxChartArchiveBytes", got.MaxBytes)
	}
}

func TestFetchChartArchive_OCIUsesOCIRefAndPlainHTTP(t *testing.T) {
	var got infrahelm.ChartArchiveRequest
	r := chartSourceReconciler(t, func(_ context.Context, req infrahelm.ChartArchiveRequest) ([]byte, error) {
		got = req
		return []byte("x"), nil
	}, nil)
	info := clusterRepoInfo{Name: "repo", Kind: repoKindOCI, URL: "oci://registry.example.com/charts", InsecurePlainHTTP: true}
	if _, err := r.fetchChartArchive(context.Background(), demoComponent, info); err != nil {
		t.Fatalf("fetchChartArchive: %v", err)
	}
	if got.RepoURL != ociChartRef(info.URL, "demo") || !got.PlainHTTP || got.Auth != nil {
		t.Fatalf("request = %+v, want oci ref %q, plain HTTP, no auth", got, ociChartRef(info.URL, "demo"))
	}
}

func TestFetchChartArchive_TLSFromClusterRepo(t *testing.T) {
	var got infrahelm.ChartArchiveRequest
	r := chartSourceReconciler(t, func(_ context.Context, req infrahelm.ChartArchiveRequest) ([]byte, error) {
		got = req
		return []byte("x"), nil
	}, nil)
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	info := clusterRepoInfo{Name: "repo", Kind: repoKindHTTP, URL: "https://charts.example.com", CABundle: caPEM, InsecureSkipTLSVerify: true}
	if _, err := r.fetchChartArchive(context.Background(), demoComponent, info); err != nil {
		t.Fatalf("fetchChartArchive: %v", err)
	}
	if got.TLS == nil || got.TLS.RootCAs == nil || !got.TLS.InsecureSkipVerify {
		t.Fatalf("TLS = %+v, want RootCAs set and InsecureSkipVerify", got.TLS)
	}
}

func TestFetchChartArchive_NoTLSSettingsLeavesTLSNil(t *testing.T) {
	var got infrahelm.ChartArchiveRequest
	r := chartSourceReconciler(t, func(_ context.Context, req infrahelm.ChartArchiveRequest) ([]byte, error) {
		got = req
		return []byte("x"), nil
	}, nil)
	if _, err := r.fetchChartArchive(context.Background(), demoComponent, clusterRepoInfo{Name: "repo", Kind: repoKindHTTP, URL: "https://c.example.com"}); err != nil {
		t.Fatalf("fetchChartArchive: %v", err)
	}
	if got.TLS != nil {
		t.Fatalf("TLS = %+v, want nil (system roots)", got.TLS)
	}
}

func TestClusterRepoTLSConfig_RejectsBundleWithoutCertificates(t *testing.T) {
	if _, err := clusterRepoTLSConfig(clusterRepoInfo{CABundle: []byte(testPEM)}, nil); err == nil {
		t.Fatalf("want an error for a caBundle with no parseable certificates")
	}
}

func TestFetchChartArchive_SeparateIndexCapAndTerminalTooLarge(t *testing.T) {
	var got infrahelm.ChartArchiveRequest
	r := chartSourceReconciler(t, func(_ context.Context, req infrahelm.ChartArchiveRequest) ([]byte, error) {
		got = req
		return nil, fmt.Errorf("%w: body exceeds", infrahelm.ErrArchiveTooLarge)
	}, nil)
	_, err := r.fetchChartArchive(context.Background(), demoComponent, clusterRepoInfo{Name: "repo", Kind: repoKindHTTP, URL: "https://c.example.com"})
	if got.MaxIndexBytes != maxChartIndexBytes {
		t.Fatalf("MaxIndexBytes = %d, want maxChartIndexBytes", got.MaxIndexBytes)
	}
	if !errors.Is(err, errChartTooLarge) || !strings.Contains(err.Error(), "remove the overlay") {
		t.Fatalf("an oversized archive must be the terminal ChartTooLarge with the overlay hint, got %v", err)
	}
}

func TestFetchChartArchive_OversizeIndexIsTerminal(t *testing.T) {
	r := chartSourceReconciler(t, func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) {
		return nil, fmt.Errorf("read repository index: %w", infrahelm.ErrIndexTooLarge)
	}, nil)
	_, err := r.fetchChartArchive(context.Background(), demoComponent, clusterRepoInfo{Name: "repo", Kind: repoKindHTTP, URL: "https://c.example.com"})
	if !errors.Is(err, errChartTooLarge) || !strings.Contains(err.Error(), "index.yaml") {
		t.Fatalf("an oversized index must be the terminal ChartTooLarge, got %v", err)
	}
}

func TestFetchChartArchive_TrustsClientSecretCACerts(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.yaml" {
			_, _ = w.Write([]byte("apiVersion: v1\nentries:\n  demo:\n  - name: demo\n    version: 1.0.0\n    urls: [demo-1.0.0.tgz]\n"))
			return
		}
		_, _ = w.Write([]byte("TGZ"))
	}))
	defer srv.Close()
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	scheme := gitRepoTestScheme()
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "repo-auth", Namespace: "cattle-system"},
		Data: map[string][]byte{"username": []byte("u"), "password": []byte("p"), "cacerts": caPEM}}
	r := &AIWorkloadReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(sec).Build(), Scheme: scheme, ChartPuller: infrahelm.PullChartArchive}
	info := clusterRepoInfo{Name: "repo", Kind: repoKindHTTP, URL: srv.URL, ClientSecret: "repo-auth", ClientSecretNS: "cattle-system"}
	if got, err := r.fetchChartArchive(context.Background(), demoComponent, info); err != nil || string(got) != "TGZ" {
		t.Fatalf("private CA only in the clientSecret's cacerts: got %q, %v", got, err)
	}
	// Unioned with spec.caBundle: an unrelated caBundle must not hide cacerts.
	other := httptest.NewTLSServer(http.NotFoundHandler())
	defer other.Close()
	info.CABundle = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: other.Certificate().Raw})
	if got, err := r.fetchChartArchive(context.Background(), demoComponent, info); err != nil || string(got) != "TGZ" {
		t.Fatalf("caBundle plus cacerts: got %q, %v", got, err)
	}
}

func TestClusterRepoTLSConfig_RejectsSecretCAWithoutCertificates(t *testing.T) {
	if _, err := clusterRepoTLSConfig(clusterRepoInfo{}, []byte("not pem")); err == nil {
		t.Fatal("want an error for cacerts with no parseable certificates")
	}
}

func TestChartTgzToBundleResources_CountsNonRegularEntries(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i := 0; i <= maxChartUnpackedBytes/512; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("chart/d%d/", i), Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	if _, _, err := chartTgzToBundleResources(buf.Bytes()); !errors.Is(err, errChartTooLarge) {
		t.Fatalf("a flood of directory entries must hit the unpack limit, got %v", err)
	}
}

func TestFetchChartArchive_AuthFailureHints(t *testing.T) {
	const ngcURL = "https://helm.ngc.nvidia.com/nvidia/runai"
	const otherURL = "https://charts.example.com"
	ngcHint := "for NVIDIA gated repositories (NIM, cuOpt, NVIDIA Run:ai) use an NGC API key from an organization with that entitlement"
	for _, tc := range []struct {
		name      string
		code      int
		url       string
		secret    bool
		want      string
		wantNoNGC bool
	}{
		{"403 NGC with secret", 403, ngcURL, true, `the credentials configured for repository "repo" authenticate but are not allowed to download this chart (HTTP 403): the account behind them probably lacks the required entitlement; ` + ngcHint, false},
		{"403 other host with secret", 403, otherURL, true, `the credentials configured for repository "repo" authenticate but are not allowed to download this chart (HTTP 403): the account behind them probably lacks the required entitlement: fetch chart`, true},
		{"401 with secret", 401, ngcURL, true, `the credentials configured for repository "repo" were rejected (HTTP 401): check them in Settings`, true},
		{"401 without secret", 401, otherURL, false, `repository "repo" requires credentials but none are configured for it in Settings`, true},
		{"403 without secret", 403, ngcURL, false, `repository "repo" requires credentials but none are configured for it in Settings`, true},
		{"404 not wrapped", 404, ngcURL, true, "", true},
		{"500 not wrapped", 500, ngcURL, true, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.url + "/charts/demo-1.0.0.tgz"
			status := fmt.Sprintf("%d %s", tc.code, http.StatusText(tc.code))
			r := chartSourceReconciler(t, func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) {
				return nil, fmt.Errorf("download chart archive: %w", &infrahelm.HTTPStatusError{URL: target, Code: tc.code, Status: status})
			}, nil)
			info := clusterRepoInfo{Name: "repo", Kind: repoKindHTTP, URL: tc.url}
			if tc.secret {
				info.ClientSecret, info.ClientSecretNS = "repo-auth", "cattle-system"
			}
			_, err := r.fetchChartArchive(context.Background(), demoComponent, info)
			if err == nil {
				t.Fatal("want an error")
			}
			msg := err.Error()
			original := "fetch chart demo@1.0.0 from repo \"repo\": download chart archive: GET " + target + ": " + status
			if !strings.HasSuffix(msg, original) {
				t.Fatalf("message %q lost the original text %q", msg, original)
			}
			if tc.want == "" {
				if msg != original {
					t.Fatalf("status %d must not be wrapped, got %q", tc.code, msg)
				}
			} else if !strings.HasPrefix(msg, tc.want) {
				t.Fatalf("message %q\nwant prefix %q", msg, tc.want)
			}
			if tc.wantNoNGC && strings.Contains(msg, ngcHint) {
				t.Fatalf("NGC-specific hint in %q", msg)
			}
			if strings.Contains(msg, "\"p\"") || strings.Contains(msg, ":p@") {
				t.Fatalf("message leaks credentials: %q", msg)
			}
			var se *infrahelm.HTTPStatusError
			if !errors.As(err, &se) || se.Code != tc.code {
				t.Fatalf("HTTPStatusError not preserved in the chain: %v", err)
			}
		})
	}
}
