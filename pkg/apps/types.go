package apps

import "time"

// App is the canonical, source-agnostic representation of a catalog
// entry rendered by GET /api/v1/apps and the Apps page in the UI. The
// shape mirrors ARCHITECTURE.md §5 Apps. Adapters in this package
// translate engine-native types (NIMEntry, source_collection.CatalogApp)
// into App; the engine packages remain unaware of App's existence.
//
// IDs are namespaced by source so dedupe and routing are mechanical;
// the dot separator is chosen so the REST surface uses a plain
// path-segment route (`/api/v1/apps/{id}`) rather than a wildcard:
//
//	nvidia.<chart>:<version>     e.g. nvidia.nim-llm:1.2.0
//	suse.<slug>:<version>        e.g. suse.ollama:0.4.1
type App struct {
	ID                 string     `json:"id"`                 // namespaced; canonical key for dedupe + Get
	Name               string     `json:"name"`               // bare chart/slug name (no namespace, no version)
	DisplayName        string     `json:"displayName"`        // human-readable
	Description        string     `json:"description"`
	Publisher          string     `json:"publisher"`
	Version            string     `json:"version"`           // chart version
	LogoURL            string     `json:"logoURL"`
	Source             string     `json:"source"`            // "nvidia" | "suse"
	AssetType          string     `json:"assetType"`         // "chart" today; reserved for future asset kinds
	Categories         []string   `json:"categories"`        // flattened category names
	Tags               []string   `json:"tags"`
	ChartRef           ChartRef   `json:"chartRef"`
	ProjectURL         string     `json:"projectURL"`
	ReferenceBlueprint bool       `json:"referenceBlueprint"` // populated by P2-5; false by default
	UseCase            string     `json:"useCase,omitempty"`  // populated from ai.suse.com/use-case (consumed by P2-7)
	LastUpdatedAt      *time.Time `json:"lastUpdatedAt,omitempty"` // *time.Time validates at parse time; nil when the source has no timestamp
}

// ChartRef matches ARCHITECTURE.md §5: {repo, chart, version}.
type ChartRef struct {
	Repo    string `json:"repo"`    // OCI repository, including scheme (e.g. oci://registry.suse.com/ai/charts/nvidia)
	Chart   string `json:"chart"`   // chart name within the repo
	Version string `json:"version"`
}

// ListOpts carries filters for Catalog.List. Empty fields mean no filter.
type ListOpts struct {
	Source                   string // "" = all; otherwise must match App.Source
	Category                 string // "" = all; otherwise exact match against App.Categories
	IncludeReferenceBlueprints bool // when false (zero value), apps with ReferenceBlueprint=true are filtered out
}

// EngineSettings is the Catalog-level settings push pushed by
// SettingsReconciler (P5-4). Each adapter slices off the section it
// needs and translates to its engine-native shape.
type EngineSettings struct {
	SUSERegistry          RegistrySettings
	ApplicationCollection AppCollectionSettings
	RefreshInterval       time.Duration // default refresh cadence; adapters fall back to 10m if zero
}

// RegistrySettings is the SUSE Registry slice consumed by NVIDIASource.
type RegistrySettings struct {
	Endpoint string
	Username string
	Token    string
}

// AppCollectionSettings is the SUSE Application Collection slice
// consumed by AppCoSource.
type AppCollectionSettings struct {
	APIURL   string
	OCIHost  string
	Username string
	Token    string
}

// SourceStatus is per-Source health metadata, exposed for diagnostics
// (e.g. /healthz, admin endpoints) and used internally by Catalog to
// decide whether a Source's cache is "stale-but-good" or "never
// succeeded" (the latter is the only condition under which List can
// return a Source-level failure to its caller).
type SourceStatus struct {
	LastSuccessAt time.Time
	LastError     error
	EntryCount    int
}

