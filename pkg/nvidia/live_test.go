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
// the production SUSE Registry endpoint and that the OCI Bearer-token
// exchange succeeds. The test passes whenever Refresh + Index complete
// without error; the count of entries is reported informationally.
//
// Why no `≥1` assertion: as of this writing the SUSE-managed mirror has
// not yet published any charts under `ai/charts/nvidia/` (only
// `ai/containers/...` are present). When the mirror does publish charts,
// they will appear in the t.Logf list below — but a zero count today is
// the expected steady state, not a failure.
//
// Skipped unless SUSE_REG_USER and SUSE_REG_TOKEN are both set.
func TestLive_DiscoversNIMs_FromSUSERegistry(t *testing.T) {
	user := os.Getenv("SUSE_REG_USER")
	token := os.Getenv("SUSE_REG_TOKEN")
	if user == "" || token == "" {
		t.Skip("SUSE_REG_USER and SUSE_REG_TOKEN must both be set for live test")
	}

	d, ar := NewDiscovery(silentLogger())
	d.UpdateSettings(EngineSettings{
		RegistryEndpoint: "registry.suse.com",
		Username:         user,
		Token:            token,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("calling Discovery.Refresh against registry.suse.com…")
	if err := d.Refresh(ctx); err != nil {
		t.Fatalf("Refresh failed (Bearer-token exchange or catalog walk broken): %v", err)
	}

	entries, err := d.Index(ctx)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	t.Logf("Bearer exchange succeeded; discovered %d NIM entries under ai/charts/nvidia/:", len(entries))
	for _, e := range entries {
		t.Logf("  %-25s  type=%-3s  chart=%s", e.ID, e.Type, e.ChartRef)
	}
	if len(entries) == 0 {
		t.Log("note: zero entries is currently expected — the SUSE-managed mirror publishes only ai/containers/... today; charts will land later.")
	}

	// Exercise the AnnotationReader handshake — pick the first entry (if any)
	// and fetch its annotations. We don't assert on contents because the
	// SUSE-managed mirror has not yet published charts under ai/charts/nvidia/
	// (per P2-1 caveat). Successful auth + fetch is the assertion.
	if len(entries) > 0 {
		first := entries[0]
		ann, err := ar.ChartAnnotations(ctx, first.Chart, first.Version)
		if err != nil {
			t.Fatalf("ChartAnnotations(%s, %s): %v", first.Chart, first.Version, err)
		}
		t.Logf("annotations for %s:%s = %v", first.Chart, first.Version, ann)
	}
}
