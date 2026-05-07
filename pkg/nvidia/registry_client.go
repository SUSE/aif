package nvidia

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// registryClient is a thin HTTP adapter over the OCI Distribution v2 API.
// It is unexported because consumers depend on the Discovery port, not on
// the raw catalog client.
//
// The two methods here implement the contract from ARCHITECTURE.md §13.1
// "How AIF discovers NIMs": _catalog enumeration + per-repo tag listing,
// with OCI Bearer-token exchange (RFC: distribution/spec/auth/token) and
// Link-header pagination.
type registryClient struct {
	httpClient *http.Client
	endpoint   string // base URL, e.g. "https://registry.suse.com"; no trailing slash
	username   string
	token      string
	logger     *slog.Logger // nil-safe; per-page debug logging only
}

// newRegistryClient is the constructor. The HTTP client is injected so
// tests can supply httptest.Server.Client(); production callers pass a
// configured *http.Client (with timeouts). logger may be nil — all
// internal log calls are nil-safe (debug-level pagination tracing only).
func newRegistryClient(httpClient *http.Client, endpoint, username, token string, logger *slog.Logger) *registryClient {
	return &registryClient{
		httpClient: httpClient,
		endpoint:   strings.TrimRight(endpoint, "/"),
		username:   username,
		token:      token,
		logger:     logger,
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
// Emits a debug log per page so multi-page catalog walks can be traced
// in production (Recommendation 3 from PR #8 review).
func (c *registryClient) ListRepositories(ctx context.Context) ([]string, error) {
	var all []string
	next := "/v2/_catalog"
	for page := 1; next != ""; page++ {
		start := time.Now()
		var body catalogResponse
		nextLink, err := c.getJSON(ctx, next, &body)
		if err != nil {
			return nil, err
		}
		all = append(all, body.Repositories...)
		c.debug("registry _catalog page fetched",
			"page", page, "items", len(body.Repositories), "running_total", len(all),
			"duration", time.Since(start), "has_next", nextLink != "")
		next = nextLink
	}
	return all, nil
}

// ListTags walks /v2/<repo>/tags/list with the same pagination handling
// as ListRepositories.
func (c *registryClient) ListTags(ctx context.Context, repo string) ([]string, error) {
	var all []string
	next := "/v2/" + repo + "/tags/list"
	for page := 1; next != ""; page++ {
		start := time.Now()
		var body tagsResponse
		nextLink, err := c.getJSON(ctx, next, &body)
		if err != nil {
			return nil, err
		}
		all = append(all, body.Tags...)
		c.debug("registry tags/list page fetched",
			"repo", repo, "page", page, "items", len(body.Tags), "running_total", len(all),
			"duration", time.Since(start), "has_next", nextLink != "")
		next = nextLink
	}
	return all, nil
}

// debug forwards to the optional logger. nil-safe so tests need not
// supply one.
func (c *registryClient) debug(msg string, args ...any) {
	if c.logger == nil {
		return
	}
	c.logger.Debug(msg, args...)
}

// getJSON performs a GET, classifies the response, and decodes the body
// into out. The string return is the relative path of the next page
// (from the Link header) or "" if no more pages. Authentication —
// including the OCI Bearer-token exchange — is handled by doWithAuth.
func (c *registryClient) getJSON(ctx context.Context, pathOrURL string, out any) (string, error) {
	u := pathOrURL
	if strings.HasPrefix(pathOrURL, "/") {
		u = c.endpoint + pathOrURL
	}

	resp, err := c.doWithAuth(ctx, u)
	if err != nil {
		return "", err
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

// doWithAuth executes the GET against url. If the registry responds with
// 401 carrying a `Www-Authenticate: Bearer realm=...,service=...,scope=...`
// challenge AND credentials are configured, it performs a Basic→Bearer
// exchange against the challenge realm and retries the request once with
// the resulting Bearer token. Otherwise the first response is returned
// unchanged for the caller to classify.
func (c *registryClient) doWithAuth(ctx context.Context, url string) (*http.Response, error) {
	resp, err := c.do(ctx, url, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	challenge := parseBearerChallenge(resp.Header.Get("Www-Authenticate"))
	if challenge.realm == "" || (c.username == "" && c.token == "") {
		return resp, nil
	}
	_ = resp.Body.Close()

	bearer, err := c.fetchBearerToken(ctx, challenge)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, url, bearer)
}

// do builds and dispatches a single GET. When bearer is non-empty it is
// sent as `Authorization: Bearer <token>`; otherwise Basic credentials
// are attached when configured. Network failures are wrapped as
// ErrUnreachable.
func (c *registryClient) do(ctx context.Context, url, bearer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	switch {
	case bearer != "":
		req.Header.Set("Authorization", "Bearer "+bearer)
	case c.username != "" || c.token != "":
		req.SetBasicAuth(c.username, c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return resp, nil
}

// bearerChallenge captures the parameters of a
// `Www-Authenticate: Bearer realm="...",service="...",scope="..."` header.
type bearerChallenge struct {
	realm   string
	service string
	scope   string
}

// parseBearerChallenge tolerates whitespace and missing fields. Returns
// the zero value if the header is absent or not a Bearer challenge.
func parseBearerChallenge(header string) bearerChallenge {
	var ch bearerChallenge
	if header == "" {
		return ch
	}
	rest := strings.TrimSpace(header)
	if !strings.EqualFold(firstWord(rest), "bearer") {
		return ch
	}
	rest = strings.TrimSpace(rest[len("bearer"):])
	for _, part := range splitChallengeParams(rest) {
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(part[:eq])
		val := strings.Trim(strings.TrimSpace(part[eq+1:]), `"`)
		switch strings.ToLower(key) {
		case "realm":
			ch.realm = val
		case "service":
			ch.service = val
		case "scope":
			ch.scope = val
		}
	}
	return ch
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// splitChallengeParams splits "k1=\"v1\",k2=\"v2\"" on commas that fall
// outside quoted strings. Naive but sufficient for OCI registries.
func splitChallengeParams(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' {
			inQuotes = !inQuotes
			cur.WriteByte(ch)
			continue
		}
		if ch == ',' && !inQuotes {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// tokenResponse is the JSON shape returned by Bearer token endpoints. The
// OCI distribution spec uses "token"; some implementations also emit
// "access_token". We accept either.
type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

// fetchBearerToken performs the Basic→Bearer exchange against the realm
// URL. Returns ErrUnauthorized when the realm rejects the credentials,
// and ErrUnreachable / ErrUnexpectedResponse for transport / parse
// failures.
func (c *registryClient) fetchBearerToken(ctx context.Context, ch bearerChallenge) (string, error) {
	u, err := url.Parse(ch.realm)
	if err != nil {
		return "", fmt.Errorf("%w: parse realm %q: %v", ErrUnexpectedResponse, ch.realm, err)
	}
	q := u.Query()
	if ch.service != "" {
		q.Set("service", ch.service)
	}
	if ch.scope != "" {
		q.Set("scope", ch.scope)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build realm request: %w", err)
	}
	req.SetBasicAuth(c.username, c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: realm %s: %v", ErrUnreachable, ch.realm, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		// proceed
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("%w: realm %s rejected credentials", ErrUnauthorized, ch.realm)
	default:
		return "", fmt.Errorf("%w: realm %s: %s", ErrUnexpectedResponse, ch.realm, resp.Status)
	}
	var body tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("%w: decode token body: %v", ErrUnexpectedResponse, err)
	}
	if body.Token != "" {
		return body.Token, nil
	}
	if body.AccessToken != "" {
		return body.AccessToken, nil
	}
	return "", fmt.Errorf("%w: realm %s returned empty token", ErrUnexpectedResponse, ch.realm)
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
