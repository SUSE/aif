package rancher

import (
	"context"
	"fmt"
	urlpkg "net/url"
	"strings"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	logging "github.com/SUSE/aif-operator/internal/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
			return unstructured.SetNestedField(repo.Object, svcURL, "spec", "url")

		case v1alpha1.ExtensionSourceKindGit:
			logging.Trace(log).Info("Setting ClusterRepo git source",
				"repo", ext.Spec.Source.Git.Repo,
				"branch", ext.Spec.Source.Git.Branch,
			)
			unstructured.RemoveNestedField(repo.Object, "spec", "url")
			if err := unstructured.SetNestedField(repo.Object, ext.Spec.Source.Git.Repo, "spec", "gitRepo"); err != nil {
				return err
			}
			return unstructured.SetNestedField(repo.Object, ext.Spec.Source.Git.Branch, "spec", "gitBranch")

		default:
			return fmt.Errorf("unsupported source kind: %s", ext.Spec.Source.Kind)
		}
	})
	if err != nil {
		return err
	}

	logging.Debug(log).Info("ClusterRepo ensured")
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
