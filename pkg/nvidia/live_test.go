//go:build live

// Package nvidia live tests exercise the Discovery against the real SUSE
// Registry. Excluded from the default test build by the //go:build live
// tag; run with `go test -tags=live` (or `make verify-nim-live`).
//
// Required env vars:
//   SUSE_REG_USER   — SUSE Registry username
//   SUSE_REG_TOKEN  — SUSE Registry access token
package nvidia

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLive_DiscoversNIMs_FromSUSERegistry verifies the discovery reaches
// the production SUSE Registry endpoint and surfaces at least one NIM.
// Skipped unless SUSE_REG_USER and SUSE_REG_TOKEN are both set.
func TestLive_DiscoversNIMs_FromSUSERegistry(t *testing.T) {
	user := os.Getenv("SUSE_REG_USER")
	token := os.Getenv("SUSE_REG_TOKEN")
	if user == "" || token == "" {
		t.Skip("SUSE_REG_USER and SUSE_REG_TOKEN must both be set for live test")
	}

	d := NewDiscovery(silentLogger())
	d.UpdateSettings(EngineSettings{
		RegistryEndpoint: "registry.suse.com",
		Username:         user,
		Token:            token,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("calling Discovery.Refresh against registry.suse.com…")
	if err := d.Refresh(ctx); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	entries, _ := d.Index(ctx)
	t.Logf("discovered %d NIM entries:", len(entries))
	for _, e := range entries {
		t.Logf("  %-25s  type=%-3s  chart=%s", e.ID, e.Type, e.ChartRef)
	}
	if len(entries) == 0 {
		t.Error("expected at least one NIM entry; SUSE Registry returned empty under ai/charts/nvidia/. Possible causes: credentials lack visibility, mirror process hasn't published anything, or endpoint changed.")
	}
}
