package nvidia

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/SUSE/aif/pkg/helm_oci"
)

// annotationCacheEntry holds one chart's last-known annotations together
// with the manifest digest under which they were fetched. A new digest
// observed via HEAD triggers a re-fetch and entry replacement.
type annotationCacheEntry struct {
	digest      string
	annotations map[string]string
}

// ChartAnnotations fetches the chart's OCI manifest and the chart
// tarball layer, then merges annotations from both sources: OCI
// manifest-level annotations (set by `helm push`, e.g.
// org.opencontainers.image.created) and Chart.yaml annotations (set
// by the chart author, e.g. ai.suse.com/role). Chart.yaml annotations
// take precedence on key collisions. Per-chart digest cache
// short-circuits when the manifest hasn't changed.
//
// Returns (nil, nil) when neither source has annotations. Returns
// ErrChartNotFound on 404, ErrUnauthorized on 401/403, ErrUnreachable
// on transport errors.
func (d *discoveryImpl) ChartAnnotations(ctx context.Context, chart, version string) (map[string]string, error) {
	d.mu.RLock()
	client := d.client
	d.mu.RUnlock()
	if client == nil {
		return nil, ErrNotConfigured
	}

	repo := nvidiaChartPrefix + chart
	manifestPath := "/v2/" + repo + "/manifests/" + version

	digest, err := d.headManifestDigest(ctx, client, manifestPath)
	if err != nil {
		return nil, err
	}

	d.mu.RLock()
	entry, ok := d.annCache[chart]
	d.mu.RUnlock()
	if ok && entry.digest == digest {
		return entry.annotations, nil
	}

	manifest, err := d.fetchBytes(ctx, client, manifestPath)
	if err != nil {
		return nil, err
	}

	manifestAnns, err := helm_oci.ExtractManifestAnnotations(manifest)
	if err != nil {
		return nil, fmt.Errorf("nvidia: %w", err)
	}

	layerDigest, err := helm_oci.FindChartLayerDigest(manifest)
	if err != nil {
		return nil, fmt.Errorf("nvidia: %w", err)
	}
	body, err := d.fetchBytes(ctx, client, "/v2/"+repo+"/blobs/"+layerDigest)
	if err != nil {
		return nil, err
	}
	chartAnns, err := helm_oci.ExtractChartYamlAnnotations(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("nvidia: %w", err)
	}

	if len(manifestAnns) == 0 && len(chartAnns) == 0 {
		d.mu.Lock()
		d.annCache[chart] = annotationCacheEntry{digest: digest}
		d.mu.Unlock()
		return nil, nil
	}

	annotations := make(map[string]string, len(manifestAnns)+len(chartAnns))
	for k, v := range manifestAnns {
		annotations[k] = v
	}
	for k, v := range chartAnns {
		annotations[k] = v
	}

	d.mu.Lock()
	d.annCache[chart] = annotationCacheEntry{digest: digest, annotations: annotations}
	d.mu.Unlock()
	return annotations, nil
}

// headManifestDigest issues a HEAD against the manifest path and reads
// Docker-Content-Digest. Reuses the registry client's auth handshake.
func (d *discoveryImpl) headManifestDigest(ctx context.Context, c *registryClient, path string) (string, error) {
	resp, err := c.headWithAuth(ctx, c.endpoint+path)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		// proceed
	case http.StatusNotFound:
		return "", fmt.Errorf("%w: %s", ErrChartNotFound, resp.Status)
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("%w: %s", ErrUnauthorized, resp.Status)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnexpectedResponse, resp.Status)
	}
	return resp.Header.Get("Docker-Content-Digest"), nil
}

// fetchBytes performs a GET via the registry client's auth-aware
// transport and returns the full response body.
func (d *discoveryImpl) fetchBytes(ctx context.Context, c *registryClient, path string) ([]byte, error) {
	resp, err := c.doWithAuth(ctx, c.endpoint+path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		// proceed
	case http.StatusNotFound:
		return nil, fmt.Errorf("%w: %s", ErrChartNotFound, resp.Status)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%w: %s", ErrUnauthorized, resp.Status)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnexpectedResponse, resp.Status)
	}
	const maxBlobSize = 16 << 20 // 16 MiB; Helm charts are tiny
	buf, err := helm_oci.ReadAllLimited(resp.Body, maxBlobSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnexpectedResponse, err)
	}
	return buf, nil
}
