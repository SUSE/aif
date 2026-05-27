package source_collection

// CatalogApp represents a SUSE Application Collection app in the AIF catalog.
// Publisher is intentionally absent: the upstream list endpoint no longer
// carries it, and pkg/apps/appco_source.go hardcodes "SUSE" at the
// translation boundary (mirroring pkg/apps/nvidia_source.go's "NVIDIA"
// hardcode).
//
// LatestVersion vs ChartTag: AppCo publishes each chart at an OCI tag of
// the form "<chart-version>-<build-revision>" (e.g. "1.55.0-13.1"); the
// chart's own Chart.yaml :version is just "1.55.0". The two are kept
// separate so callers don't have to guess which one fits their need:
//   - LatestVersion is the chart's Chart.yaml :version (display surface;
//     what the UI shows, what wrapping Blueprints carry per
//     ARCHITECTURE.md §4.3).
//   - ChartTag is the OCI registry tag (the only key that resolves to a
//     chart binary; what ChartRef's ":<tag>" suffix encodes).
// Both come from the /v1/artifacts response.
type CatalogApp struct {
	ID            string
	DisplayName   string
	Description   string
	Categories    []string
	ChartRef      string
	LatestVersion string
	ChartTag      string
	Source        string
	LogoURL       string
	ProjectURL    string
	LastUpdatedAt string
}

// ChartMetadata holds Chart.yaml metadata for a specific chart version.
// Description and Annotations require fetching Chart.yaml from OCI (handled
// by AnnotationReader); GetChart populates only Name, Version, and AppVersion
// from the /v1/artifacts endpoint. Version is the chart's Chart.yaml :version
// (bare, e.g. "1.55.0"), not the OCI registry tag — see CatalogApp.ChartTag
// for the latter.
type ChartMetadata struct {
	Name        string
	Version     string
	AppVersion  string
	Description string
	Annotations map[string]string
}

// apiListResponse models the /v1/applications list envelope.
// Pagination is page-based (no Next URL): page, page_size, total_size,
// total_pages. Maximum page_size enforced by upstream is 100.
type apiListResponse struct {
	Items      []apiListItem `json:"items"`
	Page       int           `json:"page"`
	PageSize   int           `json:"page_size"`
	TotalSize  int           `json:"total_size"`
	TotalPages int           `json:"total_pages"`
}

// apiListItem is what the list endpoint returns per app — minimal metadata
// only. Version, categories, and the helm chart pointer all moved to the
// per-app detail endpoint (apiAppDetail).
type apiListItem struct {
	SlugName        string `json:"slug_name"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	ProjectURL      string `json:"project_url"`
	LogoURL         string `json:"logo_url"`
	LastUpdatedAt   string `json:"last_updated_at"`
	PackagingFormat string `json:"packaging_format"`
}

// apiAppDetail is the slice of GET /v1/applications/{slug} we consume.
// We only read labels[] for Categories; chart version comes from the
// /v1/artifacts endpoint (see fetchLatestChartArtifact).
type apiAppDetail struct {
	SlugName string   `json:"slug_name"`
	Labels   []string `json:"labels"`
}

// apiArtifactsPage is the paged response shape of GET /v1/artifacts.
// Items are sorted by registered_at desc by the upstream — we rely on
// that to read page 1 / size 1 as "most recently registered artifact".
type apiArtifactsPage struct {
	Items      []apiArtifactItem `json:"items"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalSize  int               `json:"total_size"`
	TotalPages int               `json:"total_pages"`
}

// apiArtifactItem is one published artifact. For HELM_CHART artifacts:
//   - Name is "<slug>:<version>-<revision>" (e.g. "ollama:1.55.0-13.1")
//   - Version is the chart version (e.g. "1.55.0")
//   - Revision is the build revision (e.g. "13.1")
//   - ApplicationVersion is the upstream app version (e.g. "0.21.2")
// CONTAINER artifacts also populate Version/Revision but leave
// ApplicationVersion empty; we filter to HELM_CHART at the query layer.
type apiArtifactItem struct {
	Name               string `json:"name"`
	Version            string `json:"version"`
	Revision           string `json:"revision"`
	PackagingFormat    string `json:"packaging_format"`
	ApplicationVersion string `json:"application_version"`
	RegisteredAt       string `json:"registered_at"`
}
