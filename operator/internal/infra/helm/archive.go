/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helm

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

// ChartArchiveRequest identifies one chart version in a Helm HTTP repository
// or an OCI registry. Credentials and TLS are in-memory only.
type ChartArchiveRequest struct {
	// RepoURL is the https:// repository root, or the oci:// chart reference.
	RepoURL string
	// ChartName is looked up in the repository's index.yaml (HTTP repos only).
	ChartName string
	Version   string
	Auth      *RegistryAuth
	TLS       *tls.Config
	// PlainHTTP talks to an OCI registry without TLS (ClusterRepo insecurePlainHttp).
	PlainHTTP bool
	// MaxBytes caps the archive download: the HTTP archive body, or each OCI
	// registry response (manifest, config, chart layer) while it streams, so
	// an oversized layer is refused before Helm buffers it whole.
	MaxBytes int64
	// MaxIndexBytes caps index.yaml, which for large public repos is far bigger
	// than any one chart. 0 means MaxBytes.
	MaxIndexBytes int64
	// Timeout bounds the whole pull. 0 means defaultPullTimeout.
	Timeout time.Duration
}

// defaultPullTimeout bounds a chart pull so a stalled repository cannot hold a
// reconcile worker indefinitely.
const defaultPullTimeout = 2 * time.Minute

// ErrArchiveTooLarge marks a chart archive over ChartArchiveRequest.MaxBytes.
var ErrArchiveTooLarge = errors.New("chart archive too large")

// ErrIndexTooLarge marks a repository index.yaml over ChartArchiveRequest.MaxIndexBytes.
var ErrIndexTooLarge = errors.New("repository index too large")

// PullChartArchive downloads the .tgz for one chart version, entirely in
// memory, the way Fleet's own chart download would: an HTTP repository is
// resolved through its index.yaml, an OCI reference is pulled by tag.
func PullChartArchive(ctx context.Context, req ChartArchiveRequest) ([]byte, error) {
	if req.MaxBytes <= 0 {
		return nil, errors.New("chart archive pull: MaxBytes must be positive")
	}
	if req.MaxIndexBytes <= 0 {
		req.MaxIndexBytes = req.MaxBytes
	}
	if req.Timeout <= 0 {
		req.Timeout = defaultPullTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	httpClient := &http.Client{
		Transport: defaultTransportClone(req.TLS),
		Timeout:   req.Timeout,
		// Go drops Authorization on a redirect to another host, but not on a
		// same-host downgrade to plain HTTP; refuse that outright.
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if via[len(via)-1].URL.Scheme == "https" && next.URL.Scheme == "http" {
				return fmt.Errorf("refusing redirect from https to http (%s)", next.URL.Host)
			}
			return nil
		},
	}
	if strings.HasPrefix(req.RepoURL, ociSchemePrefix) {
		return pullOCIArchive(req, httpClient)
	}
	return pullHTTPArchive(ctx, req, httpClient)
}

func pullHTTPArchive(ctx context.Context, req ChartArchiveRequest, httpClient *http.Client) ([]byte, error) {
	base := strings.TrimSuffix(req.RepoURL, "/")
	repoURL, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse repository URL %q: %w", req.RepoURL, err)
	}
	indexData, err := httpGetLimited(ctx, httpClient, base+"/index.yaml", req.Auth, req.MaxIndexBytes, ErrIndexTooLarge)
	if err != nil {
		return nil, fmt.Errorf("read repository index: %w", err)
	}
	index := &repo.IndexFile{}
	if err := yaml.Unmarshal(indexData, index); err != nil {
		return nil, fmt.Errorf("parse repository index: %w", err)
	}
	index.SortEntries()
	cv, err := index.Get(req.ChartName, req.Version)
	if err != nil || len(cv.URLs) == 0 {
		return nil, fmt.Errorf("chart %s version %s not found in repository index", req.ChartName, req.Version)
	}
	archiveURL, err := repo.ResolveReferenceURL(base, cv.URLs[0])
	if err != nil {
		return nil, fmt.Errorf("resolve archive URL %q: %w", cv.URLs[0], err)
	}

	// Credentials go to the repository's own host only, like Helm's default
	// (pass-credentials off): an index may point archives at a CDN or mirror.
	auth := req.Auth
	if u, err := url.Parse(archiveURL); err != nil || u.Scheme != repoURL.Scheme || u.Host != repoURL.Host {
		auth = nil
	}
	archive, err := httpGetLimited(ctx, httpClient, archiveURL, auth, req.MaxBytes, ErrArchiveTooLarge)
	if err != nil {
		return nil, fmt.Errorf("download chart archive: %w", err)
	}
	return archive, nil
}

