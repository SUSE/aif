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

package credentials_test

import (
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/catalog"
	"github.com/SUSE/aif-operator/internal/credentials"
)

const (
	testRepoName   = "acme"
	repoTypeHelm   = "helm"
	placeholderURL = "https://x"
)

func TestCustomRepoResourceName(t *testing.T) {
	if got := credentials.CustomRepoResourceName(testRepoName); got != testRepoName {
		t.Fatalf("got %q, want acme", got)
	}
	if got := credentials.CustomRepoAuthSecretName(testRepoName); got != testRepoName+"-custom-repo-auth" {
		t.Fatalf("got %q, want acme-custom-repo-auth", got)
	}
}

// A custom repo's auth secret must never take the name of a built-in registry
// auth secret, whatever name the admin picks.
func TestCustomRepoAuthSecretName_NeverMatchesBuiltIn(t *testing.T) {
	for _, builtIn := range []string{
		credentials.AuthSecretApplicationCollection,
		credentials.AuthSecretSUSERegistry,
		credentials.AuthSecretNvidia,
	} {
		name := strings.TrimSuffix(builtIn, "-auth")
		if got := credentials.CustomRepoAuthSecretName(name); got == builtIn {
			t.Fatalf("custom repo %q maps onto built-in auth secret %q", name, builtIn)
		}
	}
}

func TestIsReservedRepoName(t *testing.T) {
	reserved := []string{
		credentials.ClusterRepoApplicationCollection,
		credentials.ClusterRepoSUSERegistry,
		credentials.ClusterRepoNvidia,
		credentials.ClusterRepoNvidiaBlueprint,
		"rancher-charts",
		"rancher-partner-charts",
		"rancher-rke2-charts",
	}
	for _, name := range reserved {
		if !credentials.IsReservedRepoName(name) {
			t.Errorf("IsReservedRepoName(%q) = false, want true", name)
		}
	}
	if credentials.IsReservedRepoName(testRepoName) {
		t.Error("IsReservedRepoName(" + testRepoName + ") = true, want false")
	}
}

// Team repo names are derived from the bundled catalog, so every one of them
// must be reserved: a custom repo with the same name would take over the
// operator-managed team ClusterRepo.
func TestIsReservedRepoName_CatalogTeamRepos(t *testing.T) {
	teams := catalog.ClassifyNGCTeamRepos()
	urls := append(append([]string{}, teams.Public...), teams.Gated...)
	if len(urls) == 0 {
		t.Skip("bundled catalog references no team repos")
	}
	for _, u := range urls {
		name, err := catalog.NGCClusterRepoName(u)
		if err != nil {
			continue
		}
		if !credentials.IsReservedRepoName(name) {
			t.Errorf("team repo name %q (from %s) is not reserved", name, u)
		}
	}
}

func TestValidateCustomRepos(t *testing.T) {
	tests := []struct {
		name    string
		repos   []aiplatformv1alpha1.CustomRepoSpec
		wantErr bool
	}{
		{"empty", nil, false},
		{"valid helm", []aiplatformv1alpha1.CustomRepoSpec{{Name: "acme", Type: "helm", URL: "https://charts.example.com"}}, false},
		{"valid oci", []aiplatformv1alpha1.CustomRepoSpec{{Name: "acme", Type: "oci", URL: "oci://reg.example.com/charts"}}, false},
		{"valid git", []aiplatformv1alpha1.CustomRepoSpec{{Name: "acme", Type: "git", GitRepo: "https://git.example.com/repo.git", GitBranch: "main"}}, false},
		{"missing name", []aiplatformv1alpha1.CustomRepoSpec{{Type: "helm", URL: "https://x"}}, true},
		{"bad dns name", []aiplatformv1alpha1.CustomRepoSpec{{Name: "Acme_Repo", Type: "helm", URL: "https://x"}}, true},
		{"reserved name", []aiplatformv1alpha1.CustomRepoSpec{{Name: "nvidia", Type: "helm", URL: "https://x"}}, true},
		{"duplicate", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a", Type: "helm", URL: "https://x"}, {Name: "a", Type: "helm", URL: "https://y"}}, true},
		{"helm wrong scheme", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a", Type: "helm", URL: "oci://x"}}, true},
		{"oci wrong scheme", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a", Type: "oci", URL: "https://x"}}, true},
		{"git missing branch", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a", Type: "git", GitRepo: "https://x"}}, true},
		{"git with url", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a", Type: "git", GitRepo: "https://x", GitBranch: "main", URL: "https://y"}}, true},
		{"git http anonymous", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a", Type: "git", GitRepo: "http://x/repo.git", GitBranch: "main"}}, false},
		{"git http with basic auth", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a", Type: "git", GitRepo: "http://x/repo.git", GitBranch: "main", UserSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "s", Key: "u"}, TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "s", Key: "t"}}}, true},
		{"unknown type", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a", Type: "svn", URL: "https://x"}}, true},
		{"reserved rancher name", []aiplatformv1alpha1.CustomRepoSpec{{Name: "rancher-charts", Type: repoTypeHelm, URL: placeholderURL}}, true},
		{"name at the 63-char limit", []aiplatformv1alpha1.CustomRepoSpec{{Name: strings.Repeat("a", 63), Type: repoTypeHelm, URL: placeholderURL}}, false},
		{"name too long", []aiplatformv1alpha1.CustomRepoSpec{{Name: strings.Repeat("a", 64), Type: repoTypeHelm, URL: placeholderURL}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := credentials.ValidateCustomRepos(tt.repos)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateCustomRepos() err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
