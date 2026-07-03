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

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

func resolveChart(
	opts *action.ChartPathOptions,
	settings *cli.EnvSettings,
	ref string,
) (*chart.Chart, string, error) {

	chartPath, err := opts.LocateChart(ref, settings)
	if err != nil {
		return nil, "", err
	}

	ch, err := loader.Load(chartPath)
	if err != nil {
		return nil, "", err
	}

	if err := action.CheckDependencies(ch, ch.Metadata.Dependencies); err != nil {
		return nil, "", fmt.Errorf("missing dependencies: %w", err)
	}

	return ch, chartPath, nil
}
