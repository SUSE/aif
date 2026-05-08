//go:build live

// Package apps live tests exercise the full Catalog assembled from
// real NVIDIASource (wrapping a real nvidia.Discovery against
// registry.suse.com) and AppCoSource (wrapping a real
// source_collection.Client against api.apps.rancher.io). Excluded
// from the default test build by the //go:build live tag; run with
// `go test -tags=live` (or `make verify-apps-live`).
//
// Required env vars (same names used by the per-package live tests in
// pkg/nvidia and pkg/source_collection — distinct because the two
// upstream services have distinct credentials per ARCHITECTURE.md
// §13.2):
//   SUSE_REG_USER     SUSE_REG_TOKEN     — SUSE Registry creds
//   SUSE_APPCO_USER   SUSE_APPCO_TOKEN   — SUSE Application Collection creds
package apps

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/SUSE/aif/pkg/nvidia"
	"github.com/SUSE/aif/pkg/source_collection"
)

// TestLive_Catalog_AssemblesFromBothUpstreams verifies the full Option B
// hexagonal stack works against both real upstreams. Asserts only that
// Refresh + List complete without error; per-Source entry counts are
// reported informationally so the test is robust to upstream catalog
// drift (matches the discipline used by pkg/nvidia and
// pkg/source_collection live tests).
//
// Skipped unless all four credential env vars are set.
func TestLive_Catalog_AssemblesFromBothUpstreams(t *testing.T) {
	regUser := os.Getenv("SUSE_REG_USER")
	regToken := os.Getenv("SUSE_REG_TOKEN")
	appcoUser := os.Getenv("SUSE_APPCO_USER")
	appcoToken := os.Getenv("SUSE_APPCO_TOKEN")
	if regUser == "" || regToken == "" || appcoUser == "" || appcoToken == "" {
		t.Skip("SUSE_REG_USER/TOKEN and SUSE_APPCO_USER/TOKEN must all be set for live test")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Build the engines.
	nvDiscovery := nvidia.NewDiscovery(logger)
	nvDiscovery.UpdateSettings(nvidia.EngineSettings{
		RegistryEndpoint: "registry.suse.com",
		Username:         regUser,
		Token:            regToken,
	})

	appcoClient := source_collection.NewClient(logger)
	appcoClient.UpdateSettings(source_collection.EngineSettings{
		Username: appcoUser,
		Token:    appcoToken,
	})

	// Build Catalog and register both adapters.
	catalog := New(logger, 10*time.Minute).(*catalogImpl)
	catalog.AddSource(NewNVIDIASource(nvDiscovery, logger, 10*time.Minute))
	catalog.AddSource(NewAppCoSource(appcoClient, logger, 10*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Log("calling Catalog.Refresh against registry.suse.com + api.apps.rancher.io…")
	if err := catalog.Refresh(ctx); err != nil {
		t.Fatalf("Refresh failed (one or more upstream auth/walks broken): %v", err)
	}

	apps, err := catalog.List(ctx, ListOpts{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Count per source for informational logging.
	var nvCount, suseCount int
	for _, a := range apps {
		switch a.Source {
		case "nvidia":
			nvCount++
		case "suse":
			suseCount++
		}
	}
	t.Logf("Catalog assembled: total=%d  nvidia=%d  suse=%d", len(apps), nvCount, suseCount)
	for _, a := range apps {
		t.Logf("  %-40s  source=%-6s  publisher=%s", a.ID, a.Source, a.Publisher)
	}

	// Source-filter sanity check: ListOpts.Source="nvidia" returns only
	// nvidia entries.
	nvOnly, err := catalog.List(ctx, ListOpts{Source: "nvidia"})
	if err != nil {
		t.Fatalf("List(source=nvidia): %v", err)
	}
	for _, a := range nvOnly {
		if a.Source != "nvidia" {
			t.Errorf("ListOpts{Source:nvidia} returned non-nvidia App: %+v", a)
		}
	}

	if len(apps) == 0 {
		t.Log("note: zero entries is currently expected when both mirror prefixes are empty; the auth handshakes still completed successfully.")
	}
}
