//go:build live

// Package source_collection live tests exercise the Client against the
// real SUSE Application Collection HTTP API. Excluded from the default
// test build by the //go:build live tag; run with `go test -tags=live`
// (or `make verify-appco-live`).
//
// Required env vars — these are *distinct* from the SUSE Registry creds
// used by pkg/nvidia's live test (per ARCHITECTURE.md §13.2: credentials
// live under `Settings.spec.applicationCollection.{user, token}`, separate
// from `Settings.suseRegistry.{user, token}`):
//   SUSE_APPCO_USER     — SUSE Application Collection username
//   SUSE_APPCO_TOKEN    — SUSE Application Collection access token
//
// Optional:
//   SUSE_APPCO_API_URL  — overrides the production default
//                         (https://api.apps.rancher.io)
//   SUSE_APPCO_OCI_HOST — when set, also exercises the AnnotationReader
//                         against the OCI host
package source_collection

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLive_ListsCatalog_FromApplicationCollection verifies the client
// reaches the production api.apps.rancher.io endpoint and that HTTP
// Basic authentication succeeds. The test passes whenever List returns
// without error; the count of entries is reported informationally so
// the test is robust to upstream catalog changes.
//
// Skipped unless SUSE_APPCO_USER and SUSE_APPCO_TOKEN are both set.
func TestLive_ListsCatalog_FromApplicationCollection(t *testing.T) {
	user := os.Getenv("SUSE_APPCO_USER")
	token := os.Getenv("SUSE_APPCO_TOKEN")
	if user == "" || token == "" {
		t.Skip("SUSE_APPCO_USER and SUSE_APPCO_TOKEN must both be set for live test")
	}

	apiURL := os.Getenv("SUSE_APPCO_API_URL")
	if apiURL == "" {
		apiURL = "https://api.apps.rancher.io"
	}

	c, ar := NewClient(silentLogger())
	c.UpdateSettings(EngineSettings{
		APIURL:   apiURL,
		Username: user,
		Token:    token,
		OCIHost:  os.Getenv("SUSE_APPCO_OCI_HOST"), // optional; empty → ChartAnnotations returns ErrNotConfigured (skipped below)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("calling Client.List against api.apps.rancher.io…")
	apps, err := c.List(ctx)
	if err != nil {
		t.Fatalf("List failed (Basic auth or catalog walk broken): %v", err)
	}

	t.Logf("Basic auth succeeded; discovered %d HELM_CHART applications:", len(apps))
	withTimestamp := 0
	for _, a := range apps {
		t.Logf("  %-30s  publisher=%-25s  latest=%s", a.ID, a.Publisher, a.LatestVersion)
		if a.LastUpdatedAt != "" {
			withTimestamp++
		}
	}
	if len(apps) == 0 {
		t.Log("note: zero apps came back — auth handshake still validated, but the upstream catalog may be empty under the configured filter.")
	}
	t.Logf("%d/%d apps have LastUpdatedAt", withTimestamp, len(apps))
	if len(apps) > 0 && withTimestamp == 0 {
		t.Errorf("no apps have LastUpdatedAt — upstream may have renamed last_updated_at")
	}

	// Exercise the AnnotationReader handshake — pick the first chart (if any)
	// and fetch its annotations. Gated on SUSE_APPCO_OCI_HOST being set,
	// since OCI access is independent from the HTTP API auth above.
	ociHost := os.Getenv("SUSE_APPCO_OCI_HOST")
	if len(apps) > 0 && ociHost != "" {
		first := apps[0]
		repo, chart := splitAppCoChartRef(first.ChartRef, ociHost, first.LatestVersion)
		if chart == "" {
			t.Logf("could not parse chart ref %q against OCIHost %q; skipping annotation fetch", first.ChartRef, ociHost)
		} else {
			ann, err := ar.ChartAnnotations(ctx, repo, chart, first.LatestVersion)
			if err != nil {
				t.Fatalf("ChartAnnotations(%s/%s:%s): %v", repo, chart, first.LatestVersion, err)
			}
			t.Logf("annotations for %s/%s:%s = %v", repo, chart, first.LatestVersion, ann)
		}
	}
}

// splitAppCoChartRef parses an "oci://<host>/<repo>/<chart>:<version>"
// reference relative to the configured OCIHost into (repo, chart). Best
// effort — returns ("", "") if the ref doesn't match the expected shape.
func splitAppCoChartRef(chartRef, ociHost, version string) (string, string) {
	prefix := "oci://" + strings.TrimPrefix(strings.TrimPrefix(ociHost, "https://"), "http://") + "/"
	rest := strings.TrimPrefix(chartRef, prefix)
	if rest == chartRef {
		return "", ""
	}
	rest = strings.TrimSuffix(rest, ":"+version)
	idx := strings.LastIndex(rest, "/")
	if idx < 0 {
		return "", ""
	}
	return rest[:idx], rest[idx+1:]
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
