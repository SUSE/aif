package source_collection

// CatalogApp represents a SUSE Application Collection app in the AIF catalog.
type CatalogApp struct {
	ID            string
	DisplayName   string
	Description   string
	Publisher     string
	Categories    []string
	ChartRef      string
	LatestVersion string
	Source        string
}

// ChartMetadata holds Chart.yaml metadata for a specific chart version.
type ChartMetadata struct {
	Name        string
	Version     string
	AppVersion  string
	Description string
	Annotations map[string]string
}

type apiResponse struct {
	Items []apiApplication `json:"items"`
	Next  string           `json:"next"`
}

type apiApplication struct {
	SlugName      string        `json:"slug_name"`
	Title         string        `json:"title"`
	Description   string        `json:"description"`
	PublisherName string        `json:"publisher_name"`
	Categories    []apiCategory `json:"categories"`
	Tags          []string      `json:"tags"`
	LogoURL       string        `json:"logo_url"`
	Helm          apiHelm       `json:"helm"`
	LatestVersion apiVersion    `json:"latest_version"`
}

type apiCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type apiHelm struct {
	RepositoryURL string `json:"repository_url"`
	ChartName     string `json:"chart_name"`
}

type apiVersion struct {
	Version string `json:"version"`
}

type apiVersionsResponse struct {
	Items []apiVersionEntry `json:"items"`
	Next  string            `json:"next"`
}

type apiVersionEntry struct {
	Version    string `json:"version"`
	AppVersion string `json:"app_version"`
}
