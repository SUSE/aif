package rancher

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/openapi"
	restclient "k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fakeDiscovery struct {
	shouldFail bool
}

func (f *fakeDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if f.shouldFail {
		return nil, errors.New("UIPlugin CRD not found")
	}
	return &metav1.APIResourceList{GroupVersion: groupVersion}, nil
}

func (f *fakeDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, nil, errors.New("not implemented")
}
func (f *fakeDiscovery) ServerGroups() (*metav1.APIGroupList, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeDiscovery) ServerResourcesForGroupVersionKind(_ schema.GroupVersionKind) (*metav1.APIResourceList, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeDiscovery) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeDiscovery) ServerVersion() (*version.Info, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeDiscovery) OpenAPISchema() (*openapi_v2.Document, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeDiscovery) OpenAPIV3() openapi.Client { return nil }
func (f *fakeDiscovery) RESTClient() restclient.Interface { return nil }
func (f *fakeDiscovery) WithLegacy() discovery.DiscoveryInterface { return f }

var _ discovery.DiscoveryInterface = &fakeDiscovery{}

func TestCheckCRDs(t *testing.T) {
	tests := []struct {
		name        string
		shouldFail  bool
		expectError bool
	}{
		{"CRDs exist", false, false},
		{"CRDs missing", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(nil, &fakeDiscovery{shouldFail: tt.shouldFail}, nil)
			err := m.CheckCRDs(context.Background())
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestEnsureUIPlugin(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	m := New(fakeClient, nil, nil)

	meta := PluginMetadata{
		DisplayName:       "Test Extension",
		RancherVersion:    ">= 2.10.0",
		ExtensionsVersion: ">= 3.0.0 < 4.0.0",
	}

	err := m.EnsureUIPlugin(context.Background(), UIPluginOpts{
		ExtensionName:    "ai-factory",
		ExtensionVersion: "1.0.0",
		CRName:           "test-ext",
		Endpoint:         "http://test-svc:8080/plugin/ai-factory-1.0.0",
		Metadata:         meta,
	})
	if err != nil {
		t.Fatalf("EnsureUIPlugin failed: %v", err)
	}

	var plugin unstructured.Unstructured
	plugin.SetGroupVersionKind(uiPluginGVK())
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "ai-factory",
		Namespace: UIPluginNamespace,
	}, &plugin); err != nil {
		t.Fatalf("UIPlugin not found: %v", err)
	}

	pluginLabels := plugin.GetLabels()
	if pluginLabels["ai.suse.com/installaiextension"] != "test-ext" {
		t.Errorf("expected back-reference label, got %q", pluginLabels["ai.suse.com/installaiextension"])
	}

	name, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "name")
	if name != "ai-factory" {
		t.Errorf("expected plugin name ai-factory, got %q", name)
	}
	ver, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "version")
	if ver != "1.0.0" {
		t.Errorf("expected plugin version 1.0.0, got %q", ver)
	}
	endpoint, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "endpoint")
	if endpoint != "http://test-svc:8080/plugin/ai-factory-1.0.0" {
		t.Errorf("expected plugin endpoint, got %q", endpoint)
	}
	displayName, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "metadata", "catalog.cattle.io/display-name")
	if displayName != "Test Extension" {
		t.Errorf("expected display-name, got %q", displayName)
	}
}

func TestEnsureUIPlugin_Update(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	m := New(fakeClient, nil, nil)

	err := m.EnsureUIPlugin(context.Background(), UIPluginOpts{
		ExtensionName:    "ai-factory",
		ExtensionVersion: "0.9.0",
		CRName:           "test-ext",
		Endpoint:         "http://old-svc:8080/plugin/ai-factory-0.9.0",
		Metadata:         PluginMetadata{DisplayName: "Old Name", RancherVersion: ">= 2.9.0", ExtensionsVersion: ">= 2.0.0 < 3.0.0"},
	})
	if err != nil {
		t.Fatalf("initial EnsureUIPlugin failed: %v", err)
	}

	err = m.EnsureUIPlugin(context.Background(), UIPluginOpts{
		ExtensionName:    "ai-factory",
		ExtensionVersion: "1.0.0",
		CRName:           "test-ext",
		Endpoint:         "http://new-svc:8080/plugin/ai-factory-1.0.0",
		Metadata:         PluginMetadata{DisplayName: "Updated Name", RancherVersion: ">= 2.10.0", ExtensionsVersion: ">= 3.0.0 < 4.0.0"},
	})
	if err != nil {
		t.Fatalf("update EnsureUIPlugin failed: %v", err)
	}

	var plugin unstructured.Unstructured
	plugin.SetGroupVersionKind(uiPluginGVK())
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "ai-factory",
		Namespace: UIPluginNamespace,
	}, &plugin); err != nil {
		t.Fatalf("UIPlugin not found: %v", err)
	}

	endpoint, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "endpoint")
	if endpoint != "http://new-svc:8080/plugin/ai-factory-1.0.0" {
		t.Errorf("expected updated endpoint, got %q", endpoint)
	}
	displayName, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "metadata", "catalog.cattle.io/display-name")
	if displayName != "Updated Name" {
		t.Errorf("expected updated display-name, got %q", displayName)
	}
	rancherVer, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "metadata", "catalog.cattle.io/rancher-version")
	if rancherVer != ">= 2.10.0" {
		t.Errorf("expected updated rancher-version, got %q", rancherVer)
	}
}