// HTTPStatusError is a non-200 response to an index or archive download.
type HTTPStatusError struct {
	URL    string
	Code   int
	Status string
}

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("GET %s: %s", e.URL, e.Status) }

// httpGetLimited GETs target and returns at most limit bytes of a 200 body; a
// longer body is an error, wrapping tooLarge when given.
func httpGetLimited(ctx context.Context, c *http.Client, target string, auth *RegistryAuth, limit int64, tooLarge error) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	if auth != nil {
		httpReq.SetBasicAuth(auth.Username, auth.Password)
	}
	resp, err := c.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{URL: target, Code: resp.StatusCode, Status: resp.Status}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		if tooLarge != nil {
			return nil, fmt.Errorf("%w: GET %s: body exceeds %d bytes", tooLarge, target, limit)
		}
		return nil, fmt.Errorf("GET %s: body exceeds %d bytes", target, limit)
	}
	return data, nil
}

func pullOCIArchive(req ChartArchiveRequest, httpClient *http.Client) ([]byte, error) {
	capped := &cappedTransport{base: httpClient.Transport, limit: req.MaxBytes}
	limited := *httpClient
	limited.Transport = capped
	client, err := newRegistryClient(req.Auth, req.TLS, req.PlainHTTP, false, &limited)
	if err != nil {
		return nil, err
	}
	result, err := client.Pull(req.RepoURL + ":" + req.Version)
	if capped.exceeded.Load() {
		return nil, fmt.Errorf("%w: pull %s:%s: a registry response exceeds %d bytes", ErrArchiveTooLarge, req.RepoURL, req.Version, req.MaxBytes)
	}
	if err != nil {
		return nil, fmt.Errorf("pull %s:%s: %w", req.RepoURL, req.Version, err)
	}
	if int64(len(result.Chart.Data)) > req.MaxBytes {
		return nil, fmt.Errorf("%w: pull %s:%s: chart archive exceeds %d bytes", ErrArchiveTooLarge, req.RepoURL, req.Version, req.MaxBytes)
	}
	return result.Chart.Data, nil
}

// cappedTransport fails every response whose body is over limit bytes,
// refusing a declared Content-Length up front and cutting the stream
// otherwise. Helm's registry client reads each blob whole, so this is what
// bounds the memory an OCI pull can use.
type cappedTransport struct {
	base     http.RoundTripper
	limit    int64
	exceeded atomic.Bool
}

func (t *cappedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > t.limit {
		_ = resp.Body.Close()
		t.exceeded.Store(true)
		return nil, fmt.Errorf("%w: %s declares %d bytes", ErrArchiveTooLarge, req.URL.Redacted(), resp.ContentLength)
	}
	resp.Body = &cappedBody{ReadCloser: resp.Body, remaining: t.limit, t: t}
	return resp, nil
}

type cappedBody struct {
	io.ReadCloser
	remaining int64
	t         *cappedTransport
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		var probe [1]byte
		n, err := b.ReadCloser.Read(probe[:])
		if n > 0 {
			b.t.exceeded.Store(true)
			return 0, ErrArchiveTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}
