// Package source_collection provides a client for the SUSE Application Collection HTTP API.
// It discovers HELM_CHART packaged applications and chart metadata.
package source_collection

import "context"

// Client discovers SUSE Application Collection apps. All methods are
// rate-limited to 30 requests/minute. Credentials and endpoints arrive
// via UpdateSettings; the client never reads Secrets.
type Client interface {
	List(ctx context.Context) ([]CatalogApp, error)
	GetChart(ctx context.Context, repo, chart, version string) (*ChartMetadata, error)
	UpdateSettings(s EngineSettings)
}

// AnnotationReader fetches a chart's Chart.yaml annotations from the
// Application Collection's OCI host. Separated from Client to keep the
// Client interface within the ≤4 ISP target. Implemented by the same
// backing struct as Client (see api_client.go); consumers receive both
// ports from NewClient.
type AnnotationReader interface {
	ChartAnnotations(ctx context.Context, repo, chart, version string) (map[string]string, error)
}

// EngineSettings holds configuration pushed from Settings CRD.
type EngineSettings struct {
	APIURL   string
	OCIHost  string
	Username string
	Token    string
}
