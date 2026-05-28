package helm

import (
	"errors"
	"fmt"

	"sigs.k8s.io/yaml"
)

var (
	ErrChartNotFound   = errors.New("chart not found in index")
	ErrVersionNotFound = errors.New("version not found for chart")
)

// ChartAnnotations holds metadata annotations extracted from a Helm repo
// index.yaml entry. Used by InstallAIExtensionReconciler to populate
// UIPlugin.spec.plugin.metadata.
type ChartAnnotations struct {
	DisplayName       string
	RancherVersion    string
	ExtensionsVersion string
}

type repoIndex struct {
	Entries map[string][]chartEntry `json:"entries"`
}

type chartEntry struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Annotations map[string]string `json:"annotations"`
}

// FindChartAnnotations parses raw index.yaml bytes, locates the entry
// matching chartName and chartVersion, and returns the catalog.cattle.io
// metadata annotations. Returns an error if the chart or version is not
// found in the index.
func FindChartAnnotations(indexData []byte, chartName, chartVersion string) (ChartAnnotations, error) {
	var idx repoIndex
	if err := yaml.Unmarshal(indexData, &idx); err != nil {
		return ChartAnnotations{}, fmt.Errorf("parse index.yaml: %w", err)
	}

	entries, ok := idx.Entries[chartName]
	if !ok {
		return ChartAnnotations{}, fmt.Errorf("%w: %q", ErrChartNotFound, chartName)
	}

	for _, entry := range entries {
		if entry.Version == chartVersion {
			return ChartAnnotations{
				DisplayName:       entry.Annotations["catalog.cattle.io/display-name"],
				RancherVersion:    entry.Annotations["catalog.cattle.io/rancher-version"],
				ExtensionsVersion: entry.Annotations["catalog.cattle.io/ui-extensions-version"],
			}, nil
		}
	}

	return ChartAnnotations{}, fmt.Errorf("%w: %q version %q", ErrVersionNotFound, chartName, chartVersion)
}
