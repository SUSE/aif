//go:build live

package blueprint

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/SUSE/aif/pkg/apps"
	"github.com/SUSE/aif/pkg/nvidia"
	"github.com/SUSE/aif/pkg/source_collection"
)

// TestWrapperLive_DryRun exercises the wrapper against the real
// registry.suse.com and api.apps.rancher.io upstreams. The wrapper runs
// in dry-run mode: it creates Blueprints in a FakeRepository (no K8s
// cluster needed) and logs the results.
//
// Skipped unless all four credential env vars are set.
func TestWrapperLive_DryRun(t *testing.T) {
	regUser := os.Getenv("SUSE_REG_USER")
	regToken := os.Getenv("SUSE_REG_TOKEN")
	appcoUser := os.Getenv("SUSE_APPCO_USER")
	appcoToken := os.Getenv("SUSE_APPCO_TOKEN")
	if regUser == "" || regToken == "" || appcoUser == "" || appcoToken == "" {
		t.Skip("SUSE_REG_USER/TOKEN and SUSE_APPCO_USER/TOKEN must all be set for live test")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	nvDiscovery, nvAnn := nvidia.NewDiscovery(logger)
	nvDiscovery.UpdateSettings(nvidia.EngineSettings{
		RegistryEndpoint: "registry.suse.com",
		Username:         regUser,
		Token:            regToken,
	})

	appcoClient, appcoAnn := source_collection.NewClient(logger)
	appcoClient.UpdateSettings(source_collection.EngineSettings{
		Username: appcoUser,
		Token:    appcoToken,
	})

	catalog := apps.New(logger, 10*time.Minute)
	catalog.AddSource(apps.NewNVIDIASource(nvDiscovery, nvAnn, logger, 10*time.Minute))
	catalog.AddSource(apps.NewAppCoSource(appcoClient, appcoAnn, logger, 10*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Log("refreshing catalog against real upstreams...")
	if err := catalog.Refresh(ctx); err != nil {
		t.Fatalf("Catalog.Refresh: %v", err)
	}

	store := NewFakeRepository()
	emitter := &liveEmitter{t: t}
	w := NewWrapper(catalog, store, emitter, logger)

	t.Log("running WrapDetectedCharts (dry-run against FakeRepository)...")
	if err := w.WrapDetectedCharts(ctx); err != nil {
		t.Fatalf("WrapDetectedCharts: %v", err)
	}

	bps, _ := store.ListWrapped(ctx)
	t.Logf("wrapper created %d Blueprint CRs", len(bps))
	for _, bp := range bps {
		t.Logf("  %-40s  lineage=%-20s  version=%s", bp.Name, bp.Lineage, bp.Version)
	}
	if len(bps) == 0 {
		t.Log("note: 0 RBs is currently expected — upstream mirrors may not yet carry annotated charts")
	}
}

type liveEmitter struct{ t *testing.T }

func (e *liveEmitter) BlueprintWrappedFromVendorChart(bp Blueprint) {
	e.t.Logf("EVENT: BlueprintWrappedFromVendorChart %s", bp.Name)
}
func (e *liveEmitter) BlueprintWithdrawn(bp Blueprint) {
	e.t.Logf("EVENT: BlueprintWithdrawn %s", bp.Name)
}
