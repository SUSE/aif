//go:build live

// Package source_collection live tests exercise the Client against the
// real SUSE Application Collection HTTP API. Excluded from the default
// test build by the //go:build live tag; run with `go test -tags=live`
// (or `make verify-appco-live`).
//
// Required env vars — these are *distinct* from the SUSE Registry creds
// used by pkg/nvidia's live test, even though customers often reuse the
// same value (per ARCHITECTURE.md §13.2: credentials live under
// `Settings.spec.applicationCollection.{user, token}`, separate from
// `Settings.suseRegistry.{user, token}`):
//   SUSE_APPCO_USER   — SUSE Application Collection username
//   SUSE_APPCO_TOKEN  — SUSE Application Collection access token
package source_collection

import (
	"context"
	"io"
	"log/slog"
	"os"
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

	c := NewClient(silentLogger())
	// Empty APIURL → effectiveSettings() applies the production default
	// (https://api.apps.rancher.io). Same Basic-auth flow as production.
	c.UpdateSettings(EngineSettings{
		Username: user,
		Token:    token,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("calling Client.List against api.apps.rancher.io…")
	apps, err := c.List(ctx)
	if err != nil {
		t.Fatalf("List failed (Basic auth or catalog walk broken): %v", err)
	}

	t.Logf("Basic auth succeeded; discovered %d HELM_CHART applications:", len(apps))
	for _, a := range apps {
		t.Logf("  %-30s  publisher=%-25s  latest=%s", a.ID, a.Publisher, a.LatestVersion)
	}
	if len(apps) == 0 {
		t.Log("note: zero apps came back — auth handshake still validated, but the upstream catalog may be empty under the configured filter.")
	}
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
