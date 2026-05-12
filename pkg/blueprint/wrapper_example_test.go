package blueprint_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"

	"github.com/SUSE/aif/pkg/apps"
	"github.com/SUSE/aif/pkg/blueprint"
)

// staticCatalog is a minimal apps.Catalog for examples.
type staticCatalog struct {
	entries []apps.App
}

func (c *staticCatalog) List(_ context.Context, opts apps.ListOpts) ([]apps.App, error) {
	var out []apps.App
	for _, a := range c.entries {
		if !opts.IncludeReferenceBlueprints && a.ReferenceBlueprint {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}
func (c *staticCatalog) Get(_ context.Context, _ string) (apps.App, error) { return apps.App{}, nil }
func (c *staticCatalog) Refresh(_ context.Context) error                   { return nil }
func (c *staticCatalog) UpdateSettings(_ apps.EngineSettings)              {}

// noopEmitter discards events for the example.
type noopEmitter struct{}

func (e *noopEmitter) BlueprintWrappedFromVendorChart(_ blueprint.Blueprint) {}
func (e *noopEmitter) BlueprintWithdrawn(_ blueprint.Blueprint)              {}

// Example_wrapper demonstrates the Wrapper creating Blueprint CRs from
// a catalog containing a mix of regular Apps and Reference Blueprints.
// Non-RB apps are ignored; non-semver versions are skipped. Doubles as
// `make verify-wrapper-mock`.
func Example_wrapper() {
	catalog := &staticCatalog{
		entries: []apps.App{
			{
				ID: "nvidia.nim-llm:1.0.0", Name: "nim-llm", Source: "nvidia",
				Version: "1.0.0", ReferenceBlueprint: true, UseCase: "inference",
				ChartRef: apps.ChartRef{Repo: "oci://registry.suse.com/ai/charts/nvidia", Chart: "nim-llm", Version: "1.0.0"},
			},
			{
				ID: "nvidia.nim-vlm:2.0.0", Name: "nim-vlm", Source: "nvidia",
				Version: "2.0.0", ReferenceBlueprint: false,
				ChartRef: apps.ChartRef{Repo: "oci://registry.suse.com/ai/charts/nvidia", Chart: "nim-vlm", Version: "2.0.0"},
			},
			{
				ID: "suse.rag:0.1.0-rc.1", Name: "rag", Source: "suse",
				Version: "0.1.0-rc.1", ReferenceBlueprint: true, UseCase: "rag",
				ChartRef: apps.ChartRef{Repo: "oci://registry.suse.com/ai/charts/suse", Chart: "rag", Version: "0.1.0-rc.1"},
			},
			{
				ID: "suse.ollama:0.4.1", Name: "ollama", Source: "suse",
				Version: "0.4.1", ReferenceBlueprint: false,
				ChartRef: apps.ChartRef{Repo: "oci://registry.suse.com/ai/charts/suse", Chart: "ollama", Version: "0.4.1"},
			},
		},
	}

	store := blueprint.NewFakeRepository()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := blueprint.NewWrapper(catalog, store, &noopEmitter{}, logger)

	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		fmt.Println("error:", err)
		return
	}

	bps, _ := store.ListWrapped(context.Background())
	sort.Slice(bps, func(i, j int) bool { return bps[i].Name < bps[j].Name })
	for _, bp := range bps {
		fmt.Printf("%-30s  lineage=%-16s  version=%-12s  source=%s\n",
			bp.Name, bp.Lineage, bp.Version, bp.Source.Type)
	}

	// Output:
	// nvidia-nim-llm.1.0.0            lineage=nvidia-nim-llm    version=1.0.0         source=WrapsVendorChart
	// suse-rag.0.1.0-rc.1             lineage=suse-rag          version=0.1.0-rc.1    source=WrapsVendorChart
}
