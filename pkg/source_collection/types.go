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
	LastUpdatedAt string
}

// ChartMetadata holds Chart.yaml metadata for a specific chart version.
// Description and Annotations require fetching Chart.yaml from OCI (handled by P2-5);
// the /versions API populates only Name, Version, and AppVersion.
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
	LastUpdatedAt string        `json:"last_updated_at"`
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

func (a apiApplication) toApp() CatalogApp {
	categories := make([]string, len(a.Categories))
	for i, c := range a.Categories {
		categories[i] = c.Name
	}
	chartRef := a.Helm.RepositoryURL + "/" + a.Helm.ChartName + ":" + a.LatestVersion.Version
	return CatalogApp{
		ID:            a.SlugName,
		DisplayName:   a.Title,
		Description:   a.Description,
		Publisher:     a.PublisherName,
		Categories:    categories,
		ChartRef:      chartRef,
		LatestVersion: a.LatestVersion.Version,
		Source:        "api",
		LastUpdatedAt: a.LastUpdatedAt,
	}
}
