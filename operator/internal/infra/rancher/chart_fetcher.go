package rancher

import (
	"context"
	"errors"
)

// ErrChartTooLarge distinguishes content limits from retryable network errors.
var ErrChartTooLarge = errors.New("Git chart exceeds size limit")

// ErrClusterRepoNotReady marks a repository that is missing or still being indexed.
var ErrClusterRepoNotReady = errors.New("cluster repo not ready")

// ChartFetcher reads an exact chart version at the ClusterRepo revision used
// by the reconciler. Implementations must not substitute a moving branch tip.
type ChartFetcher interface {
	FetchChart(ctx context.Context, repoName, chartName, version, revision string) ([]byte, error)
}
