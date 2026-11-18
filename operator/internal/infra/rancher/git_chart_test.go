package rancher

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"helm.sh/helm/v3/pkg/chart/loader"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func chartArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func commitFiles(t *testing.T, dir string, files map[string][]byte) (*gogit.Repository, plumbing.Hash) {
	t.Helper()
	r, err := gogit.PlainOpen(dir)
	if err != nil {
		r, err = gogit.PlainInit(dir, false)
	}
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		filename := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Add(name); err != nil {
			t.Fatal(err)
		}
	}
	hash, err := w.Commit("fixture", &gogit.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.invalid", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	return r, hash
}

func gitChartFiles(t *testing.T, address, digest string, archive []byte) map[string][]byte {
	t.Helper()
	if digest == "" {
		digest = fmt.Sprintf("%x", sha256.Sum256(archive))
	}
	return map[string][]byte{
		"index.yaml":            []byte(fmt.Sprintf("apiVersion: v1\nentries:\n  demo:\n  - name: demo\n    version: 1.0.0\n    digest: %s\n    urls: [%q]\n", digest, address)),
		"assets/demo-1.0.0.tgz": archive,
	}
}

func TestGitChartIndexAndArchiveValidation(t *testing.T) {
	valid := chartArchive(t, map[string]string{"demo/Chart.yaml": "apiVersion: v2\nname: demo\nversion: 1.0.0\n"})
	for _, tc := range []struct {
		name, address, digest, want string
		archive                     []byte
	}{
		{name: "relative archive", address: "assets/demo-1.0.0.tgz", archive: valid},
		{name: "dot relative archive", address: "./assets/demo-1.0.0.tgz", archive: valid},
		{name: "no public fallback", address: "https://public.invalid/demo.tgz", archive: valid, want: "repository-relative"},
		{name: "no scheme relative URL", address: "//public.invalid/demo.tgz", archive: valid, want: "repository-relative"},
		{name: "no traversal", address: "../assets/demo.tgz", archive: valid, want: "repository-relative"},
		{name: "no encoded traversal", address: "%2e%2e/assets/demo.tgz", archive: valid, want: "repository-relative"},
		{name: "digest mismatch", address: "assets/demo-1.0.0.tgz", digest: "bad", archive: valid, want: "digest"},
		{name: "wrong chart", address: "assets/demo-1.0.0.tgz", archive: chartArchive(t, map[string]string{"demo/Chart.yaml": "name: other\nversion: 1.0.0\n"}), want: "metadata"},
		{name: "unsafe archive", address: "assets/demo-1.0.0.tgz", archive: chartArchive(t, map[string]string{"../Chart.yaml": "name: demo\nversion: 1.0.0\n"}), want: "unsafe path"},
		{name: "expansion limit", address: "assets/demo-1.0.0.tgz", archive: chartArchive(t, map[string]string{"demo/large": strings.Repeat("a", maxGitChartUnpackedBytes+1)}), want: "unpacked size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, hash := commitFiles(t, t.TempDir(), gitChartFiles(t, tc.address, tc.digest, tc.archive))
			commit, err := r.CommitObject(hash)
			if err != nil {
				t.Fatal(err)
			}
			tree, err := commit.Tree()
			if err != nil {
				t.Fatal(err)
			}
			got, err := chartFromGitTree(tree, "demo", "1.0.0")
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want %q", err, tc.want)
				}
			} else if err != nil || !bytes.Equal(got, tc.archive) {
				t.Fatalf("archive mismatch or error: %v", err)
			}
		})
	}
}

