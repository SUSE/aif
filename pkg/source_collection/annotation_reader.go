package source_collection

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/SUSE/aif/pkg/helm_oci"
)

type annotationCacheEntry struct {
	digest      string
	annotations map[string]string
}

func (c *apiClient) ChartAnnotations(ctx context.Context, repo, chart, version string) (map[string]string, error) {
	settings, err := c.effectiveAnnotationSettings()
	if err != nil {
		return nil, err
	}

	manifestPath := strings.TrimRight(settings.OCIHost, "/") + "/v2/" + repo + "/" + chart + "/manifests/" + version

	digest, err := c.headOCIManifest(ctx, settings, manifestPath)
	if err != nil {
		return nil, err
	}

	c.mu.RLock()
	entry, ok := c.annCache[chart]
	c.mu.RUnlock()
	if ok && entry.digest == digest {
		return entry.annotations, nil
	}

	manifest, err := c.getOCIBytes(ctx, settings, manifestPath)
	if err != nil {
		return nil, err
	}
	layerDigest, err := helm_oci.FindChartLayerDigest(manifest)
	if err != nil {
		return nil, fmt.Errorf("source_collection: %w", err)
	}
	blobPath := strings.TrimRight(settings.OCIHost, "/") + "/v2/" + repo + "/" + chart + "/blobs/" + layerDigest
	body, err := c.getOCIBytes(ctx, settings, blobPath)
	if err != nil {
		return nil, err
	}
	annotations, err := helm_oci.ExtractChartYamlAnnotations(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("source_collection: %w", err)
	}

	c.mu.Lock()
	c.annCache[chart] = annotationCacheEntry{digest: digest, annotations: annotations}
	c.mu.Unlock()
	return annotations, nil
}

func (c *apiClient) effectiveAnnotationSettings() (EngineSettings, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.settings.OCIHost == "" {
		return EngineSettings{}, ErrNotConfigured
	}
	return c.settings, nil
}

func (c *apiClient) headOCIManifest(ctx context.Context, s EngineSettings, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if s.Username != "" || s.Token != "" {
		req.SetBasicAuth(s.Username, s.Token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
		return resp.Header.Get("Docker-Content-Digest"), nil
	case resp.StatusCode == http.StatusNotFound:
		return "", fmt.Errorf("%w: HTTP %d", ErrChartNotFound, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", fmt.Errorf("%w: HTTP %d", ErrAuthFailed, resp.StatusCode)
	default:
		return "", fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}
}

func (c *apiClient) getOCIBytes(ctx context.Context, s EngineSettings, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if s.Username != "" || s.Token != "" {
		req.SetBasicAuth(s.Username, s.Token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
		const maxBlobSize = 16 << 20
		lr := &io.LimitedReader{R: resp.Body, N: maxBlobSize + 1}
		buf, err := io.ReadAll(lr)
		if err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}
		if int64(len(buf)) > maxBlobSize {
			return nil, fmt.Errorf("blob exceeds %d-byte limit", maxBlobSize)
		}
		return buf, nil
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%w: HTTP %d", ErrChartNotFound, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: HTTP %d", ErrAuthFailed, resp.StatusCode)
	default:
		return nil, fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}
}
