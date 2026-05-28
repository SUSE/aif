//go:build live

// Package suse_registry live test exercises the Provider against the real
// registry.suse.com. Excluded from the default test build by the //go:build
// live tag; run with `go test -tags=live` (or `make verify-suse-registry-live`).
//
// Required env vars (same creds as verify-nim-live):
//
//	SUSE_REG_USER   — SUSE Registry username
//	SUSE_REG_TOKEN  — SUSE Registry access token
package suse_registry

import (
	"context"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/SUSE/aif/pkg/oci"
)

// TestLive_EnumeratesSUSECharts verifies the provider reaches the
// production SUSE Registry endpoint, completes the OCI Bearer handshake,
// and enumerates charts under ai/charts/* excluding nvidia/. The count is
// informational — what we assert is the round-trip completes without error.
func TestLive_EnumeratesSUSECharts(t *testing.T) {
	user := os.Getenv("SUSE_REG_USER")
	token := os.Getenv("SUSE_REG_TOKEN")
	if user == "" || token == "" {
		t.Skip("SUSE_REG_USER and SUSE_REG_TOKEN must both be set for live test")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	walker := oci.NewWalker(logger)
	annR := oci.NewAnnotationReader(logger, walker)
	p := NewProvider(logger, walker, annR)
	p.UpdateSettings(EngineSettings{
		RegistryEndpoint: "registry.suse.com",
		Username:         user,
		Token:            token,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	t.Log("calling Provider.Refresh against registry.suse.com…")
	if err := p.Refresh(ctx); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	entries, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	t.Logf("Bearer exchange succeeded; discovered %d SUSE charts under ai/charts/ (excluding nvidia/):", len(entries))

	if len(entries) == 0 {
		t.Fatal("no entries returned; broken Refresh or empty registry")
	}

	seenCharts := make(map[string]string)
	for _, e := range entries {
		t.Logf("  %-40s  display=%s", e.ID, e.DisplayName)

		// (a) No cosign-shaped IDs.
		if isSigstoreLikeID(e.ID) {
			t.Errorf("sigstore manifest leaked into catalog: %q", e.ID)
		}

		// (b) Each chart appears at most once.
		if prev, ok := seenCharts[e.Chart]; ok {
			t.Errorf("chart %q appears multiple times: %q and %q", e.Chart, prev, e.ID)
		}
		seenCharts[e.Chart] = e.ID
	}
}

// sigstoreVersionPattern mirrors pkg/oci's production tag filter exactly
// (lowercase hex, 64 chars). Duplicated rather than exported so the
// production package's surface stays minimal for a single test assertion.
var sigstoreVersionPattern = regexp.MustCompile(`^sha256-[a-f0-9]{64}\.(sig|att|sbom)$`)

// isSigstoreLikeID returns true when an ID's version segment matches
// the cosign manifest tag shape that pkg/oci.Walker is supposed to filter.
func isSigstoreLikeID(id string) bool {
	i := strings.LastIndexByte(id, ':')
	if i < 0 {
		return false
	}
	return sigstoreVersionPattern.MatchString(id[i+1:])
}
