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

package credcheck

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"gopkg.in/yaml.v3"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

// ChartResult describes only the resource actually checked. Success is not a
// subscription assertion, a complete chart download, or an image pull test.
type ChartResult struct {
	RepositoryURL string `json:"repositoryUrl"`
	ChartName     string `json:"chartName,omitempty"`
	Version       string `json:"version,omitempty"`
	Check         string `json:"check,omitempty"` // manifest or chartFile
	Status        string `json:"status"`
	Reason        string `json:"reason,omitempty"`
	HTTPStatus    int    `json:"httpStatus,omitempty"`
	LatencyMs     int64  `json:"latencyMs"`
}

const maxChartMetadata = 8 << 20

// ProbeChart checks an OCI chart manifest or an HTTPS Helm index plus a chart
// file's accessibility. It uses explicit credentials and CA trust, never an
// ambient Helm login, a registry-wide catalog, or an alternative public source.
func ProbeChart(ctx context.Context, repositoryURL, chartName, username, password string, caPEM []byte) (result ChartResult) {
	result = ChartResult{RepositoryURL: repositoryURL, ChartName: chartName, Status: "error"}
	start := time.Now()
	defer func() { result.LatencyMs = time.Since(start).Milliseconds() }()
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	u, err := url.Parse(repositoryURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "oci" && u.Scheme != "https") {
		result.Reason = "configuration"
		return
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	if len(caPEM) > 0 {
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(caPEM) {
			result.Reason = "tls"
			return
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	}
	httpClient := &http.Client{
		Transport: transport,
		// A redirect must not downgrade TLS or forward credentials to another
		// origin. Cross-origin redirects are allowed, though: a Helm chart or an
		// OCI blob download commonly 302s to object storage or a CDN on a
		// different host, so refusing them reports a working repository as broken.
		// The registry credential is dropped before leaving the requested origin
		// (the Go client also strips Authorization on a cross-host redirect); an
		// OCI bearer realm is handled separately by the registry protocol.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 || req.URL.Scheme != "https" {
				return errors.New("chart probe redirect downgrades TLS or exceeds the redirect limit")
			}
			if req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
	if u.Scheme == "oci" {
		err = probeOCIChart(ctx, httpClient, u, username, password, &result)
	} else {
		err = probeHTTPSChart(ctx, httpClient, u, username, password, &result)
	}
	if err != nil {
		classifyChartError(&result, err)
	} else if result.Reason == "" {
		result.Status = "ok"
	}
	return
}

func probeOCIChart(ctx context.Context, httpClient *http.Client, u *url.URL, username, password string, result *ChartResult) error {
	result.Check = "manifest"
	// Chart names are one path component. Never let a sample escape the configured
	// chart prefix, and never append the public /ai/charts path to a private mirror.
	if result.ChartName == "" || strings.ContainsAny(result.ChartName, "/\\:@?#%") || result.ChartName == "." || result.ChartName == ".." {
		result.Reason = "configuration"
		return nil
	}
	repository, err := remote.NewRepository(u.Host + strings.TrimRight(u.Path, "/") + "/" + result.ChartName)
	if err != nil {
		result.Reason = "configuration"
		return nil
	}
	repository.Client = &auth.Client{
		Client:     httpClient,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(u.Host, auth.Credential{Username: username, Password: password}),
	}
	repository.TagListPageSize = 1
	repository.MaxMetadataBytes = maxChartMetadata
	stop := errors.New("sample selected")
	err = repository.Tags(ctx, "", func(tags []string) error {
		if len(tags) > 0 {
			result.Version = tags[0]
		}
		return stop // One page only: this is a sample, not full catalog discovery.
	})
	if err != nil && !errors.Is(err, stop) {
		return err
	}
	if result.Version == "" {
		result.Status, result.Reason = "failed", "notFound"
		return nil
	}
	_, body, err := repository.FetchReference(ctx, result.Version)
	if err != nil {
		return err
	}
	defer body.Close()
	data, err := readChartMetadata(body)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		result.Reason = "invalidChart"
		return nil
	}
	var manifest ocispec.Manifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Config.MediaType != "application/vnd.cncf.helm.config.v1+json" {
		result.Reason = "invalidChart"
		return nil
	}
	for _, layer := range manifest.Layers {
		if layer.MediaType == "application/vnd.cncf.helm.chart.content.v1.tar+gzip" {
			return nil
		}
	}
	result.Reason = "invalidChart"
	return nil
}

func probeHTTPSChart(ctx context.Context, client *http.Client, u *url.URL, username, password string, result *ChartResult) error {
	result.Check = "chartFile"
	base := *u
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	indexURL := base.ResolveReference(&url.URL{Path: "index.yaml"})
	resp, err := chartRequest(ctx, client, http.MethodGet, indexURL, username, password)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if chartHTTPFailure(result, resp.StatusCode) {
		return nil
	}
	data, err := readChartMetadata(resp.Body)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var index struct {
		APIVersion string `yaml:"apiVersion"`
		Entries    map[string][]struct {
			Version string   `yaml:"version"`
			URLs    []string `yaml:"urls"`
		} `yaml:"entries"`
	}
	// Decode straight into the struct with yaml.v3. Unlike sigs.k8s.io/yaml,
	// which converts the whole document to an intermediate JSON representation,
	// this is a single pass and avoids a large transient allocation spike when a
	// public repository serves a multi-megabyte index (e.g. thousands of chart
	// versions), which could otherwise exhaust the operator's memory limit.
	if err != nil || yaml.NewDecoder(bytes.NewReader(data)).Decode(&index) != nil || index.APIVersion != "v1" {
		result.Reason = "invalidIndex"
		return nil
	}
	if result.ChartName == "" {
		names := make([]string, 0, len(index.Entries))
		for name := range index.Entries {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > 0 {
			result.ChartName = names[0]
		}
	}
	versions := index.Entries[result.ChartName]
	if len(versions) == 0 || len(versions[0].URLs) == 0 {
		result.Status, result.Reason = "failed", "notFound"
		return nil
	}
	result.Version = versions[0].Version
	ref, err := url.Parse(versions[0].URLs[0])
	if err != nil {
		result.Reason = "invalidIndex"
		return nil
	}
	chartURL := base.ResolveReference(ref)
	authenticated := username != "" || password != ""
	// The chart file must be reachable over HTTPS and carry no inline userinfo.
	// A different host is allowed only for an unauthenticated (public) repository:
	// public repos commonly serve their index from one host while hosting the
	// chart tarballs on a release host or CDN. An authenticated private mirror
	// must serve its own chart files, so a cross-host reference there is a
	// misconfiguration, and a registry credential is never sent off-origin.
	if chartURL.Scheme != "https" || chartURL.User != nil || (chartURL.Host != u.Host && authenticated) {
		result.Reason = "outsideRepository"
		return nil
	}
	// Never attach the repository credential to a different host.
	chartUser, chartPassword := username, password
	if chartURL.Host != u.Host {
		chartUser, chartPassword = "", ""
	}
	// Use GET with a one-byte Range to check the chart download permission.
	resp, err = chartRequest(ctx, client, http.MethodGet, chartURL, chartUser, chartPassword)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // Never buffer the archive, even if Range is ignored.
	chartHTTPFailure(result, resp.StatusCode)
	return nil
}

func chartRequest(ctx context.Context, client *http.Client, method string, u *url.URL, username, password string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	if method == http.MethodGet && !strings.HasSuffix(u.Path, "/index.yaml") {
		req.Header.Set("Range", "bytes=0-0")
	}
	return client.Do(req)
}

func readChartMetadata(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxChartMetadata+1))
	if len(data) > maxChartMetadata {
		return nil, fmt.Errorf("chart metadata exceeds %d bytes", maxChartMetadata)
	}
	return data, err
}

func chartHTTPFailure(result *ChartResult, status int) bool {
	if status == http.StatusOK || status == http.StatusPartialContent {
		return false
	}
	result.HTTPStatus = status
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		result.Status, result.Reason = "failed", "accessDenied"
	case http.StatusNotFound:
		result.Status, result.Reason = "failed", "notFound"
	default:
		result.Status, result.Reason = "error", "connectionFailed"
	}
	return true
}

func classifyChartError(result *ChartResult, err error) {
	var response *errcode.ErrorResponse
	var certificate *tls.CertificateVerificationError
	switch {
	case errors.As(err, &response):
		chartHTTPFailure(result, response.StatusCode)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		result.Reason = "timeout"
	case errors.As(err, &certificate):
		result.Reason = "tls"
	default:
		// Do not return remote response bodies/errors which may echo credentials.
		result.Reason = "connectionFailed"
	}
}
