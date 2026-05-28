package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const ociManifestMediaType = "application/vnd.oci.image.manifest.v1+json"

// walker is the production Walker. Replaces pkg/nvidia.registryClient;
// behaviour is identical, but the prefix logic that used to live here
// (`ai/charts/nvidia/` filtering) moves to callers via EnumerateCharts's
// prefix+exclude parameters.
type walker struct {
	logger     *slog.Logger
	httpClient *http.Client

	mu       sync.RWMutex
	settings EngineSettings
	endpoint string // normalized URL (no trailing slash). "" when not configured.
}

// NewWalker returns a Walker bound to logger.
func NewWalker(logger *slog.Logger) Walker {
	return &walker{
		logger:     logger,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (w *walker) UpdateSettings(s EngineSettings) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.settings = s
	if s.Endpoint == "" {
		w.endpoint = ""
		return
	}
	w.endpoint = normalizeForHTTP(s.Endpoint)
}

func (w *walker) EnumerateCharts(ctx context.Context, prefix string, excludeFirstSegment []string) ([]ChartCoordinate, error) {
	w.mu.RLock()
	endpoint := w.endpoint
	w.mu.RUnlock()
	if endpoint == "" {
		return nil, ErrNotConfigured
	}

	excludeSet := make(map[string]struct{}, len(excludeFirstSegment))
	for _, seg := range excludeFirstSegment {
		excludeSet[seg] = struct{}{}
	}

	repos, err := w.listRepositories(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]ChartCoordinate, 0, len(repos))
	for _, repo := range repos {
		if !strings.HasPrefix(repo, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(repo, prefix)
		firstSeg := remainder
		if i := strings.IndexByte(remainder, '/'); i >= 0 {
			firstSeg = remainder[:i]
		}
		if _, skip := excludeSet[firstSeg]; skip {
			continue
		}
		tags, err := w.listTagsInternal(ctx, repo)
		if err != nil {
			return nil, err
		}
		for _, tag := range tags {
			out = append(out, ChartCoordinate{Repository: repo, Tag: tag})
		}
	}
	return out, nil
}

func (w *walker) ListTags(ctx context.Context, repository string) ([]string, error) {
	w.mu.RLock()
	endpoint := w.endpoint
	w.mu.RUnlock()
	if endpoint == "" {
		return nil, ErrNotConfigured
	}
	return w.listTagsInternal(ctx, repository)
}

// listRepositories: lift pagination body from pkg/nvidia.registryClient.ListRepositories verbatim.
func (w *walker) listRepositories(ctx context.Context) ([]string, error) {
	var all []string
	next := "/v2/_catalog"
	for page := 1; next != ""; page++ {
		start := time.Now()
		var body catalogResponse
		nextLink, err := w.getJSON(ctx, next, &body)
		if err != nil {
			return nil, err
		}
		all = append(all, body.Repositories...)
		w.debug("oci _catalog page fetched",
			"page", page, "items", len(body.Repositories), "running_total", len(all),
			"duration", time.Since(start), "has_next", nextLink != "")
		next = nextLink
	}
	return all, nil
}

func (w *walker) listTagsInternal(ctx context.Context, repo string) ([]string, error) {
	var all []string
	var droppedSigstore int
	next := "/v2/" + repo + "/tags/list"
	for page := 1; next != ""; page++ {
		start := time.Now()
		var body tagsResponse
		nextLink, err := w.getJSON(ctx, next, &body)
		if err != nil {
			return nil, err
		}
		for _, tag := range body.Tags {
			if isSigstoreTag(tag) {
				droppedSigstore++
				continue
			}
			all = append(all, tag)
		}
		w.debug("oci tags/list page fetched",
			"repo", repo, "page", page, "items", len(body.Tags), "running_total", len(all),
			"duration", time.Since(start), "has_next", nextLink != "")
		next = nextLink
	}
	if droppedSigstore > 0 {
		w.debug("oci tags/list sigstore tags filtered", "repo", repo, "dropped", droppedSigstore)
	}
	return all, nil
}

func (w *walker) debug(msg string, args ...any) {
	if w.logger == nil {
		return
	}
	w.logger.Debug(msg, args...)
}

func (w *walker) getJSON(ctx context.Context, pathOrURL string, out any) (string, error) {
	u := pathOrURL
	if strings.HasPrefix(pathOrURL, "/") {
		u = w.endpoint + pathOrURL
	}
	resp, err := w.doWithAuth(ctx, u)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("%w: %s", ErrUnauthorized, resp.Status)
	case http.StatusNotFound:
		return "", fmt.Errorf("%w: %s", ErrNotFound, resp.Status)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnexpectedResponse, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return "", fmt.Errorf("%w: decode body: %v", ErrUnexpectedResponse, err)
	}
	return parseLinkNext(resp.Header.Get("Link")), nil
}

