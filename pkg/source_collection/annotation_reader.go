package source_collection

import (
	"bytes"
	"context"
	"fmt"
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

	base := registryBaseURL(settings.OCIHost) + "/v2/" + normalizeRepoPath(repo) + "/" + chart
	manifestPath := base + "/manifests/" + version

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
	blobPath := base + "/blobs/" + layerDigest
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

// registryBaseURL turns the OCIHost setting into a scheme-bearing base URL.
// buildChartRef (api_client.go) documents OCIHost as a bare host, but it also
// tolerates a scheme; mirror that here so a misconfigured "https://host" or
// "http://host" still produces a valid URL.
func registryBaseURL(host string) string {
	host = strings.TrimRight(host, "/")
	if strings.Contains(host, "://") {
		return host
	}
	return "https://" + host
}

// normalizeRepoPath drops the "oci://<host>/" prefix that parseAppCoChartRef
// leaves on App.ChartRef.Repo. The annotation reader needs only the path
// portion ("charts") to compose registry URLs; the scheme + host come from
// OCIHost. Callers that already pass a bare path component pass through.
func normalizeRepoPath(repo string) string {
	if rest, ok := strings.CutPrefix(repo, "oci://"); ok {
		if i := strings.Index(rest, "/"); i >= 0 {
			return strings.Trim(rest[i+1:], "/")
		}
		return ""
	}
	return strings.Trim(repo, "/")
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
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Header.Get("Docker-Content-Digest"), nil
	case http.StatusNotFound:
		return "", fmt.Errorf("%w: HTTP %d", ErrChartNotFound, resp.StatusCode)
	case http.StatusUnauthorized, http.StatusForbidden:
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
	switch resp.StatusCode {
	case http.StatusOK:
		const maxBlobSize = 16 << 20
		return helm_oci.ReadAllLimited(resp.Body, maxBlobSize)
	case http.StatusNotFound:
		return nil, fmt.Errorf("%w: HTTP %d", ErrChartNotFound, resp.StatusCode)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%w: HTTP %d", ErrAuthFailed, resp.StatusCode)
	default:
		return nil, fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}
}
