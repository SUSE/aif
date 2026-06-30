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
