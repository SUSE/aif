/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
