package apps_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/SUSE/aif/pkg/apps"
	"github.com/SUSE/aif/pkg/suse_registry"
)

// staticSource is a tiny apps.Source for examples — returns a fixed
// app list, no upstream engine, no ticker. The real production
// adapters are NVIDIASource, AppCoSource, and SUSERegistrySource.
type staticSource struct {
	name string
	apps []apps.App
}

func (s *staticSource) Name() string                               { return s.name }
func (s *staticSource) List(_ context.Context) ([]apps.App, error) { return s.apps, nil }
func (s *staticSource) Refresh(_ context.Context) error            { return nil }
func (s *staticSource) UpdateSettings(_ apps.EngineSettings)       {}

// Example_catalog demonstrates the unified Apps Catalog assembling
// entries from three registered Sources, deduplicating by namespaced ID,
// and emitting a stable sort. Doubles as the contract `make
// verify-apps-mock` runs to prove the package wires together without
// hitting any live upstream.
//
// Spec hooks: ARCHITECTURE.md §5 (Apps schema), PROJECT_PLAN.md P2-3
// (Apps Catalog Manager — six design decisions), 2026-05-27 SUSE AI
// Library plan (SUSERegistrySource as the third Source).
func Example_catalog() {
	// staticSource stands in for NVIDIASource and AppCoSource so the
	// Output is deterministic; SUSERegistrySource is wired to a
	// suse_registry.FakeProvider so the real adapter is exercised.
	nvidia := &staticSource{
		name: "nvidia.ngc",
		apps: []apps.App{
			{ID: "nvidia.ngc.nim-llm:1.0.0", Source: "nvidia", Origin: "ngc", Publisher: "NVIDIA"},
			{ID: "nvidia.ngc.nim-vlm:2.0.0", Source: "nvidia", Origin: "ngc", Publisher: "NVIDIA"},
		},
	}
	appco := &staticSource{
		name: "suse.appco",
		apps: []apps.App{
			{ID: "suse.appco.ollama:0.4.1", Source: "suse", Origin: "appco", Publisher: "Ollama Inc"},
			{ID: "suse.appco.milvus:2.4.0", Source: "suse", Origin: "appco", Publisher: "Zilliz"},
		},
	}
	registryFake := &suse_registry.FakeProvider{
		Charts: map[string]suse_registry.SUSEChart{
			"example-chart:1.0.0": {
				ID:          "example-chart:1.0.0",
				Chart:       "example-chart",
				Version:     "1.0.0",
				DisplayName: "Example Chart",
				ChartRef:    "oci://registry.suse.com/ai/charts/example-chart:1.0.0",
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := apps.NewSUSERegistrySource(registryFake, logger, time.Minute)

	catalog := apps.New(logger, 10*time.Minute)
	catalog.AddSource(nvidia)
	catalog.AddSource(appco)
	catalog.AddSource(registry)

	// SUSERegistrySource is cache-only on List; Refresh primes it.
	if err := catalog.Refresh(context.Background()); err != nil {
		fmt.Println("Refresh error:", err)
		return
	}

	entries, err := catalog.List(context.Background(), apps.ListOpts{})
	if err != nil {
		fmt.Println("List error:", err)
		return
	}
	for _, a := range entries {
		fmt.Printf("%-34s  source=%-6s  origin=%s\n", a.ID, a.Source, a.Origin)
	}

	// Output:
	// nvidia.ngc.nim-llm:1.0.0            source=nvidia  origin=ngc
	// nvidia.ngc.nim-vlm:2.0.0            source=nvidia  origin=ngc
	// suse.appco.milvus:2.4.0             source=suse    origin=appco
	// suse.appco.ollama:0.4.1             source=suse    origin=appco
	// suse.registry.example-chart:1.0.0   source=suse    origin=registry
}
