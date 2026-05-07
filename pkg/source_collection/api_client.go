package source_collection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultAPIURL  = "https://api.apps.rancher.io"
	defaultOCIHost = "dp.apps.rancher.io"
)

type apiClient struct {
	httpClient *http.Client
	limiter    *rate.Limiter
	log        *slog.Logger

	mu       sync.RWMutex
	settings EngineSettings
}

// NewClient returns a Client that talks to the SUSE Application Collection HTTP API.
func NewClient(log *slog.Logger) Client {
	return &apiClient{
		httpClient: &http.Client{},
		limiter:    rate.NewLimiter(rate.Every(2*time.Second), 1),
		log:        log,
	}
}

func (c *apiClient) UpdateSettings(s EngineSettings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings = s
}

// effectiveSettings returns a copy of the current settings with defaults applied.
func (c *apiClient) effectiveSettings() EngineSettings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := c.settings
	if s.APIURL == "" {
		s.APIURL = defaultAPIURL
	}
	if s.OCIHost == "" {
		s.OCIHost = defaultOCIHost
	}
	return s
}

func (c *apiClient) List(ctx context.Context) ([]CatalogApp, error) {
	settings := c.effectiveSettings()
	nextURL := settings.APIURL + "/v1/applications?packaging_format=HELM_CHART"
	seen := make(map[string]struct{})
	var apps []CatalogApp

	for nextURL != "" {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		resp, err := c.doGet(ctx, settings, nextURL)
		if err != nil {
			return nil, err
		}

		for _, item := range resp.Items {
			if _, dup := seen[item.SlugName]; dup {
				continue
			}
			seen[item.SlugName] = struct{}{}
			apps = append(apps, item.toApp())
		}

		nextURL = resp.Next
	}

	if apps == nil {
		apps = []CatalogApp{}
	}
	return apps, nil
}

var errRetryableStatus = errors.New("retryable HTTP status")

func (c *apiClient) doGet(ctx context.Context, settings EngineSettings, url string) (*apiResponse, error) {
	resp, err := c.fetchAndDecode(ctx, settings, url)
	if err == nil {
		return resp, nil
	}

	if !isRetryable(err) {
		return nil, err
	}

	c.log.Info("retrying after transient error", "url", url, "error", err)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(1 * time.Second):
	}

	resp, err = c.fetchAndDecode(ctx, settings, url)
	if err != nil {
		if errors.Is(err, errRetryableStatus) {
			return nil, fmt.Errorf("%w", ErrUpstreamUnavailable)
		}
		return nil, err
	}
	return resp, nil
}

func isRetryable(err error) bool {
	return errors.Is(err, errRetryableStatus) || errors.Is(err, ErrCatalogMalformed)
}

func (c *apiClient) fetchAndDecode(ctx context.Context, settings EngineSettings, url string) (*apiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if settings.Username != "" || settings.Token != "" {
		req.SetBasicAuth(settings.Username, settings.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		var result apiResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCatalogMalformed, err)
		}
		return &result, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: HTTP %d", ErrAuthFailed, resp.StatusCode)
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w: HTTP %d", errRetryableStatus, resp.StatusCode)
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: HTTP %d", ErrUpstreamUnavailable, resp.StatusCode)
	default:
		return nil, fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}
}

func (c *apiClient) GetChart(ctx context.Context, repo, chart, version string) (*ChartMetadata, error) {
	settings := c.effectiveSettings()
	nextURL := settings.APIURL + "/v1/applications/" + chart + "/versions"

	for nextURL != "" {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		resp, err := c.fetchAndDecodeVersions(ctx, settings, nextURL)
		if err != nil {
			return nil, err
		}

		for _, entry := range resp.Items {
			if entry.Version == version {
				return &ChartMetadata{
					Name:       chart,
					Version:    entry.Version,
					AppVersion: entry.AppVersion,
				}, nil
			}
		}

		nextURL = resp.Next
	}

	return nil, fmt.Errorf("version %s not found for chart %s", version, chart)
}

func (c *apiClient) fetchAndDecodeVersions(ctx context.Context, settings EngineSettings, url string) (*apiVersionsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if settings.Username != "" || settings.Token != "" {
		req.SetBasicAuth(settings.Username, settings.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		var result apiVersionsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCatalogMalformed, err)
		}
		return &result, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: HTTP %d", ErrAuthFailed, resp.StatusCode)
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: HTTP %d", ErrUpstreamUnavailable, resp.StatusCode)
	default:
		return nil, fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}
}