// fetchBytes is a GET that returns the full body — used by AnnotationReader.
// Sentinels match getJSON (404 → ErrNotFound).
func (w *walker) fetchBytes(ctx context.Context, path string) ([]byte, error) {
	w.mu.RLock()
	endpoint := w.endpoint
	w.mu.RUnlock()
	resp, err := w.doWithAuth(ctx, endpoint+path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%w: %s", ErrUnauthorized, resp.Status)
	case http.StatusNotFound:
		return nil, fmt.Errorf("%w: %s", ErrNotFound, resp.Status)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnexpectedResponse, resp.Status)
	}
	const maxBlobSize = 16 << 20 // 16 MiB; Helm charts are tiny
	return readAllLimited(resp.Body, maxBlobSize)
}

// headDigest issues HEAD against path and returns Docker-Content-Digest.
func (w *walker) headDigest(ctx context.Context, path string) (string, error) {
	w.mu.RLock()
	endpoint := w.endpoint
	w.mu.RUnlock()
	resp, err := w.headWithAuth(ctx, endpoint+path)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", fmt.Errorf("%w: %s", ErrNotFound, resp.Status)
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("%w: %s", ErrUnauthorized, resp.Status)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnexpectedResponse, resp.Status)
	}
	return resp.Header.Get("Docker-Content-Digest"), nil
}

// ---- Lifted from pkg/nvidia/registry_client.go ----

type catalogResponse struct {
	Repositories []string `json:"repositories"`
}

type tagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func (w *walker) doWithAuth(ctx context.Context, url string) (*http.Response, error) {
	w.mu.RLock()
	username, token := w.settings.Username, w.settings.Token
	w.mu.RUnlock()

	resp, err := w.do(ctx, url, "", username, token)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	challenge := parseBearerChallenge(resp.Header.Get("Www-Authenticate"))
	if challenge.realm == "" || (username == "" && token == "") {
		return resp, nil
	}
	_ = resp.Body.Close()
	bearer, err := w.fetchBearerToken(ctx, challenge, username, token)
	if err != nil {
		return nil, err
	}
	return w.do(ctx, url, bearer, username, token)
}

func (w *walker) do(ctx context.Context, url, bearer, username, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", ociManifestMediaType)
	switch {
	case bearer != "":
		req.Header.Set("Authorization", "Bearer "+bearer)
	case username != "" || token != "":
		req.SetBasicAuth(username, token)
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return resp, nil
}

func (w *walker) headWithAuth(ctx context.Context, url string) (*http.Response, error) {
	w.mu.RLock()
	username, token := w.settings.Username, w.settings.Token
	w.mu.RUnlock()

	resp, err := w.head(ctx, url, "", username, token)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	challenge := parseBearerChallenge(resp.Header.Get("Www-Authenticate"))
	if challenge.realm == "" || (username == "" && token == "") {
		return resp, nil
	}
	_ = resp.Body.Close()
	bearer, err := w.fetchBearerToken(ctx, challenge, username, token)
	if err != nil {
		return nil, err
	}
	return w.head(ctx, url, bearer, username, token)
}

func (w *walker) head(ctx context.Context, url, bearer, username, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", ociManifestMediaType)
	switch {
	case bearer != "":
		req.Header.Set("Authorization", "Bearer "+bearer)
	case username != "" || token != "":
		req.SetBasicAuth(username, token)
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return resp, nil
}

type bearerChallenge struct {
	realm   string
	service string
	scope   string
}

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

type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

func (w *walker) fetchBearerToken(ctx context.Context, ch bearerChallenge, username, token string) (string, error) {
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
	req.SetBasicAuth(username, token)
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: realm %s: %v", ErrUnreachable, ch.realm, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
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

func normalizeForHTTP(endpoint string) string {
	endpoint = strings.TrimRight(endpoint, "/")
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint
	}
	return "https://" + endpoint
}

// StripScheme removes the leading http:// or https:// from an endpoint
// so it can be embedded as the host portion of an OCI reference.
// Exported because pkg/nvidia.Discovery uses the same hostname for its
// ChartRef rendering.
func StripScheme(endpoint string) string {
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(endpoint, scheme) {
			return endpoint[len(scheme):]
		}
	}
	return endpoint
}

// readAllLimited reads up to max bytes from r and returns the slice.
// Duplicates pkg/helm_oci.ReadAllLimited's contract with a chunked
// loop so pkg/oci stays free of sibling-package imports.
func readAllLimited(r interface{ Read([]byte) (int, error) }, max int64) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	for int64(len(buf)) < max {
		var tmp [4096]byte
		n, err := r.Read(tmp[:])
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf, nil
			}
			return buf, fmt.Errorf("read: %w", err)
		}
	}
	return buf, fmt.Errorf("blob exceeds %d bytes", max)
}
