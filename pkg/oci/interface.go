package oci

import "context"

// Walker is the catalog-enumeration port. 3 methods (within ISP).
type Walker interface {
	// EnumerateCharts walks /v2/_catalog and returns every
	// (repository, tag) pair whose repository starts with `prefix`
	// and whose first path segment AFTER `prefix` is not in
	// `excludeFirstSegment`. `prefix` is matched with strings.HasPrefix;
	// `excludeFirstSegment` lets callers skip subtrees like "nvidia"
	// from a broader "ai/charts/" walk without copy-paste.
	EnumerateCharts(ctx context.Context, prefix string, excludeFirstSegment []string) ([]ChartCoordinate, error)

	// ListTags returns the tag list for a single repository.
	// Useful for refreshing a known repository without a full catalog walk.
	ListTags(ctx context.Context, repository string) ([]string, error)

	// UpdateSettings installs credentials + endpoint and rebuilds
	// the internal HTTP client. Empty Endpoint clears the client
	// (subsequent calls return ErrNotConfigured). Synchronous; never
	// reads K8s Secrets directly.
	UpdateSettings(s EngineSettings)
}

// AnnotationReader fetches OCI manifest annotations and Chart.yaml
// annotations for a chart. Returns ErrNotFound on 404, ErrUnauthorized
// on 401/403, ErrUnreachable on transport failures. Returns
// (nil, nil) when neither source has annotations.
type AnnotationReader interface {
	ChartAnnotations(ctx context.Context, repository, tag string) (map[string]string, error)
}
