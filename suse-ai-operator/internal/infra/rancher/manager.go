package rancher

import (
	"context"

	v1alpha1 "github.com/SUSE/suse-ai-operator/api/v1alpha1"
	"github.com/SUSE/suse-ai-operator/internal/infra/helm"
	logging "github.com/SUSE/suse-ai-operator/internal/logging"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var requiredCRDs = []string{
	"uiplugins.catalog.cattle.io",
	"clusterrepos.catalog.cattle.io",
}

type Manager struct {
	client     client.Client
	scheme     *runtime.Scheme
	indexCache *helm.IndexCache
}

func NewManager(c client.Client, s *runtime.Scheme) *Manager {
	return &Manager{client: c, scheme: s, indexCache: helm.NewIndexCache()}
}

func (m *Manager) EnsureHelmResources(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	svcURL string,
	namespace string,
) error {
	log := logging.FromContext(ctx, "rancher").
		WithValues(logging.KeyExtension, ext.Name)

	log.Info("Ensuring Rancher resources (Helm)")

	if err := m.CheckCRDs(ctx, requiredCRDs); err != nil {
		return err
	}

	if err := m.EnsureClusterRepo(ctx, ext, svcURL); err != nil {
		return err
	}

	if err := m.EnsureUIPlugin(ctx, ext, svcURL, namespace); err != nil {
		return err
	}

	log.Info("Rancher resources ensured")
	return nil
}

func (m *Manager) EnsureGitResources(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	namespace string,
) error {
	log := logging.FromContext(ctx, "rancher").
		WithValues(logging.KeyExtension, ext.Name)

	log.Info("Ensuring Rancher resources (Git)")

	if err := m.CheckCRDs(ctx, requiredCRDs); err != nil {
		return err
	}

	if err := m.EnsureClusterRepo(ctx, ext, ""); err != nil {
		return err
	}

	log.Info("Rancher resources ensured (Git)")
	return nil
}

func (m *Manager) ResolveLatestVersion(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	svcURL string,
) (string, error) {
	log := logging.FromContext(ctx, "rancher.resolve").
		WithValues(logging.KeyExtension, ext.Spec.Extension.Name)

	var indexURLs []string
	switch ext.Spec.Source.Kind {
	case v1alpha1.ExtensionSourceKindHelm:
		indexURLs = []string{svcURL + "/index.yaml"}
	case v1alpha1.ExtensionSourceKindGit:
		base := GitRawBaseURL(ext.Spec.Source.Git.Repo, ext.Spec.Source.Git.Branch)
		indexURLs = []string{
			base + "/index.yaml",
			base + "/assets/index.yaml",
		}
	}

	index, err := getOrFetchIndexMulti(ctx, m.indexCache, indexURLs)
	if err != nil {
		return "", err
	}

	version, err := helm.FindLatestVersion(index, ext.Spec.Extension.Name)
	if err != nil {
		return "", err
	}

	log.Info("Resolved latest version", "version", version)
	return version, nil
}
