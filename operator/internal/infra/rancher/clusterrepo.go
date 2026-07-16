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

package rancher

import (
	"context"
	"fmt"
	urlpkg "net/url"
	"strings"
	"time"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	logging "github.com/SUSE/aif-operator/internal/logging"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// annotationSyncedVersion records the extension version for which the operator
// last stamped spec.forceUpdate on the ClusterRepo. It lets EnsureClusterRepo
// force a Rancher index re-download exactly once per version change instead of
// on every reconcile.
const annotationSyncedVersion = "ai-factory.suse.com/synced-version"

func ClusterRepoName(extensionName string) string {
	return extensionName
}

func (m *Manager) EnsureClusterRepo(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	svcURL string,
) error {
	name := ClusterRepoName(ext.Spec.Extension.Name)
	log := logging.FromContext(ctx, "rancher.clusterrepo").
		WithValues(logging.KeyExtension, ext.Name, logging.KeyName, name)

	log.Info("Ensuring ClusterRepo")

	repo := &unstructured.Unstructured{}
	repo.SetAPIVersion("catalog.cattle.io/v1")
	repo.SetKind("ClusterRepo")
	repo.SetName(name)

	_, err := ctrl.CreateOrUpdate(ctx, m.client, repo, func() error {
		switch ext.Spec.Source.Kind {
		case v1alpha1.ExtensionSourceKindHelm:
			logging.Trace(log).Info("Setting ClusterRepo URL", "url", svcURL)
			unstructured.RemoveNestedField(repo.Object, "spec", "gitRepo")
			unstructured.RemoveNestedField(repo.Object, "spec", "gitBranch")
			if err := unstructured.SetNestedField(repo.Object, svcURL, "spec", "url"); err != nil {
				return err
			}

		case v1alpha1.ExtensionSourceKindGit:
			logging.Trace(log).Info("Setting ClusterRepo git source",
				"repo", ext.Spec.Source.Git.Repo,
				"branch", ext.Spec.Source.Git.Branch,
			)
			unstructured.RemoveNestedField(repo.Object, "spec", "url")
			if err := unstructured.SetNestedField(repo.Object, ext.Spec.Source.Git.Repo, "spec", "gitRepo"); err != nil {
				return err
			}
			if err := unstructured.SetNestedField(repo.Object, ext.Spec.Source.Git.Branch, "spec", "gitBranch"); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unsupported source kind: %s", ext.Spec.Source.Kind)
		}

		return forceIndexRefreshIfVersionChanged(log, repo, ext.Spec.Extension.Version)
	})
	if err != nil {
		return err
	}

	logging.Debug(log).Info("ClusterRepo ensured")
	return nil
}

// forceIndexRefreshIfVersionChanged stamps spec.forceUpdate with the current
// time whenever the served extension version differs from the one recorded in
// the synced-version annotation. Rancher re-downloads the repo index when
// forceUpdate is newer than its last download, which is what the UI "Refresh"
// button does — without this, an upgraded chart behind an unchanged service URL
// (or git branch) leaves Rancher serving a stale cached index.
//
// The timestamp is RFC3339 in UTC (trailing "Z"). A value missing the timezone
// makes cattle-cluster-agent fail to parse the field and crash-loop, so the
// format matters.
func forceIndexRefreshIfVersionChanged(log logr.Logger, repo *unstructured.Unstructured, version string) error {
	anns := repo.GetAnnotations()
	if anns == nil {
		anns = map[string]string{}
	}
	if anns[annotationSyncedVersion] == version {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	logging.Debug(log).Info("Forcing ClusterRepo index refresh",
		"version", version, "forceUpdate", now)
	if err := unstructured.SetNestedField(repo.Object, now, "spec", "forceUpdate"); err != nil {
		return err
	}

	anns[annotationSyncedVersion] = version
	repo.SetAnnotations(anns)
	return nil
}

func (m *Manager) DeleteClusterRepo(ctx context.Context, name string) error {
	log := logging.FromContext(ctx, "rancher.clusterrepo").
		WithValues(logging.KeyName, name)

	log.Info("Deleting ClusterRepo")

	repo := &unstructured.Unstructured{}
	repo.SetAPIVersion("catalog.cattle.io/v1")
	repo.SetKind("ClusterRepo")
	repo.SetName(name)

	if err := m.client.Delete(ctx, repo); client.IgnoreNotFound(err) != nil {
		log.Error(err, "Failed to delete ClusterRepo")
		return err
	}

	log.Info("ClusterRepo deleted")
	return nil
}

func GitRawBaseURL(repo string, branch string) (string, error) {
	if branch == "" {
		return "", fmt.Errorf("git branch must not be empty")
	}
	u, err := urlpkg.Parse(repo)
	if err != nil {
		return "", fmt.Errorf("invalid git repo URL: %w", err)
	}
	if u.Host != "github.com" {
		return "", fmt.Errorf("unsupported git host %q: only github.com is supported", u.Host)
	}
	repoPath := strings.TrimSuffix(u.Path, ".git")
	return fmt.Sprintf("https://raw.githubusercontent.com%s/refs/heads/%s", repoPath, branch), nil
}
