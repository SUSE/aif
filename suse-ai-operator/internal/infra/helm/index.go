package helm

import (
	"fmt"
	"io"
	"net/http"

	"gopkg.in/yaml.v3"
)

type IndexFile struct {
	Entries map[string][]ChartVersion `yaml:"entries"`
}

type ChartVersion struct {
	Version     string            `yaml:"version"`
	Annotations map[string]string `yaml:"annotations"`
}

func FetchIndex(url string) (*IndexFile, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch index.yaml: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var index IndexFile
	if err := yaml.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	return &index, nil
}

func FindLatestVersion(index *IndexFile, chartName string) (string, error) {
	versions, ok := index.Entries[chartName]
	if !ok {
		return "", fmt.Errorf("chart %q not found in index", chartName)
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions found for chart %q", chartName)
	}
	return versions[0].Version, nil
}

func FindAnnotations(
	index *IndexFile,
	chartName string,
	version string,
) (map[string]string, error) {

	versions, ok := index.Entries[chartName]
	if !ok {
		return nil, fmt.Errorf("chart %q not found in index", chartName)
	}

	for _, v := range versions {
		if v.Version == version {
			return v.Annotations, nil
		}
	}

	return nil, fmt.Errorf(
		"version %q not found for chart %q",
		version,
		chartName,
	)
}
