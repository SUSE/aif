package rancher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/SUSE/aif/pkg/helm"
)

const UIPluginNamespace = "cattle-ui-plugin-system"

// Catalog implements CatalogManager using the K8s client and discovery APIs.
type Catalog struct {
	client     client.Client
	discovery  discovery.DiscoveryInterface
	httpClient *http.Client
}

var _ CatalogManager = (*Catalog)(nil)

func New(c client.Client, disc discovery.DiscoveryInterface, httpClient *http.Client) *Catalog {
	return &Catalog{client: c, discovery: disc, httpClient: httpClient}
}

func (m *Catalog) CheckCRDs(ctx context.Context) error {
	_, err := m.discovery.ServerResourcesForGroupVersion("catalog.cattle.io/v1")
	if err != nil {
		return fmt.Errorf("catalog.cattle.io/v1 CRDs not found: %w", err)
	}
	return nil
}

func (m *Catalog) EnsureClusterRepo(ctx context.Context, opts ClusterRepoOpts) error {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK())
	repo.SetName(clusterRepoName(opts.ExtensionName))

	_, err := controllerutil.CreateOrUpdate(ctx, m.client, repo, func() error {
		repo.SetLabels(map[string]string{
			"catalog.cattle.io/ui-extensions-catalog-image": opts.ExtensionName,
			"ai.suse.com/installaiextension":                opts.CRName,
		})
		if err := unstructured.SetNestedField(repo.Object, opts.ServiceURL, "spec", "url"); err != nil {
			return err
		}
		unstructured.RemoveNestedField(repo.Object, "spec", "gitRepo")
		unstructured.RemoveNestedField(repo.Object, "spec", "gitBranch")
		return nil
	})
	return err
}

func (m *Catalog) EnsureClusterRepoGit(ctx context.Context, opts ClusterRepoGitOpts) error {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK())
	repo.SetName(clusterRepoName(opts.ExtensionName))

	_, err := controllerutil.CreateOrUpdate(ctx, m.client, repo, func() error {
		repo.SetLabels(map[string]string{
			"catalog.cattle.io/ui-extensions-catalog-image": opts.ExtensionName,
			"ai.suse.com/installaiextension":                opts.CRName,
		})
		if err := unstructured.SetNestedField(repo.Object, opts.RepoURL, "spec", "gitRepo"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(repo.Object, opts.Branch, "spec", "gitBranch"); err != nil {
			return err
		}
		unstructured.RemoveNestedField(repo.Object, "spec", "url")
		return nil
	})
	return err
}

func (m *Catalog) DeleteClusterRepo(ctx context.Context, extensionName string) error {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK())
	repo.SetName(clusterRepoName(extensionName))
	if err := m.client.Delete(ctx, repo); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete ClusterRepo %s: %w", clusterRepoName(extensionName), err)
	}
	return nil
}

func (m *Catalog) EnsureUIPlugin(ctx context.Context, opts UIPluginOpts) error {
	plugin := &unstructured.Unstructured{}
	plugin.SetGroupVersionKind(uiPluginGVK())
	plugin.SetName(opts.ExtensionName)
	plugin.SetNamespace(UIPluginNamespace)

	_, err := controllerutil.CreateOrUpdate(ctx, m.client, plugin, func() error {
		plugin.SetLabels(map[string]string{
			"ai.suse.com/installaiextension": opts.CRName,
		})

		pluginSpec := map[string]interface{}{
			"name":     opts.ExtensionName,
			"version":  opts.ExtensionVersion,
			"endpoint": opts.Endpoint,
			"noAuth":   false,
			"noCache":  false,
		}

		metadata := map[string]interface{}{}
		if opts.Metadata.DisplayName != "" {
			metadata["catalog.cattle.io/display-name"] = opts.Metadata.DisplayName
		}
		if opts.Metadata.RancherVersion != "" {
			metadata["catalog.cattle.io/rancher-version"] = opts.Metadata.RancherVersion
		}
		if opts.Metadata.ExtensionsVersion != "" {
			metadata["catalog.cattle.io/ui-extensions-version"] = opts.Metadata.ExtensionsVersion
		}
		if len(metadata) > 0 {
			pluginSpec["metadata"] = metadata
		}

		return unstructured.SetNestedMap(plugin.Object, pluginSpec, "spec", "plugin")
	})
	return err
}

func (m *Catalog) DeleteUIPlugin(ctx context.Context, name string) error {
	plugin := &unstructured.Unstructured{}
	plugin.SetGroupVersionKind(uiPluginGVK())
	plugin.SetName(name)
	plugin.SetNamespace(UIPluginNamespace)
	if err := m.client.Delete(ctx, plugin); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete UIPlugin %s: %w", name, err)
	}
	return nil
}

func (m *Catalog) FetchIndexMetadata(ctx context.Context, indexURL, chartName, chartVersion string) (PluginMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return PluginMetadata{}, fmt.Errorf("build request: %w", err)
	}

	httpClient := m.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return PluginMetadata{}, fmt.Errorf("fetch index.yaml: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return PluginMetadata{}, fmt.Errorf("index.yaml returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return PluginMetadata{}, fmt.Errorf("read index.yaml: %w", err)
	}

	ann, err := helm.FindChartAnnotations(body, chartName, chartVersion)
	if err != nil {
		return PluginMetadata{}, err
	}

	return PluginMetadata{
		DisplayName:       ann.DisplayName,
		RancherVersion:    ann.RancherVersion,
		ExtensionsVersion: ann.ExtensionsVersion,
	}, nil
}

// DeriveReleaseName extracts a Helm release name from a chart URL,
// stripping any OCI tag suffix (e.g. ":1.0.0").
// Assumes chartURL is a well-formed URL with a non-empty path component
// (enforced by CRD validation: +kubebuilder:validation:MinLength=1).
func DeriveReleaseName(chartURL string) string {
	base := path.Base(chartURL)
	if i := strings.LastIndex(base, ":"); i > 0 {
		return base[:i]
	}
	return base
}

// GitRepoToRawURL converts a GitHub repository URL to a raw.githubusercontent.com URL.
func GitRepoToRawURL(repoURL, branch string) (string, error) {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("parse git URL: %w", err)
	}

	if parsed.Host != "github.com" {
		return "", fmt.Errorf("unsupported git host %q: only github.com is supported", parsed.Host)
	}

	repoPath := strings.TrimSuffix(parsed.Path, ".git")
	return fmt.Sprintf("https://raw.githubusercontent.com%s/refs/heads/%s", repoPath, branch), nil
}

// ClusterRepoName returns the ClusterRepo name for a given extension name.
func ClusterRepoName(extensionName string) string {
	return clusterRepoName(extensionName)
}

func clusterRepoGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	}
}

func uiPluginGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "UIPlugin",
	}
}

func clusterRepoName(extensionName string) string {
	return extensionName + "-charts"
}
