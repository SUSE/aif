package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/SUSE/aif-operator/internal/catalog"
)

// ngcAPIBase is the NGC API host serving the authenticated per-chart details
// endpoint used to enrich catalog entries with a full overview and latest version.
const ngcAPIBase = "https://api.ngc.nvidia.com"

// maxDetailsConcurrency bounds how many chart-details requests run at once, so a
// full catalog refresh does not hammer the NGC API.
const maxDetailsConcurrency = 8

// detailsRequestTimeout bounds a single chart-details request.
const detailsRequestTimeout = 30 * time.Second

// maxDetailsBodyBytes caps how much of a details response body is read, guarding
// against an oversized or runaway response.
const maxDetailsBodyBytes = 2 << 20 // 2 MiB

// chartDetails is the subset of the NGC chart-details response the catalog needs.
type chartDetails struct {
	Overview      string
	LatestVersion string
	UpdatedDate   string
}

// detailsByChart maps a chartKey to the details fetched for it.
type detailsByChart map[string]chartDetails

// chartKey identifies a chart by its repository URL and slug, matching how
// catalog items are keyed elsewhere in the generator.
func chartKey(repositoryURL, slug string) string {
	return strings.TrimRight(strings.TrimSpace(repositoryURL), "/") + "|" + slug
}

// ngcDetailsPath maps an NGC Helm repository URL and chart slug to the path of
// NGC's authenticated per-chart details endpoint. It returns false for any URL
// that is not an NGC Helm repo (https on helm.ngc.nvidia.com), one with no org
// segment, one with more than an org/team pair, or an empty slug.
func ngcDetailsPath(repositoryURL, slug string) (string, bool) {
	if slug == "" {
		return "", false
	}
	trimmed := strings.TrimRight(strings.TrimSpace(repositoryURL), "/")
	if !catalog.IsNGCURL(trimmed) {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	path := strings.Trim(parsed.Path, "/")
	var segs []string
	if path != "" {
		segs = strings.Split(path, "/")
	}
	switch len(segs) {
	case 1:
		return fmt.Sprintf("/v2/org/%s/helm-charts/%s", segs[0], slug), true
	case 2:
		return fmt.Sprintf("/v2/org/%s/team/%s/helm-charts/%s", segs[0], segs[1], slug), true
	default:
		return "", false
	}
}

// ngcDetailsResponse is the shape of NGC's per-chart details response.
type ngcDetailsResponse struct {
	Artifact *struct {
		Description     string `json:"description"`
		LatestVersionID string `json:"latestVersionId"`
		UpdatedDate     string `json:"updatedDate"`
	} `json:"artifact"`
}

// parseDetails extracts chartDetails from a details response body. It errors
// when the body does not parse as JSON or the "artifact" object is absent.
func parseDetails(body []byte) (chartDetails, error) {
	var resp ngcDetailsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return chartDetails{}, fmt.Errorf("parse NGC details response: %w", err)
	}
	if resp.Artifact == nil {
		return chartDetails{}, fmt.Errorf("NGC details response missing artifact")
	}
	return chartDetails{
		Overview:      resp.Artifact.Description,
		LatestVersion: resp.Artifact.LatestVersionID,
		UpdatedDate:   resp.Artifact.UpdatedDate,
	}, nil
}

// fetchAllDetails fetches NGC chart details for every distinct chart referenced
// by items whose repository URL and slug resolve to an NGC details path (other
// items are skipped). Requests run with bounded concurrency. A chart whose
// request fails, returns a non-200 status, or fails to parse is logged as a
// warning and simply absent from the result — it never fails the run.
func fetchAllDetails(
	ctx context.Context, client *http.Client, apiBase, apiKey string, items []catalog.Item,
) detailsByChart {
	type target struct {
		path string
		slug string
	}

	targets := make(map[string]target)
	for _, it := range items {
		path, ok := ngcDetailsPath(it.RepositoryURL, it.SlugName)
		if !ok {
			continue
		}
		key := chartKey(it.RepositoryURL, it.SlugName)
		if _, seen := targets[key]; seen {
			continue
		}
		targets[key] = target{path: path, slug: it.SlugName}
	}

	result := make(detailsByChart, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxDetailsConcurrency)

	for key, t := range targets {
		wg.Add(1)
		go func(key string, t target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			details, err := fetchOneDetails(ctx, client, apiBase, apiKey, t.path)
			if err != nil {
				log.Printf("warning: NGC details for %q: %v", t.slug, err)
				return
			}

			mu.Lock()
			result[key] = details
			mu.Unlock()
		}(key, t)
	}
	wg.Wait()
	return result
}

// fetchOneDetails performs a single bounded, authenticated details request.
func fetchOneDetails(
	ctx context.Context, client *http.Client, apiBase, apiKey, path string,
) (chartDetails, error) {
	reqCtx, cancel := context.WithTimeout(ctx, detailsRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiBase+path, nil)
	if err != nil {
		return chartDetails{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return chartDetails{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDetailsBodyBytes))
	if err != nil {
		return chartDetails{}, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return chartDetails{}, fmt.Errorf("unexpected status %s", resp.Status)
	}

	return parseDetails(body)
}
