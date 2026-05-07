package source_collection

import (
	"context"
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

func (c *apiClient) List(_ context.Context) ([]CatalogApp, error) {
	return nil, nil
}

func (c *apiClient) GetChart(_ context.Context, _, _, _ string) (*ChartMetadata, error) {
	return nil, nil
}