func TestEnsureClusterRepo(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	m := New(fakeClient, nil, nil)

	err := m.EnsureClusterRepo(context.Background(), ClusterRepoOpts{
		ExtensionName: "ai-factory",
		CRName:        "test-ext",
		ServiceURL:    "http://ai-factory.cattle-ui-plugin-system.svc.cluster.local:8080",
	})
	if err != nil {
		t.Fatalf("EnsureClusterRepo failed: %v", err)
	}

	var repo unstructured.Unstructured
	repo.SetGroupVersionKind(clusterRepoGVK())
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "ai-factory-charts",
	}, &repo); err != nil {
		t.Fatalf("ClusterRepo not found: %v", err)
	}

	repoLabels := repo.GetLabels()
	if repoLabels["ai.suse.com/installaiextension"] != "test-ext" {
		t.Errorf("expected back-reference label, got %q", repoLabels["ai.suse.com/installaiextension"])
	}

	url, _, _ := unstructured.NestedString(repo.Object, "spec", "url")
	if url != "http://ai-factory.cattle-ui-plugin-system.svc.cluster.local:8080" {
		t.Errorf("expected service URL, got %q", url)
	}

	gitRepo, found, _ := unstructured.NestedString(repo.Object, "spec", "gitRepo")
	if found {
		t.Errorf("expected no gitRepo field, got %q", gitRepo)
	}
}

func TestEnsureClusterRepoGit(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	m := New(fakeClient, nil, nil)

	err := m.EnsureClusterRepoGit(context.Background(), ClusterRepoGitOpts{
		ExtensionName: "ai-factory",
		CRName:        "test-ext",
		RepoURL:       "https://github.com/suse/aif-ui-extension",
		Branch:        "gh-pages",
	})
	if err != nil {
		t.Fatalf("EnsureClusterRepoGit failed: %v", err)
	}

	var repo unstructured.Unstructured
	repo.SetGroupVersionKind(clusterRepoGVK())
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "ai-factory-charts",
	}, &repo); err != nil {
		t.Fatalf("ClusterRepo not found: %v", err)
	}

	gitRepoURL, _, _ := unstructured.NestedString(repo.Object, "spec", "gitRepo")
	if gitRepoURL != "https://github.com/suse/aif-ui-extension" {
		t.Errorf("expected git repo URL, got %q", gitRepoURL)
	}

	branch, _, _ := unstructured.NestedString(repo.Object, "spec", "gitBranch")
	if branch != "gh-pages" {
		t.Errorf("expected branch gh-pages, got %q", branch)
	}

	urlField, found, _ := unstructured.NestedString(repo.Object, "spec", "url")
	if found {
		t.Errorf("expected no url field for git mode, got %q", urlField)
	}
}

func TestDeleteClusterRepo(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	m := New(fakeClient, nil, nil)

	if err := m.EnsureClusterRepo(context.Background(), ClusterRepoOpts{
		ExtensionName: "ai-factory",
		CRName:        "test-ext",
		ServiceURL:    "http://svc:8080",
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := m.DeleteClusterRepo(context.Background(), "ai-factory"); err != nil {
		t.Fatalf("DeleteClusterRepo failed: %v", err)
	}

	var repo unstructured.Unstructured
	repo.SetGroupVersionKind(clusterRepoGVK())
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "ai-factory-charts"}, &repo)
	if err == nil {
		t.Error("expected ClusterRepo to be deleted, but it still exists")
	}
}

func TestDeleteClusterRepo_NotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	m := New(fakeClient, nil, nil)

	err := m.DeleteClusterRepo(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("expected no error for not-found delete, got %v", err)
	}
}

