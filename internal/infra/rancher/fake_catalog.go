package rancher

import (
	"context"
	"sync"
)

// FakeCall records one method invocation against FakeCatalogManager.
type FakeCall struct {
	Method        string
	ExtensionName string
	CRName        string
	// ClusterRepo fields
	ServiceURL string
	RepoURL    string
	Branch     string
	// UIPlugin fields
	ExtensionVersion string
	Endpoint         string
	Metadata         PluginMetadata
	// FetchIndexMetadata fields
	IndexURL     string
	ChartName    string
	ChartVersion string
}

// FakeCatalogManager is a recording fake satisfying CatalogManager.
type FakeCatalogManager struct {
	mu    sync.Mutex
	Calls []FakeCall

	CheckCRDsErr          error
	EnsureClusterRepoErr  error
	EnsureClusterRepoGitErr error
	DeleteClusterRepoErr  error
	EnsureUIPluginErr     error
	DeleteUIPluginErr     error
	FetchIndexMetadataErr error
	FetchIndexMetadataResult PluginMetadata
}

var _ CatalogManager = (*FakeCatalogManager)(nil)

func NewFake() *FakeCatalogManager { return &FakeCatalogManager{} }

func (f *FakeCatalogManager) CheckCRDs(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeCall{Method: "CheckCRDs"})
	return f.CheckCRDsErr
}

func (f *FakeCatalogManager) EnsureClusterRepo(_ context.Context, opts ClusterRepoOpts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeCall{
		Method:        "EnsureClusterRepo",
		ExtensionName: opts.ExtensionName,
		CRName:        opts.CRName,
		ServiceURL:    opts.ServiceURL,
	})
	return f.EnsureClusterRepoErr
}

func (f *FakeCatalogManager) EnsureClusterRepoGit(_ context.Context, opts ClusterRepoGitOpts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeCall{
		Method:        "EnsureClusterRepoGit",
		ExtensionName: opts.ExtensionName,
		CRName:        opts.CRName,
		RepoURL:       opts.RepoURL,
		Branch:        opts.Branch,
	})
	return f.EnsureClusterRepoGitErr
}

func (f *FakeCatalogManager) DeleteClusterRepo(_ context.Context, extensionName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeCall{
		Method:        "DeleteClusterRepo",
		ExtensionName: extensionName,
	})
	return f.DeleteClusterRepoErr
}

func (f *FakeCatalogManager) EnsureUIPlugin(_ context.Context, opts UIPluginOpts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeCall{
		Method:           "EnsureUIPlugin",
		ExtensionName:    opts.ExtensionName,
		ExtensionVersion: opts.ExtensionVersion,
		CRName:           opts.CRName,
		Endpoint:         opts.Endpoint,
		Metadata:         opts.Metadata,
	})
	return f.EnsureUIPluginErr
}

func (f *FakeCatalogManager) DeleteUIPlugin(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeCall{
		Method:        "DeleteUIPlugin",
		ExtensionName: name,
	})
	return f.DeleteUIPluginErr
}

func (f *FakeCatalogManager) FetchIndexMetadata(_ context.Context, indexURL, chartName, chartVersion string) (PluginMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeCall{
		Method:       "FetchIndexMetadata",
		IndexURL:     indexURL,
		ChartName:    chartName,
		ChartVersion: chartVersion,
	})
	return f.FetchIndexMetadataResult, f.FetchIndexMetadataErr
}

// FilterCalls returns calls matching the given method name.
func (f *FakeCatalogManager) FilterCalls(method string) []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []FakeCall
	for _, c := range f.Calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}
