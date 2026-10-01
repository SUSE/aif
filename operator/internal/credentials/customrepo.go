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
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/catalog"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// customRepoAuthSecretSuffix keeps custom-repo auth secrets apart from the
// built-in registry auth secrets ("<repo>-auth") that share their namespaces.
const customRepoAuthSecretSuffix = "-custom-repo-auth"

// customRepoNameMax is the DNS-1123 label limit: the name is used verbatim as
// the ClusterRepo name.
const customRepoNameMax = 63

var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// rancherDefaultRepoNames are the ClusterRepos Rancher provisions itself.
var rancherDefaultRepoNames = map[string]bool{
	"rancher-charts":         true,
	"rancher-partner-charts": true,
	"rancher-rke2-charts":    true,
}

// teamRepoNames is the set of NVIDIA team ClusterRepo names the operator derives
// from the bundled catalog.
var teamRepoNames = sync.OnceValue(func() map[string]bool {
	teams := catalog.ClassifyNGCTeamRepos()
	names := make(map[string]bool, len(teams.Public)+len(teams.Gated))
	for _, u := range append(append([]string{}, teams.Public...), teams.Gated...) {
		if name, err := catalog.NGCClusterRepoName(u); err == nil {
			names[name] = true
		}
	}
	return names
})

// CustomRepoResourceName is the ClusterRepo name for a custom repo: the
// admin-supplied name, used as-is.
func CustomRepoResourceName(name string) string { return name }

// CustomRepoAuthSecretName is the materialized basic-auth/ssh-auth secret name.
func CustomRepoAuthSecretName(name string) string { return name + customRepoAuthSecretSuffix }

// IsReservedRepoName reports whether name belongs to a ClusterRepo that the
// operator or Rancher manages: the canonical registry repos, the catalog-derived
// NVIDIA team repos, and Rancher's default repos. A custom repo may not use such
// a name, and the custom-repo prune never deletes one.
func IsReservedRepoName(name string) bool {
	switch name {
	case ClusterRepoApplicationCollection, ClusterRepoSUSERegistry, ClusterRepoNvidia, ClusterRepoNvidiaBlueprint:
		return true
	}
	return rancherDefaultRepoNames[name] || teamRepoNames()[name]
}

// ClusterRepoNameTaken reports whether a ClusterRepo named name already exists
// without the custom-repo label. Such a repo belongs to Rancher, the operator's
// built-in repos or another tool, so a custom repo must not take it over. A
// cluster that does not serve the ClusterRepo API has nothing to collide with.
func ClusterRepoNameTaken(ctx context.Context, c client.Reader, name string) (bool, error) {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	err := c.Get(ctx, client.ObjectKey{Name: name}, repo)
	switch {
	case err == nil:
		return repo.GetLabels()[CustomRepoLabel] != LabelValueTrue, nil
	case errors.IsNotFound(err), meta.IsNoMatchError(err), runtime.IsNotRegisteredError(err):
		return false, nil
	default:
		return false, fmt.Errorf("look up ClusterRepo %q: %w", name, err)
	}
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
		if IsReservedRepoName(r.Name) {
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
			if err := rejectURLUserinfo(i, r.URL); err != nil {
				return err
			}
		case "oci":
			if !strings.HasPrefix(r.URL, "oci://") {
				return fmt.Errorf("customRepos[%d]: oci url must start with oci://", i)
			}
			if r.GitRepo != "" || r.GitBranch != "" {
				return fmt.Errorf("customRepos[%d]: oci repo must not set git fields", i)
			}
			if err := rejectURLUserinfo(i, r.URL); err != nil {
				return err
			}
		case "git":
			if r.GitRepo == "" || r.GitBranch == "" {
				return fmt.Errorf("customRepos[%d]: git repo requires gitRepo and gitBranch", i)
			}
			if r.URL != "" {
				return fmt.Errorf("customRepos[%d]: git repo must not set url", i)
			}
			// Only reject userinfo for http(s) git URLs; scp-like syntax
			// (git@host:path) is not a parseable URL and carries no password.
			if strings.HasPrefix(r.GitRepo, "http://") || strings.HasPrefix(r.GitRepo, "https://") {
				if err := rejectURLUserinfo(i, r.GitRepo); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("customRepos[%d]: unknown type %q (want helm|oci|git)", i, r.Type)
		}

		if err := validateCustomRepoAuth(i, r); err != nil {
			return err
		}
	}
	return nil
}

// validateCustomRepoAuth enforces the credential-shape invariants: basic-auth
// user and token are set together or not at all; an SSH key is git-only and
// mutually exclusive with basic auth.
func validateCustomRepoAuth(i int, r aiplatformv1alpha1.CustomRepoSpec) error {
	if (r.UserSecretRef == nil) != (r.TokenSecretRef == nil) {
		return fmt.Errorf("customRepos[%d]: userSecretRef and tokenSecretRef must be set together", i)
	}
	// Reject basic auth over cleartext http (helm url or git gitRepo): the
	// password would be on the wire in the clear. Anonymous http is still allowed.
	if r.UserSecretRef != nil && (strings.HasPrefix(r.URL, "http://") || strings.HasPrefix(r.GitRepo, "http://")) {
		return fmt.Errorf("customRepos[%d]: basic-auth credentials require https (refusing to send them over cleartext http)", i)
	}
	if r.SSHKeySecretRef != nil {
		if r.Type != "git" {
			return fmt.Errorf("customRepos[%d]: sshKeySecretRef is only valid for git repositories", i)
		}
		if r.UserSecretRef != nil || r.TokenSecretRef != nil {
			return fmt.Errorf("customRepos[%d]: sshKeySecretRef is mutually exclusive with basic auth", i)
		}
	}
	return nil
}

// rejectURLUserinfo fails when a URL embeds credentials (https://user:pass@host),
// which would be written verbatim into a cluster-scoped ClusterRepo in cleartext.
func rejectURLUserinfo(i int, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("customRepos[%d]: invalid url %q: %w", i, rawURL, err)
	}
	if u.User != nil {
		return fmt.Errorf("customRepos[%d]: url must not embed credentials (user:password@host); use a secret ref", i)
	}
	return nil
}
