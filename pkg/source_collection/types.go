package source_collection

// CatalogApp represents a SUSE Application Collection app in the AIF catalog.
// Publisher is intentionally absent: the upstream list endpoint no longer
// carries it, and pkg/apps/appco_source.go hardcodes "SUSE" at the
// translation boundary (mirroring pkg/apps/nvidia_source.go's "NVIDIA"
// hardcode). Categories and LatestVersion come from the per-app detail
// endpoint, not from the list response.
type CatalogApp struct {
	ID            string
	DisplayName   string
	Description   string
	Categories    []string
	ChartRef      string
	LatestVersion string
	Source        string
	LogoURL       string
	ProjectURL    string
	LastUpdatedAt string
}

// ChartMetadata holds Chart.yaml metadata for a specific chart version.
// Description and Annotations require fetching Chart.yaml from OCI (handled
// by AnnotationReader); GetChart populates only Name, Version, and AppVersion
// from the detail endpoint's branches[] array.
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

// apiAppDetail models /v1/applications/{slug}. The list endpoint returns
// only summary info; per-app version + categories live here.
type apiAppDetail struct {
	SlugName string      `json:"slug_name"`
	Labels   []string    `json:"labels"`
	Branches []apiBranch `json:"branches"`
}

// apiBranch represents one release stream for an app. We pick the highest-
// versioned non-LTS branch's baseline as the "latest" — matching the
// upstream UI's "latest version" presentation.
type apiBranch struct {
	ID         int    `json:"id"`
	BranchName string `json:"branch_name"`
	Baseline   string `json:"baseline"`
	IsLTS      bool   `json:"is_lts"`
}
