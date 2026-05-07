package source_collection

import "context"

// Client discovers SUSE Application Collection apps. Credentials and
// endpoints arrive via UpdateSettings; the client never reads Secrets.
type Client interface {
	List(ctx context.Context) ([]CatalogApp, error)
	GetChart(ctx context.Context, repo, chart, version string) (*ChartMetadata, error)
	UpdateSettings(s EngineSettings)
}

// EngineSettings holds configuration pushed from Settings CRD.
type EngineSettings struct {
	APIURL   string
	OCIHost  string
	Username string
	Token    string
}
