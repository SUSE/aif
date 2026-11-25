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
	"context"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	igit "github.com/SUSE/aif-operator/internal/git"
)

// serveGitHTTPS serves a new bare repo over smart HTTPS (git http-backend)
// and returns its URL, the server's CA (PEM) and a function committing one
// file to main, which returns the commit hash.
func serveGitHTTPS(t *testing.T) (string, []byte, func(path, content string) string) {
	t.Helper()
	out, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skip("git not available")
	}
	backend := filepath.Join(strings.TrimSpace(string(out)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend not available")
	}
	root := t.TempDir()
	bare := filepath.Join(root, "overlays.git")
	if _, err := gogit.PlainInit(bare, true); err != nil {
		t.Fatal(err)
	}
	h := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	srv := httptest.NewTLSServer(h)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	t.Cleanup(srv.Close)
	writer := igit.NewForRepo("file://"+bare, "main", nil, nil, false)
	commit := func(path, content string) string {
		hash, err := writer.WriteFile(context.Background(), path, content, "update")
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	return srv.URL + "/overlays.git", ca, commit
}

// servedOverlayRepo is the overlay ClusterRepo for a serveGitHTTPS repo,
// trusting its CA through spec.caBundle.
func servedOverlayRepo(url string, ca []byte, commit string) *unstructured.Unstructured {
	repo := overlayGitRepo(true, commit)
	_ = unstructured.SetNestedField(repo.Object, url, "spec", "gitRepo")
	_ = unstructured.SetNestedField(repo.Object, base64.StdEncoding.EncodeToString(ca), "spec", "caBundle")
	return repo
}

// The overlay is read at the commit the ClusterRepo indexed (the one the
// fingerprint records), not at the branch tip, and a commit the branch does
// not have is a retryable error rather than a silent read of another commit.
func TestResolveKustomizeOverlay_GitRefReadsIndexedCommit(t *testing.T) {
	url, ca, commit := serveGitHTTPS(t)
	indexed := commit("prod/kustomization.yaml", "patches: [] # v1\n")
	commit("prod/kustomization.yaml", "patches: [] # v2\n")

	resolve := func(statusCommit string) (map[string]string, string, error) {
		repo := servedOverlayRepo(url, ca, statusCommit)
		scheme := gitRepoTestScheme()
		r := &AIWorkloadReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build(), Scheme: scheme}
		return r.resolveKustomizeOverlay(context.Background(), gitOverlayComponent(), "")
	}

	files, got, err := resolve(indexed)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != indexed || files["overlay/kustomization.yaml"] != "patches: [] # v1\n" {
		t.Fatalf("commit=%s files=%v, want the indexed commit's content", got, files)
	}

	_, _, err = resolve(strings.Repeat("ab", 20))
	if !errors.Is(err, igit.ErrCommitNotFound) {
		t.Fatalf("err = %v, want ErrCommitNotFound for a commit not on the branch", err)
	}

	if _, _, err := resolve(""); err == nil || !strings.Contains(err.Error(), "not been indexed") {
		t.Fatalf("err = %v, want a not-indexed error before any clone", err)
	}
}

// A deferred overlay read uses the commits its fingerprint recorded, even if
// the ClusterRepo is re-indexed in between.
func TestResolveEffectiveOverlay_ReadsFingerprintedCommit(t *testing.T) {
	url, ca, commit := serveGitHTTPS(t)
	first := commit("prod/kustomization.yaml", "patches: [] # v1\n")
	repo := servedOverlayRepo(url, ca, first)
	scheme := gitRepoTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}
	w := newBlueprintWorkload(time.Now())
	c := gitOverlayComponent()

	_, _, pins, err := r.overlayFingerprint(context.Background(), w, c)
	if err != nil || pins.blueprint != first {
		t.Fatalf("pins=%+v err=%v, want the indexed commit", pins, err)
	}
	second := commit("prod/kustomization.yaml", "patches: [] # v2\n")
	_ = unstructured.SetNestedField(repo.Object, second, "status", "commit")
	if err := cl.Update(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	o, err := r.resolveEffectiveOverlay(context.Background(), w, c, pins)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := o.Digest["commit"]; got != first {
		t.Fatalf("overlay read at %v, want the fingerprinted commit %s", got, first)
	}
	if !strings.Contains(fmt.Sprint(o.Resources), "# v1") {
		t.Fatalf("resources = %v, want the fingerprinted content", o.Resources)
	}
}

// The overlay clone trusts the clientSecret's cacerts, as chart pulls do.
func TestResolveKustomizeOverlay_TrustsClientSecretCACerts(t *testing.T) {
	url, ca, commit := serveGitHTTPS(t)
	indexed := commit("prod/kustomization.yaml", "patches: []\n")
	resolve := func(cacerts []byte) error {
		repo := overlayGitRepo(true, indexed)
		_ = unstructured.SetNestedField(repo.Object, url, "spec", "gitRepo")
		_ = unstructured.SetNestedField(repo.Object, map[string]any{"name": "creds", "namespace": "cattle-system"}, "spec", "clientSecret")
		sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "cattle-system"}, Data: map[string][]byte{"cacerts": cacerts}}
		scheme := gitRepoTestScheme()
		r := &AIWorkloadReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo, sec).Build(), Scheme: scheme}
		_, _, err := r.resolveKustomizeOverlay(context.Background(), gitOverlayComponent(), "")
		return err
	}
	if err := resolve(nil); err == nil {
		t.Fatalf("want a TLS error without the private CA")
	}
	if err := resolve(ca); err != nil {
		t.Fatalf("with cacerts: %v", err)
	}
}
