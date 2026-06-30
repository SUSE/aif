package helm

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"gopkg.in/yaml.v3"
)

type IndexFile struct {
	Entries map[string][]ChartVersion `yaml:"entries"`
}

type ChartVersion struct {
	Version     string            `yaml:"version"`
	Annotations map[string]string `yaml:"annotations"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func FetchIndex(url string) (*IndexFile, error) {
	resp, err := httpClient.Get(url)
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