func TestDeleteUIPlugin(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	m := New(fakeClient, nil, nil)

	if err := m.EnsureUIPlugin(context.Background(), UIPluginOpts{
		ExtensionName:    "ai-factory",
		ExtensionVersion: "1.0.0",
		CRName:           "test-ext",
		Endpoint:         "http://svc:8080",
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := m.DeleteUIPlugin(context.Background(), "ai-factory"); err != nil {
		t.Fatalf("DeleteUIPlugin failed: %v", err)
	}

	var plugin unstructured.Unstructured
	plugin.SetGroupVersionKind(uiPluginGVK())
	err := fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "ai-factory",
		Namespace: UIPluginNamespace,
	}, &plugin)
	if err == nil {
		t.Error("expected UIPlugin to be deleted, but it still exists")
	}
}

func TestDeleteUIPlugin_NotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	m := New(fakeClient, nil, nil)

	err := m.DeleteUIPlugin(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("expected no error for not-found delete, got %v", err)
	}
}

func TestFetchIndexMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		fmt.Fprint(w, `apiVersion: v1
entries:
  my-extension:
    - name: my-extension
      version: "1.2.0"
      annotations:
        catalog.cattle.io/display-name: My Extension
        catalog.cattle.io/rancher-version: ">= 2.10.0"
        catalog.cattle.io/ui-extensions-version: ">= 3.0.0 < 4.0.0"
    - name: my-extension
      version: "1.1.0"
      annotations:
        catalog.cattle.io/display-name: My Extension Old
`)
	}))
	defer server.Close()

	m := New(nil, nil, server.Client())

	meta, err := m.FetchIndexMetadata(context.Background(), server.URL+"/index.yaml", "my-extension", "1.2.0")
	if err != nil {
		t.Fatalf("FetchIndexMetadata failed: %v", err)
	}
	if meta.DisplayName != "My Extension" {
		t.Errorf("expected display name 'My Extension', got %q", meta.DisplayName)
	}
	if meta.RancherVersion != ">= 2.10.0" {
		t.Errorf("expected rancher version, got %q", meta.RancherVersion)
	}
	if meta.ExtensionsVersion != ">= 3.0.0 < 4.0.0" {
		t.Errorf("expected extensions version, got %q", meta.ExtensionsVersion)
	}
}

func TestFetchIndexMetadata_VersionNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `apiVersion: v1
entries:
  my-extension:
    - name: my-extension
      version: "1.0.0"
`)
	}))
	defer server.Close()

	m := New(nil, nil, server.Client())

	_, err := m.FetchIndexMetadata(context.Background(), server.URL+"/index.yaml", "my-extension", "9.9.9")
	if err == nil {
		t.Error("expected error for missing version, got nil")
	}
}

func TestDeriveReleaseName(t *testing.T) {
	tests := []struct {
		chartURL string
		expected string
	}{
		{"oci://registry.suse.com/ai/charts/aif-ui", "aif-ui"},
		{"oci://registry.suse.com/ai/charts/aif-ui:1.0.0", "aif-ui"},
		{"oci://ghcr.io/suse/chart/suse-ai-lifecycle-manager", "suse-ai-lifecycle-manager"},
		{"https://example.com/charts/my-extension", "my-extension"},
	}
	for _, tt := range tests {
		t.Run(tt.chartURL, func(t *testing.T) {
			got := DeriveReleaseName(tt.chartURL)
			if got != tt.expected {
				t.Errorf("DeriveReleaseName(%q) = %q, want %q", tt.chartURL, got, tt.expected)
			}
		})
	}
}

func TestGitRepoToRawURL(t *testing.T) {
	tests := []struct {
		name      string
		repoURL   string
		branch    string
		expected  string
		expectErr bool
	}{
		{
			name:     "standard github URL",
			repoURL:  "https://github.com/suse/aif-ui-extension",
			branch:   "gh-pages",
			expected: "https://raw.githubusercontent.com/suse/aif-ui-extension/refs/heads/gh-pages",
		},
		{
			name:     "github URL with .git suffix",
			repoURL:  "https://github.com/suse/aif-ui-extension.git",
			branch:   "main",
			expected: "https://raw.githubusercontent.com/suse/aif-ui-extension/refs/heads/main",
		},
		{
			name:      "non-github host",
			repoURL:   "https://gitlab.com/suse/aif-ui-extension",
			branch:    "gh-pages",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GitRepoToRawURL(tt.repoURL, tt.branch)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestClusterRepoName(t *testing.T) {
	if got := ClusterRepoName("ai-factory"); got != "ai-factory-charts" {
		t.Errorf("ClusterRepoName(ai-factory) = %q, want ai-factory-charts", got)
	}
}
