package source_collection

import "context"

// FakeClient is an in-memory test double for consumer tests.
type FakeClient struct {
	Apps        []CatalogApp
	Chart       *ChartMetadata
	ListErr     error
	GetChartErr error
	Settings    EngineSettings
}

func (f *FakeClient) List(_ context.Context) ([]CatalogApp, error) {
	return f.Apps, f.ListErr
}

func (f *FakeClient) GetChart(_ context.Context, _, _, _ string) (*ChartMetadata, error) {
	return f.Chart, f.GetChartErr
}

func (f *FakeClient) UpdateSettings(s EngineSettings) {
	f.Settings = s
}
