package apps_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/SUSE/aif/pkg/apps"
)

// staticSource is a tiny apps.Source for examples — returns a fixed
// app list, no upstream engine, no ticker. The real production
// adapters are NVIDIASource and AppCoSource.
type staticSource struct {
	name string
	apps []apps.App
}

func (s *staticSource) Name() string                                 { return s.name }
func (s *staticSource) List(_ context.Context) ([]apps.App, error)   { return s.apps, nil }
func (s *staticSource) Refresh(_ context.Context) error              { return nil }
func (s *staticSource) UpdateSettings(_ apps.EngineSettings)         {}

// Example_catalog demonstrates the unified Apps Catalog assembling
// entries from two registered Sources, deduplicating by namespaced ID,
// and emitting a stable sort. Doubles as the contract `make
// verify-apps-mock` runs to prove the package wires together without
// hitting any live upstream.
//
// Spec hooks: ARCHITECTURE.md §5 (Apps schema), PROJECT_PLAN.md P2-3
// (Apps Catalog Manager — six design decisions).
func Example_catalog() {
	// 1. Two Sources: an NVIDIA-flavoured one and a SUSE-flavoured one.
	//    In production these would be NVIDIASource / AppCoSource wrapping
	//    the engine packages; the example uses inline static data so the
	//    Output is deterministic.
	nvidia := &staticSource{
		name: "nvidia",
		apps: []apps.App{
			{ID: "nvidia.nim-llm:1.0.0", Source: "nvidia", Publisher: "NVIDIA"},
			{ID: "nvidia.nim-vlm:2.0.0", Source: "nvidia", Publisher: "NVIDIA"},
		},
	}
	suse := &staticSource{
		name: "suse",
		apps: []apps.App{
			{ID: "suse.ollama:0.4.1", Source: "suse", Publisher: "Ollama Inc"},
			{ID: "suse.milvus:2.4.0", Source: "suse", Publisher: "Zilliz"},
		},
	}

	// 2. Build the Aggregator and register the Sources via AddSource
	//    (the registry pattern from decision (d): New returns an empty
	//    Aggregator, sources are added imperatively at bootstrap).
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	catalog := apps.New(logger, 10*time.Minute)
	catalog.AddSource(nvidia)
	catalog.AddSource(suse)

	// 3. List with no filter → fan-out, dedupe by ID, sort by ID.
	entries, err := catalog.List(context.Background(), apps.ListOpts{})
	if err != nil {
		fmt.Println("List error:", err)
		return
	}
	for _, a := range entries {
		fmt.Printf("%-22s  source=%-6s  publisher=%s\n", a.ID, a.Source, a.Publisher)
	}

	// Output:
	// nvidia.nim-llm:1.0.0    source=nvidia  publisher=NVIDIA
	// nvidia.nim-vlm:2.0.0    source=nvidia  publisher=NVIDIA
	// suse.milvus:2.4.0       source=suse    publisher=Zilliz
	// suse.ollama:0.4.1       source=suse    publisher=Ollama Inc
}