func TestGitChartPrivateHTTPSPinnedCommitAndCredentialRotation(t *testing.T) {
	// Exercise the real Git wire protocol, certificate verification and basic
	// auth. git is a test server only; the operator does not execute git.
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git http-backend is required for this integration test")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "charts.git")
	archive := chartArchive(t, map[string]string{"demo/Chart.yaml": "name: demo\nversion: 1.0.0\n"})
	repository, oldCommit := commitFiles(t, dir, gitChartFiles(t, "assets/demo-1.0.0.tgz", "", archive))
	backend := &cgi.Handler{Path: gitBinary, Args: []string{"http-backend"}, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "repository-user" || password != "repository-password" {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	cr := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "catalog.cattle.io/v1", "kind": "ClusterRepo",
		"metadata": map[string]any{"name": "private"},
		"spec":     map[string]any{"gitRepo": server.URL + "/charts.git", "gitBranch": "master", "caBundle": base64.StdEncoding.EncodeToString(ca), "clientSecret": map[string]any{"name": "git-auth", "namespace": "cattle-system"}},
	}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "git-auth", Namespace: "cattle-system"}, Data: map[string][]byte{"username": []byte("repository-user"), "password": []byte("repository-password")}}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr, secret).Build()
	fetcher := NewGitChartFetcher(c)
	got, err := fetcher.FetchChart(context.Background(), "private", "demo", "1.0.0", oldCommit.String())
	if err != nil || !bytes.Equal(got, archive) {
		t.Fatalf("pinned private Git fetch failed: %v", err)
	}
	// A server without exact-hash fetch support can serve the indexed branch.
	// Once HEAD moves, it must fail rather than substitute the new content.
	commitFiles(t, dir, map[string][]byte{"index.yaml": []byte("apiVersion: v1\nentries: {}\n")})
	if _, err := fetcher.FetchChart(context.Background(), "private", "demo", "1.0.0", oldCommit.String()); err == nil {
		t.Fatal("accepted a moving branch instead of the indexed commit")
	}
	config, err := repository.Config()
	if err != nil {
		t.Fatal(err)
	}
	config.Raw.Section("uploadpack").SetOption("allowReachableSHA1InWant", "true")
	if err := repository.SetConfig(config); err != nil {
		t.Fatal(err)
	}
	got, err = fetcher.FetchChart(context.Background(), "private", "demo", "1.0.0", oldCommit.String())
	if err != nil || !bytes.Equal(got, archive) {
		t.Fatalf("indexed commit after HEAD moved: %v", err)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(secret), secret); err != nil {
		t.Fatal(err)
	}
	secret.Data["password"] = []byte("revoked-password")
	if err := c.Update(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	if _, err := fetcher.FetchChart(context.Background(), "private", "demo", "1.0.0", oldCommit.String()); err == nil || strings.Contains(err.Error(), "revoked-password") {
		t.Fatalf("must read rotated credentials and redact errors, got %v", err)
	}

	t.Run("mutual TLS and existing DER trust", func(t *testing.T) {
		// Require possession of the fixture's client key and explicitly pin the
		// peer certificate in this test server, without a user bearer token.
		mtls := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !bytes.Equal(r.TLS.PeerCertificates[0].Raw, server.Certificate().Raw) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			backend.ServeHTTP(w, r)
		}))
		mtls.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12}
		mtls.StartTLS()
		defer mtls.Close()
		privateKey, err := x509.MarshalPKCS8PrivateKey(server.TLS.Certificates[0].PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		credential := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "git-mtls", Namespace: "cattle-system"}, Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{corev1.TLSCertKey: ca, corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})}}
		if err := c.Create(context.Background(), credential); err != nil {
			t.Fatal(err)
		}
		mirror := cr.DeepCopy()
		mirror.SetName("mutual-tls")
		mirror.SetResourceVersion("")
		_ = unstructured.SetNestedField(mirror.Object, mtls.URL+"/charts.git", "spec", "gitRepo")
		_ = unstructured.SetNestedField(mirror.Object, "git-mtls", "spec", "clientSecret", "name")
		_ = unstructured.SetNestedField(mirror.Object, base64.StdEncoding.EncodeToString(mtls.Certificate().Raw), "spec", "caBundle")
		if err := c.Create(context.Background(), mirror); err != nil {
			t.Fatal(err)
		}
		got, err := fetcher.FetchChart(context.Background(), "mutual-tls", "demo", "1.0.0", oldCommit.String())
		if err != nil || !bytes.Equal(got, archive) {
			t.Fatalf("mutual TLS Git fetch: %v", err)
		}
	})
}

func TestGitChartDirectoriesAndNestedIndexes(t *testing.T) {
	for _, indexPath := range []string{"", "index.yaml", "metadata/index.yaml"} {
		t.Run("index="+indexPath, func(t *testing.T) {
			files := map[string][]byte{
				"charts/demo/Chart.yaml":               []byte("apiVersion: v2\nname: demo\nversion: 1.0.0\ndependencies:\n- name: helper\n  version: 1.0.0\n  repository: https://must-not-be-contacted.invalid\n"),
				"charts/demo/.helmignore":              []byte("ignore.txt\n"),
				"charts/demo/ignore.txt":               []byte("do-not-package"),
				"charts/demo/values.yaml":              []byte("replicas: 1\n"),
				"charts/demo/charts/helper/Chart.yaml": []byte("apiVersion: v2\nname: helper\nversion: 1.0.0\n"),
			}
			if indexPath != "" {
				files[indexPath] = []byte("apiVersion: v1\nentries:\n  demo:\n  - name: demo\n    version: 1.0.0\n    urls: [file://charts/demo]\n")
			}
			repository, revision := commitFiles(t, t.TempDir(), files)
			commit, _ := repository.CommitObject(revision)
			tree, _ := commit.Tree()
			archive, err := chartFromGitTree(tree, "demo", "1.0.0")
			if err != nil {
				t.Fatal(err)
			}
			chart, err := loader.LoadArchive(bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range chart.Files {
				if file.Name == "ignore.txt" {
					t.Fatal(".helmignore was not honored")
				}
			}
			if len(chart.Dependencies()) != 1 {
				t.Fatal("bundled dependency was not packaged")
			}
			if _, err := chartFromGitTree(tree, "helper", "1.0.0"); err == nil {
				t.Fatal("exposed a dependency as a separate repository chart")
			}
		})
	}
}

func TestGitChartRefusesUntrustedSSHAndInsecureTLS(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	f := NewGitChartFetcher(fake.NewClientBuilder().WithScheme(scheme).Build())
	cr := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	endpoint, _ := transport.NewEndpoint("ssh://git@mirror.invalid/charts.git")
	if _, err := f.credentials(context.Background(), cr, endpoint, t.TempDir()); err == nil || !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("SSH without trust: %v", err)
	}
	_ = unstructured.SetNestedField(cr.Object, true, "spec", "insecureSkipTLSVerify")
	endpoint, _ = transport.NewEndpoint("https://mirror.invalid/charts.git")
	if _, err := f.credentials(context.Background(), cr, endpoint, t.TempDir()); err == nil || !strings.Contains(err.Error(), "TLS verification") {
		t.Fatalf("insecure TLS: %v", err)
	}
}
