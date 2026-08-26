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
	"fmt"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const tokenAuth = "token"

type internalMapReader map[string]string

func (r internalMapReader) ReadSecretKey(_ context.Context, namespace, name, key string) (string, error) {
	value, found := r[namespace+"/"+name+"/"+key]
	if !found {
		return "", fmt.Errorf("missing secret key")
	}
	return value, nil
}

func TestNewFromSettingsLegacyTokenAuthDefaultsUsername(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		RepoURL:       "https://git.example.test/org/repo.git",
		AuthType:      tokenAuth,
		CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: tokenAuth},
	}

	client, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth/token": "secret-token",
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
		RepoURL:       "https://git.example.test/org/repo.git",
		Username:      "alice",
		CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: "password"},
	}

	client, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth/password": "secret-password",
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
		RepoURL:       "https://git.example.test/org/repo.git",
		CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: "password"},
	}

	client, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth/username": "alice",
		"aif-operator/git-auth/password": "secret-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.auth == nil || client.auth.Username != "alice" || client.auth.Password != "secret-password" {
		t.Fatalf("unexpected HTTPS auth: %#v", client.auth)
	}
}

func TestNewFromSettingsRejectsHTTPSCredentialsWithoutUsername(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		RepoURL:       "https://git.example.test/org/repo.git",
		CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: "token"},
	}

	_, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth/token": "secret-token",
	})
	if err == nil {
		t.Fatal("expected missing HTTPS username to fail")
	}
}

func TestNewFromSettingsRejectsUnsupportedSSH(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		RepoURL:       "ssh://git@git.example.test/org/repo.git",
		AuthType:      "ssh",
		CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-auth", Key: "ssh-privatekey"},
	}

	_, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{
		"aif-operator/git-auth/ssh-privatekey": "private-key",
	})
	if err == nil {
		t.Fatal("expected unsupported SSH authentication to fail")
	}
}

func TestNewFromSettingsRejectsUsernameWithoutCredentials(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet = aiplatformv1alpha1.FleetSettings{
		RepoURL:  "https://git.example.test/org/repo.git",
		Username: "alice",
	}

	_, err := NewFromSettings(context.Background(), s, "aif-operator", internalMapReader{})
	if err == nil {
		t.Fatal("expected username without credentials to fail")
	}
}
