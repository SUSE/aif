package nvidia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// registryClient is a thin HTTP adapter over the OCI Distribution v2 API.
// It is unexported because consumers depend on the Discovery port, not on
// the raw catalog client.
//
// The two methods here implement the contract from ARCHITECTURE.md §13.1
// "How AIF discovers NIMs": _catalog enumeration + per-repo tag listing,
// both with HTTP Basic auth and Link-header pagination.
type registryClient struct {
	httpClient *http.Client
	endpoint   string // base URL, e.g. "https://registry.suse.com"; no trailing slash
	username   string
	token      string
}

// newRegistryClient is the constructor. The HTTP client is injected so
// tests can supply httptest.Server.Client(); production callers pass a
// configured *http.Client (with timeouts).
func newRegistryClient(httpClient *http.Client, endpoint, username, token string) *registryClient {
	return &registryClient{
		httpClient: httpClient,
		endpoint:   strings.TrimRight(endpoint, "/"),
		username:   username,
		token:      token,
	}
}

// catalogResponse is the JSON shape of GET /v2/_catalog.
type catalogResponse struct {
	Repositories []string `json:"repositories"`
}

// tagsResponse is the JSON shape of GET /v2/<repo>/tags/list.
type tagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// ListRepositories walks the OCI _catalog endpoint, following Link
// rel="next" pagination until exhausted. Returns the concatenated list.
func (c *registryClient) ListRepositories(ctx context.Context) ([]string, error) {
	var all []string
	next := "/v2/_catalog"
	for next != "" {
		var page catalogResponse
		nextLink, err := c.getJSON(ctx, next, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Repositories...)
		next = nextLink
	}
	return all, nil
}

// ListTags walks /v2/<repo>/tags/list with the same pagination handling
// as ListRepositories.
func (c *registryClient) ListTags(ctx context.Context, repo string) ([]string, error) {
	var all []string
	next := "/v2/" + repo + "/tags/list"
	for next != "" {
		var page tagsResponse
		nextLink, err := c.getJSON(ctx, next, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Tags...)
		next = nextLink
	}
	return all, nil
}

// getJSON performs a GET, applies Basic auth (when credentials are set),
// classifies the response, and decodes the body into out. The string
// return is the relative path of the next page (from the Link header) or
// "" if no more pages.
func (c *registryClient) getJSON(ctx context.Context, pathOrURL string, out any) (string, error) {
	u := pathOrURL
	if strings.HasPrefix(pathOrURL, "/") {
		u = c.endpoint + pathOrURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if c.username != "" || c.token != "" {
		req.SetBasicAuth(c.username, c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		// proceed to decode
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("%w: %s", ErrUnauthorized, resp.Status)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnexpectedResponse, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return "", fmt.Errorf("%w: decode body: %v", ErrUnexpectedResponse, err)
	}
	return parseLinkNext(resp.Header.Get("Link")), nil
}

// parseLinkNext extracts the URL of the rel="next" link from a Link header
// (RFC 5988 / RFC 8288). Returns "" if absent or malformed. Naive parse
// — sufficient for OCI Distribution registries which emit at most one
// rel="next" entry per response.
func parseLinkNext(header string) string {
	if header == "" {
		return ""
	}
	for _, part := range strings.Split(header, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start >= 0 && end > start {
			return part[start+1 : end]
		}
	}
	return ""
}
