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
	"bytes"
	"fmt"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

func resolveChart(
	opts *action.ChartPathOptions,
	settings *cli.EnvSettings,
	ref string,
) (*chart.Chart, []byte, error) {

	chartPath, err := opts.LocateChart(ref, settings)
	if err != nil {
		return nil, nil, err
	}

	return loadLocalChart(chartPath)
}

// loadLocalChart parses a chart from a path on disk and hands back the archive it
// was parsed from, so that a caller wanting to reuse the download can keep the
// bytes rather than the path.
//
// The file is read once and parsed out of memory, instead of handing the path to
// loader.Load, so that the archive returned is provably the one the returned
// chart came from. The path is not a stable name for it: LocateChart writes every
// download into one shared directory under a filename taken from the chart's name
// and version alone, so the file here can be a different chart by the time anyone
// reads it again. See cachedChart.
//
// LocateChart can also resolve an unpacked chart directory, which has no archive
// to return. No spec this operator builds reaches that branch — the CRD holds a
// Helm source's chartURL to oci:// or https://, and a Git source's bare chart name
// is resolved against its repository — and a chart already on disk has no download
// worth saving, so it is loaded and reported as nothing to cache.
func loadLocalChart(path string) (*chart.Chart, []byte, error) {
	archive, err := os.ReadFile(path)
	if err != nil {
		ch, err := loadUnpackedChart(path)
		return ch, nil, err
	}

	ch, err := loadArchive(archive)
	if err != nil {
		return nil, nil, err
	}
	return ch, archive, nil
}

// loadArchive parses a chart from the bytes of its .tgz. Every fetch path ends
// here: the OCI and HTTPS downloads that go through LocateChart, the in-memory
// TLS download that never touches disk, and the chart cache re-parsing an archive
// it already holds.
func loadArchive(archive []byte) (*chart.Chart, error) {
	ch, err := loader.LoadArchive(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	return withDependencies(ch)
}

// loadUnpackedChart parses a chart from a directory laid out on disk.
func loadUnpackedChart(path string) (*chart.Chart, error) {
	ch, err := loader.Load(path)
	if err != nil {
		return nil, err
	}
	return withDependencies(ch)
}

func withDependencies(ch *chart.Chart) (*chart.Chart, error) {
	if err := action.CheckDependencies(ch, ch.Metadata.Dependencies); err != nil {
		return nil, fmt.Errorf("missing dependencies: %w", err)
	}
	return ch, nil
}
