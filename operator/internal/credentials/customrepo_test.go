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
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
)

func TestCustomRepoResourceName(t *testing.T) {
	if got := credentials.CustomRepoResourceName("acme"); got != "custom-acme" {
		t.Fatalf("got %q, want custom-acme", got)
	}
	if got := credentials.CustomRepoAuthSecretName("acme"); got != "custom-acme-auth" {
		t.Fatalf("got %q, want custom-acme-auth", got)
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
		{"unknown type", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a", Type: "svn", URL: "https://x"}}, true},
		{"name too long", []aiplatformv1alpha1.CustomRepoSpec{{Name: "a123456789012345678901234567890123456789012345678901234567", Type: "helm", URL: "https://x"}}, true},
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
