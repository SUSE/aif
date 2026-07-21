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

package credentials

import (
	"strings"
	"testing"
)

func TestNormalizeRegistryHost(t *testing.T) {
	cases := map[string]string{
		"ghcr.io":                          "ghcr.io",
		"https://ghcr.io":                  "ghcr.io",
		"http://ghcr.io/":                  "ghcr.io",
		"https://index.docker.io/v1/":      "index.docker.io",
		"registry.example.com:5000":        "registry.example.com:5000",
		"https://Registry.Example.Com/v1/": "registry.example.com",
	}
	for in, want := range cases {
		if got := normalizeRegistryHost(in); got != want {
			t.Errorf("normalizeRegistryHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDockerConfigCredentials_UsernamePassword(t *testing.T) {
	blob := []byte(`{"auths":{"registry.example.com":{"username":"u","password":"p"}}}`)
	user, pass, err := dockerConfigCredentials(blob, "registry.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != "u" || pass != "p" {
		t.Fatalf("got user=%q pass=%q, want u/p", user, pass)
	}
}

func TestDockerConfigCredentials_AuthBase64(t *testing.T) {
	// base64("u:p") = dTpw
	blob := []byte(`{"auths":{"https://registry.example.com":{"auth":"dTpw"}}}`)
	user, pass, err := dockerConfigCredentials(blob, "registry.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != "u" || pass != "p" {
		t.Fatalf("got user=%q pass=%q, want u/p", user, pass)
	}
}

func TestDockerConfigCredentials_NoHostEntry(t *testing.T) {
	blob := []byte(`{"auths":{"other.example.com":{"username":"u","password":"p"}}}`)
	_, _, err := dockerConfigCredentials(blob, "registry.example.com")
	if err == nil {
		t.Fatal("expected error for missing host entry")
	}
	if got := err.Error(); strings.Contains(got, "u") && strings.Contains(got, "p") {
		t.Fatalf("error must not leak credentials: %q", got)
	}
}

func TestDockerConfigCredentials_Malformed(t *testing.T) {
	_, _, err := dockerConfigCredentials([]byte(`not json`), "registry.example.com")
	if err == nil {
		t.Fatal("expected error for malformed json")
	}
}

func TestDockerConfigCredentials_CaseInsensitiveHost(t *testing.T) {
	blob := []byte(`{"auths":{"Registry.Example.Com":{"username":"u","password":"p"}}}`)
	user, pass, err := dockerConfigCredentials(blob, "registry.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != "u" || pass != "p" {
		t.Fatalf("got user=%q pass=%q, want u/p", user, pass)
	}
}

func TestDockerConfigCredentials_PasswordWithColon(t *testing.T) {
	// base64("user:pass:with:colons") = dXNlcjpwYXNzOndpdGg6Y29sb25z
	blob := []byte(`{"auths":{"registry.example.com":{"auth":"dXNlcjpwYXNzOndpdGg6Y29sb25z"}}}`) // gitleaks:allow — fake fixture (base64 "user:pass:with:colons")
	user, pass, err := dockerConfigCredentials(blob, "registry.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != "user" || pass != "pass:with:colons" {
		t.Fatalf("got user=%q pass=%q, want user/pass:with:colons", user, pass)
	}
}

func TestDockerConfigCredentials_InvalidBase64(t *testing.T) {
	blob := []byte(`{"auths":{"registry.example.com":{"auth":"not-valid-base64!!!"}}}`)
	if _, _, err := dockerConfigCredentials(blob, "registry.example.com"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDockerConfigCredentials_MalformedAuthPair(t *testing.T) {
	// base64("nocolon") = bm9jb2xvbg==
	blob := []byte(`{"auths":{"registry.example.com":{"auth":"bm9jb2xvbg=="}}}`)
	if _, _, err := dockerConfigCredentials(blob, "registry.example.com"); err == nil {
		t.Fatal("expected error for auth pair without colon")
	}
}

func TestDockerConfigCredentials_EmptyEntry(t *testing.T) {
	blob := []byte(`{"auths":{"registry.example.com":{}}}`)
	if _, _, err := dockerConfigCredentials(blob, "registry.example.com"); err == nil {
		t.Fatal("expected error for entry with no credentials")
	}
}
