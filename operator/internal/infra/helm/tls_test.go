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
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
)

func TestDefaultTransportClone(t *testing.T) {
	cfg := &tls.Config{InsecureSkipVerify: true}
	tr := defaultTransportClone(cfg)
	if tr == nil || tr.TLSClientConfig != cfg {
		t.Fatalf("expected cloned transport carrying our TLS config")
	}
	if tr == http.DefaultTransport {
		t.Fatalf("must not return the shared http.DefaultTransport")
	}
}

func TestOCIRegistryClient_NoAuthNoTLSReturnsDefault(t *testing.T) {
	def := &registry.Client{}
	c := &helmClient{settings: cli.New(), registry: def}
	got, err := c.ociRegistryClient(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != def {
		t.Fatalf("expected default client when no auth/tls")
	}
}

func TestOCIRegistryClient_WithTLSBuildsFresh(t *testing.T) {
	def := &registry.Client{}
	c := &helmClient{settings: cli.New(), registry: def}
	got, err := c.ociRegistryClient(nil, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got == def {
		t.Fatalf("expected a fresh client when tls set")
	}
}

func TestOCIRegistryClient_WithAuthBuildsFresh(t *testing.T) {
	def := &registry.Client{}
	c := &helmClient{settings: cli.New(), registry: def}
	got, err := c.ociRegistryClient(&RegistryAuth{Username: "u", Password: "p"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got == def {
		t.Fatalf("expected a fresh client when auth set")
	}
}

// TestLoadChartHTTPSWithTLS_Handshake proves the in-memory TLS transport is honored:
// trusting the server cert (or skip-verify) gets PAST the handshake and fails later at
// chart-load; an untrusting pool fails AT the handshake with a certificate error.
func TestLoadChartHTTPSWithTLS_Handshake(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-a-real-chart"))
	}))
	defer srv.Close()
	c := &helmClient{}

	isTLSErr := func(err error) bool {
		s := strings.ToLower(err.Error())
		return strings.Contains(s, "certificate") || strings.Contains(s, "x509") || strings.Contains(s, "tls")
	}

	// Trust the server cert → handshake OK → error is a chart-load error, not TLS.
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	_, err := c.loadChartHTTPSWithTLS(srv.URL+"/x.tgz", nil, &tls.Config{RootCAs: pool})
	if err == nil {
		t.Fatal("expected a chart-load error for a non-chart body")
	}
	if isTLSErr(err) {
		t.Fatalf("unexpected TLS error when trusting the server cert: %v", err)
	}

	// Empty pool → TLS verification must fail.
	_, err = c.loadChartHTTPSWithTLS(srv.URL+"/x.tgz", nil, &tls.Config{RootCAs: x509.NewCertPool()})
	if err == nil || !isTLSErr(err) {
		t.Fatalf("expected a TLS certificate error with an empty CA pool, got: %v", err)
	}

	// Skip-verify → handshake OK again → chart-load error, not TLS.
	_, err = c.loadChartHTTPSWithTLS(srv.URL+"/x.tgz", nil, &tls.Config{InsecureSkipVerify: true})
	if err == nil {
		t.Fatal("expected a chart-load error with skip-verify")
	}
	if isTLSErr(err) {
		t.Fatalf("unexpected TLS error with skip-verify: %v", err)
	}
}
