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

package git

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const tokenAuth = "token"

type internalMapReader map[string]map[string][]byte

func (r internalMapReader) ReadSecret(_ context.Context, namespace, name string) (map[string][]byte, error) {
	value, found := r[namespace+"/"+name]
	if !found {
		return nil, fmt.Errorf("missing secret")
	}
	return value, nil
}

func TestNewFromSettingsLegacyTokenAuthDefaultsUsername(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		GitRepoSource: aiplatformv1alpha1.GitRepoSource{
			RepoURL:       "https://git.example.test/org/repo.git",
			AuthType:      tokenAuth,
			CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: tokenAuth},
		},
	}

	client, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth": {tokenAuth: []byte("secret-token")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.auth == nil || client.auth.Username != tokenAuth || client.auth.Password != "secret-token" {
		t.Fatalf("unexpected token auth: %#v", client.auth)
	}
}

func TestNewFromSettingsHTTPSCredentialsUseExplicitUsername(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		GitRepoSource: aiplatformv1alpha1.GitRepoSource{
			RepoURL:       "https://git.example.test/org/repo.git",
			Username:      "alice",
			CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: "password"},
		},
	}

	client, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth": {
			"username": []byte("secret-user"),
			"password": []byte("secret-password"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.auth == nil || client.auth.Username != "alice" || client.auth.Password != "secret-password" {
		t.Fatalf("unexpected basic auth: %#v", client.auth)
	}
}

func TestNewFromSettingsHTTPSCredentialsReadUsernameFromSecret(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		GitRepoSource: aiplatformv1alpha1.GitRepoSource{
			RepoURL:       "https://git.example.test/org/repo.git",
			CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: "password"},
		},
	}

	client, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth": {
			"username": []byte("alice"),
			"password": []byte("secret-password"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.auth == nil || client.auth.Username != "alice" || client.auth.Password != "secret-password" {
		t.Fatalf("unexpected HTTPS auth: %#v", client.auth)
	}
}

func TestNewFromSettingsHTTPSCredentialsDefaultUsername(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		GitRepoSource: aiplatformv1alpha1.GitRepoSource{
			RepoURL:       "https://git.example.test/org/repo.git",
			CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: "token"},
		},
	}

	client, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth": {"token": []byte("secret-token")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.auth == nil || client.auth.Username != tokenAuth || client.auth.Password != "secret-token" {
		t.Fatalf("unexpected default HTTPS auth: %#v", client.auth)
	}
}

func TestNewFromSettingsRejectsUnsupportedSSH(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		GitRepoSource: aiplatformv1alpha1.GitRepoSource{
			RepoURL:       "ssh://git@git.example.test/org/repo.git",
			AuthType:      "ssh",
			CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: "ssh-privatekey"},
		},
	}

	_, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth": {"ssh-privatekey": []byte("private-key")},
	})
	if err == nil {
		t.Fatal("expected unsupported SSH authentication to fail")
	}
}

func TestNewFromSettingsRejectsUsernameWithoutCredentials(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		GitRepoSource: aiplatformv1alpha1.GitRepoSource{
			RepoURL:  "https://git.example.test/org/repo.git",
			Username: "alice",
		},
	}

	_, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{})
	if err == nil {
		t.Fatal("expected username without credentials to fail")
	}
}

// A git server that accepts the connection but never answers must not hold
// the caller past the operation timeout.
func TestOperationsTimeOutOnStalledServer(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	c := NewForRepo(srv.URL+"/repo.git", "main", nil, nil, false)
	c.timeout = 200 * time.Millisecond
	ops := map[string]func() error{
		"ReadDir": func() error {
			_, err := c.ReadDir(context.Background(), "overlay", strings.Repeat("ab", 20))
			return err
		},
		"ReadFile": func() error {
			_, _, err := c.ReadFile(context.Background(), "a.yaml", 1<<20)
			return err
		},
		"WriteFile":  func() error { _, err := c.WriteFile(context.Background(), "a.yaml", "x", "m"); return err },
		"DeleteFile": func() error { _, err := c.DeleteFile(context.Background(), "a.yaml", "m"); return err },
	}
	for name, op := range ops {
		start := time.Now()
		err := op()
		if err == nil {
			t.Fatalf("%s: want an error from a stalled server", name)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("%s returned after %v, want it bounded by the timeout", name, elapsed)
		}
	}
}

// newCommitRemote is an empty bare repo and a function committing one file on
// main through c's own write path, returning the commit hash.
func newCommitRemote(t *testing.T) (*Client, func(content string) string) {
	t.Helper()
	dir := t.TempDir()
	if _, err := gogit.PlainInit(dir, true); err != nil {
		t.Fatal(err)
	}
	c := NewForRepo("file://"+dir, "main", nil, nil, false)
	return c, func(content string) string {
		h, err := c.WriteFile(context.Background(), "overlay/kustomization.yaml", content, "update")
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
}

// Writes clone and push repos holding embedded charts, so they get the larger
// limit; reads keep the smaller one.
func TestOperationLimits(t *testing.T) {
	c, commit := newCommitRemote(t)
	got := map[string]time.Duration{}
	c.onDeadline = func(op string, d time.Duration) { got[op] = d }
	tip := commit("a\n")
	if _, err := c.DeleteFile(context.Background(), "missing.yaml", "m"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadFile(context.Background(), "overlay/kustomization.yaml", 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadDir(context.Background(), "overlay", tip); err != nil {
		t.Fatal(err)
	}
	if got["write"] < 5*time.Minute || got["delete"] < 5*time.Minute {
		t.Fatalf("write limits = %v, want at least 5m (a 60s limit cut multi-MiB pushes)", got)
	}
	if got["read"] != readTimeout || readTimeout < time.Minute {
		t.Fatalf("read limit = %v, want %v", got["read"], readTimeout)
	}
}

func TestReadDir_ShallowWhenTipThenDeepens(t *testing.T) {
	saved := readDirDepths
	readDirDepths = []int{1, 2, 0}
	defer func() { readDirDepths = saved }()

	c, commit := newCommitRemote(t)
	first := commit("v1\n")
	second := commit("v2\n")
	third := commit("v3\n")
	tip := commit("v4\n")
	var depths []int
	c.onClone = func(d int) { depths = append(depths, d) }
	read := func(hash string) (string, error) {
		depths = nil
		files, err := c.ReadDir(context.Background(), "overlay", hash)
		return files["kustomization.yaml"], err
	}

	if got, err := read(tip); err != nil || got != "v4\n" || fmt.Sprint(depths) != "[1]" {
		t.Fatalf("tip: %q %v depths=%v, want v4 from one shallow clone", got, err, depths)
	}
	if got, err := read(third); err != nil || got != "v3\n" || fmt.Sprint(depths) != "[1 2]" {
		t.Fatalf("parent of tip: %q %v depths=%v, want v3 from the first deepening", got, err, depths)
	}
	// second is two commits below the tip: outside depth 2, so the full clone finds it.
	if got, err := read(second); err != nil || got != "v2\n" || fmt.Sprint(depths) != "[1 2 0]" {
		t.Fatalf("older commit: %q %v depths=%v, want v2 after deepening", got, err, depths)
	}
	if got, err := read(first); err != nil || got != "v1\n" {
		t.Fatalf("oldest commit: %q %v, want v1", got, err)
	}
	if _, err := read(strings.Repeat("ab", 20)); !errors.Is(err, ErrCommitNotFound) || fmt.Sprint(depths) != "[1 2 0]" {
		t.Fatalf("unknown commit: err=%v depths=%v, want ErrCommitNotFound after every depth", err, depths)
	}
}
