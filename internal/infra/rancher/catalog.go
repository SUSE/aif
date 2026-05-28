package rancher

import "context"

// CatalogManager manages Rancher catalog resources (ClusterRepo, UIPlugin)
// on behalf of the InstallAIExtension controller.
type CatalogManager interface {
	CheckCRDs(ctx context.Context) error
	EnsureClusterRepo(ctx context.Context, opts ClusterRepoOpts) error
	EnsureClusterRepoGit(ctx context.Context, opts ClusterRepoGitOpts) error
	DeleteClusterRepo(ctx context.Context, extensionName string) error
	EnsureUIPlugin(ctx context.Context, opts UIPluginOpts) error
	DeleteUIPlugin(ctx context.Context, name string) error
	FetchIndexMetadata(ctx context.Context, indexURL, chartName, chartVersion string) (PluginMetadata, error)
}

// ClusterRepoOpts carries data for creating/updating a ClusterRepo
// pointing to a Helm chart service URL.
type ClusterRepoOpts struct {
	ExtensionName string // base name → ClusterRepo name derived as "{name}-charts"
	CRName        string // owning InstallAIExtension name for back-reference label
	ServiceURL    string // in-cluster Service URL
}

// ClusterRepoGitOpts carries data for creating/updating a ClusterRepo
// pointing to a git repository.
type ClusterRepoGitOpts struct {
	ExtensionName string
	CRName        string
	RepoURL       string
	Branch        string
}

// UIPluginOpts carries data for creating/updating a UIPlugin CR.
type UIPluginOpts struct {
	ExtensionName    string
	ExtensionVersion string
	CRName           string // owning InstallAIExtension name for back-reference label
	Endpoint         string
	Metadata         PluginMetadata
}

// PluginMetadata holds Rancher catalog annotations from a Helm repo index.yaml.
type PluginMetadata struct {
	DisplayName       string
	RancherVersion    string
	ExtensionsVersion string
}
