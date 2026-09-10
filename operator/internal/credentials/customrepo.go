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
	"fmt"
	"regexp"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const customRepoNamePrefix = "custom-"

// customRepoNameMax caps the user-supplied name so the derived ClusterRepo name
// "custom-<name>" stays within the DNS-1123 label limit (63).
const customRepoNameMax = 63 - len(customRepoNamePrefix)

var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// CustomRepoResourceName is the ClusterRepo name for a custom repo.
func CustomRepoResourceName(name string) string { return customRepoNamePrefix + name }

// CustomRepoAuthSecretName is the materialized basic-auth/ssh-auth secret name.
func CustomRepoAuthSecretName(name string) string { return customRepoNamePrefix + name + "-auth" }

// IsReservedRepoName reports whether name collides with a canonical repo name.
func IsReservedRepoName(name string) bool {
	switch name {
	case ClusterRepoApplicationCollection, ClusterRepoSUSERegistry, ClusterRepoNvidia, ClusterRepoNvidiaBlueprint:
		return true
	}
	return false
}

// ValidateCustomRepos checks the custom-repo list for name and type/url/git
// consistency. Defense in depth: the UI validates the same rules client-side.
func ValidateCustomRepos(repos []aiplatformv1alpha1.CustomRepoSpec) error {
	seen := map[string]bool{}
	for i, r := range repos {
		if r.Name == "" {
			return fmt.Errorf("customRepos[%d]: name is required", i)
		}
		if len(r.Name) > customRepoNameMax {
			return fmt.Errorf("customRepos[%d]: name %q exceeds %d characters", i, r.Name, customRepoNameMax)
		}
		if !dns1123Label.MatchString(r.Name) {
			return fmt.Errorf("customRepos[%d]: name %q must be a DNS-1123 label (lowercase alphanumeric and '-')", i, r.Name)
		}
		if IsReservedRepoName(r.Name) || IsReservedRepoName(CustomRepoResourceName(r.Name)) {
			return fmt.Errorf("customRepos[%d]: name %q is reserved", i, r.Name)
		}
		if seen[r.Name] {
			return fmt.Errorf("customRepos[%d]: duplicate name %q", i, r.Name)
		}
		seen[r.Name] = true

		switch r.Type {
		case "helm":
			if !strings.HasPrefix(r.URL, "http://") && !strings.HasPrefix(r.URL, "https://") {
				return fmt.Errorf("customRepos[%d]: helm url must start with http:// or https://", i)
			}
			if r.GitRepo != "" || r.GitBranch != "" {
				return fmt.Errorf("customRepos[%d]: helm repo must not set git fields", i)
			}
		case "oci":
			if !strings.HasPrefix(r.URL, "oci://") {
				return fmt.Errorf("customRepos[%d]: oci url must start with oci://", i)
			}
			if r.GitRepo != "" || r.GitBranch != "" {
				return fmt.Errorf("customRepos[%d]: oci repo must not set git fields", i)
			}
		case "git":
			if r.GitRepo == "" || r.GitBranch == "" {
				return fmt.Errorf("customRepos[%d]: git repo requires gitRepo and gitBranch", i)
			}
			if r.URL != "" {
				return fmt.Errorf("customRepos[%d]: git repo must not set url", i)
			}
		default:
			return fmt.Errorf("customRepos[%d]: unknown type %q (want helm|oci|git)", i, r.Type)
		}
	}
	return nil
}
