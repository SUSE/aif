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
	// MaxBytes caps the archive download.
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
		return nil, fmt.Errorf("GET %s: %s", target, resp.Status)
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
	client, err := newRegistryClient(req.Auth, req.TLS, req.PlainHTTP, false, httpClient)
	if err != nil {
		return nil, err
	}
	result, err := client.Pull(req.RepoURL + ":" + req.Version)
	if err != nil {
		return nil, fmt.Errorf("pull %s:%s: %w", req.RepoURL, req.Version, err)
	}
	// Helm's registry client reads the layer whole; enforce the cap on the result.
	if int64(len(result.Chart.Data)) > req.MaxBytes {
		return nil, fmt.Errorf("%w: pull %s:%s: chart archive exceeds %d bytes", ErrArchiveTooLarge, req.RepoURL, req.Version, req.MaxBytes)
	}
	return result.Chart.Data, nil
}
