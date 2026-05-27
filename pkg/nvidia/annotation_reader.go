package nvidia

import "context"

// ChartAnnotations delegates to the package-internal pkg/oci.AnnotationReader,
// scoping the repository to the nvidiaChartPrefix subtree. The `chart`
// parameter is the bare chart name (e.g. "nim-llm"), as before — the
// caller stays unaware that we share the underlying OCI client with
// sibling chart-discovery packages.
//
// Returns (nil, nil) when the chart has no annotations block.
// Sentinels: ErrChartNotFound on 404, ErrUnauthorized on 401/403,
// ErrUnreachable on transport failures (translated from pkg/oci).
func (d *discoveryImpl) ChartAnnotations(ctx context.Context, chart, version string) (map[string]string, error) {
	d.mu.RLock()
	endpoint := d.settings.RegistryEndpoint
	d.mu.RUnlock()
	if endpoint == "" {
		return nil, ErrNotConfigured
	}
	repo := nvidiaChartPrefix + chart
	ann, err := d.annR.ChartAnnotations(ctx, repo, version)
	if err != nil {
		return nil, translateOCIError(err)
	}
	return ann, nil
}
