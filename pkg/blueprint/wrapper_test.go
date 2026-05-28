package blueprint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/SUSE/aif/pkg/apps"
)

// fakeCatalog implements apps.Catalog for wrapper tests.
type fakeCatalog struct {
	apps []apps.App
	err  error
}

func (c *fakeCatalog) List(_ context.Context, opts apps.ListOpts) ([]apps.App, error) {
	if c.err != nil {
		return nil, c.err
	}
	var out []apps.App
	for _, a := range c.apps {
		if !opts.IncludeReferenceBlueprints && a.ReferenceBlueprint {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}
func (c *fakeCatalog) Get(_ context.Context, _ string) (apps.App, error) {
	return apps.App{}, nil
}
func (c *fakeCatalog) Refresh(_ context.Context) error     { return nil }
func (c *fakeCatalog) UpdateSettings(_ apps.EngineSettings) {}

// fakeEventEmitter records event calls for assertions.
type fakeEventEmitter struct {
	wrapped   []Blueprint
	withdrawn []Blueprint
}

func (e *fakeEventEmitter) BlueprintWrappedFromVendorChart(bp Blueprint) {
	e.wrapped = append(e.wrapped, bp)
}
func (e *fakeEventEmitter) BlueprintWithdrawn(bp Blueprint) {
	e.withdrawn = append(e.withdrawn, bp)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func rbApp(source, chart, version string) apps.App {
	return apps.App{
		ID:                 source + "." + chart + ":" + version,
		Name:               chart,
		Source:             source,
		Version:            version,
		ReferenceBlueprint: true,
		UseCase:            "inference",
		ChartRef: apps.ChartRef{
			Repo:    "oci://registry.suse.com/ai/charts/" + source,
			Chart:   chart,
			Version: version,
		},
	}
}

func TestWrapper_CreatesBlueprint(t *testing.T) {
	store := NewFakeRepository()
	emitter := &fakeEventEmitter{}
	catalog := &fakeCatalog{apps: []apps.App{rbApp("nvidia", "nim-llm", "1.0.0")}}
	w := NewWrapper(catalog, store, emitter, discardLogger())

	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		t.Fatalf("WrapDetectedCharts: %v", err)
	}

	bps, _ := store.ListWrapped(context.Background())
	if len(bps) != 1 {
		t.Fatalf("got %d blueprints, want 1", len(bps))
	}
	bp := bps[0]
	if bp.Name != "nvidia-nim-llm.1.0.0" {
		t.Errorf("name = %q, want %q", bp.Name, "nvidia-nim-llm.1.0.0")
	}
	if bp.Lineage != "nvidia-nim-llm" {
		t.Errorf("lineage = %q, want %q", bp.Lineage, "nvidia-nim-llm")
	}
	if bp.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", bp.Version, "1.0.0")
	}
	if bp.Source.Type != SourceTypeWrapsVendorChart {
		t.Errorf("source.type = %q, want WrapsVendorChart", bp.Source.Type)
	}
	if bp.Source.Vendor == nil {
		t.Fatal("source.vendor is nil")
	}
	if bp.Source.Vendor.Provider != "nvidia" {
		t.Errorf("vendor.provider = %q, want nvidia", bp.Source.Vendor.Provider)
	}
	if len(bp.Components) != 1 {
		t.Fatalf("components length = %d, want 1", len(bp.Components))
	}
	if bp.PublishedBy != "aif-system" {
		t.Errorf("publishedBy = %q, want aif-system", bp.PublishedBy)
	}
	if len(emitter.wrapped) != 1 {
		t.Errorf("wrapped events = %d, want 1", len(emitter.wrapped))
	}
}

func TestWrapper_IdempotentOnExisting(t *testing.T) {
	store := NewFakeRepository()
	emitter := &fakeEventEmitter{}
	catalog := &fakeCatalog{apps: []apps.App{rbApp("nvidia", "nim-llm", "1.0.0")}}
	w := NewWrapper(catalog, store, emitter, discardLogger())

	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}

	bps, _ := store.ListWrapped(context.Background())
	if len(bps) != 1 {
		t.Errorf("got %d blueprints after 2 runs, want 1", len(bps))
	}
	if len(emitter.wrapped) != 1 {
		t.Errorf("wrapped events = %d, want 1 (only first run)", len(emitter.wrapped))
	}
}

func TestWrapper_SkipsNonSemVer(t *testing.T) {
	store := NewFakeRepository()
	emitter := &fakeEventEmitter{}
	catalog := &fakeCatalog{apps: []apps.App{rbApp("nvidia", "nim-llm", "latest")}}
	w := NewWrapper(catalog, store, emitter, discardLogger())

	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		t.Fatalf("WrapDetectedCharts: %v", err)
	}

	bps, _ := store.ListWrapped(context.Background())
	if len(bps) != 0 {
		t.Errorf("got %d blueprints, want 0 (non-semver skipped)", len(bps))
	}
	if len(emitter.wrapped) != 0 {
		t.Errorf("wrapped events = %d, want 0", len(emitter.wrapped))
	}
}

func TestWrapper_PrereleaseLabelSet(t *testing.T) {
	store := NewFakeRepository()
	emitter := &fakeEventEmitter{}
	catalog := &fakeCatalog{apps: []apps.App{rbApp("nvidia", "nim-llm", "1.0.0-rc.1")}}
	w := NewWrapper(catalog, store, emitter, discardLogger())

	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		t.Fatalf("WrapDetectedCharts: %v", err)
	}

	bps, _ := store.ListWrapped(context.Background())
	if len(bps) != 1 {
		t.Fatalf("got %d blueprints, want 1", len(bps))
	}
	cr, _ := store.Get(context.Background(), "nvidia-nim-llm.1.0.0-rc.1")
	if cr == nil {
		t.Fatal("CR not found in store")
	}
	if cr.Labels["ai.suse.com/blueprint-prerelease"] != "true" {
		t.Errorf("prerelease label = %q, want true", cr.Labels["ai.suse.com/blueprint-prerelease"])
	}
}

func TestWrapper_EmptyCatalog(t *testing.T) {
	store := NewFakeRepository()
	emitter := &fakeEventEmitter{}
	catalog := &fakeCatalog{apps: []apps.App{}}
	w := NewWrapper(catalog, store, emitter, discardLogger())

	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		t.Fatalf("WrapDetectedCharts: %v", err)
	}

	bps, _ := store.ListWrapped(context.Background())
	if len(bps) != 0 {
		t.Errorf("got %d blueprints, want 0", len(bps))
	}
}

func seedWrappedBlueprint(store *FakeRepository, source, chart, version string) {
	bp := blueprintFromApp(apps.App{
		ID:                 source + "." + chart + ":" + version,
		Name:               chart,
		Source:             source,
		Version:            version,
		ReferenceBlueprint: true,
		UseCase:            "inference",
		ChartRef: apps.ChartRef{
			Repo:    "oci://registry.suse.com/ai/charts/" + source,
			Chart:   chart,
			Version: version,
		},
	}, source+"-"+chart+"."+version)
	store.CreateWrapped(context.Background(), bp) //nolint:errcheck
}

func TestWrapper_WithdrawsOrphaned(t *testing.T) {
	store := NewFakeRepository()
	emitter := &fakeEventEmitter{}
	seedWrappedBlueprint(store, "nvidia", "nim-llm", "1.0.0")

	catalog := &fakeCatalog{apps: []apps.App{}}
	w := NewWrapper(catalog, store, emitter, discardLogger())

	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		t.Fatalf("WrapDetectedCharts: %v", err)
	}

	bps, _ := store.ListWrapped(context.Background())
	for _, bp := range bps {
		if bp.Name == "nvidia-nim-llm.1.0.0" && bp.Status.Phase != PhaseWithdrawn {
			t.Errorf("phase = %q, want Withdrawn", bp.Status.Phase)
		}
	}
	if len(emitter.withdrawn) != 1 {
		t.Errorf("withdrawn events = %d, want 1", len(emitter.withdrawn))
	}
}

func TestWrapper_StillInCatalog_NoWithdraw(t *testing.T) {
	store := NewFakeRepository()
	emitter := &fakeEventEmitter{}
	seedWrappedBlueprint(store, "nvidia", "nim-llm", "1.0.0")

	catalog := &fakeCatalog{apps: []apps.App{rbApp("nvidia", "nim-llm", "1.0.0")}}
	w := NewWrapper(catalog, store, emitter, discardLogger())

	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		t.Fatalf("WrapDetectedCharts: %v", err)
	}

	bps, _ := store.ListWrapped(context.Background())
	for _, bp := range bps {
		if bp.Name == "nvidia-nim-llm.1.0.0" && bp.Status.Phase == PhaseWithdrawn {
			t.Error("blueprint should NOT be withdrawn when still in catalog")
		}
	}
	if len(emitter.withdrawn) != 0 {
		t.Errorf("withdrawn events = %d, want 0", len(emitter.withdrawn))
	}
}

func TestWrapper_AlreadyWithdrawn_NoEventReemit(t *testing.T) {
	store := NewFakeRepository()
	emitter := &fakeEventEmitter{}
	seedWrappedBlueprint(store, "nvidia", "nim-llm", "1.0.0")
	_ = store.Withdraw(context.Background(), "nvidia-nim-llm.1.0.0")

	catalog := &fakeCatalog{apps: []apps.App{}}
	w := NewWrapper(catalog, store, emitter, discardLogger())

	if err := w.WrapDetectedCharts(context.Background()); err != nil {
		t.Fatalf("WrapDetectedCharts: %v", err)
	}

	if len(emitter.withdrawn) != 0 {
		t.Errorf("withdrawn events = %d, want 0 (already withdrawn)", len(emitter.withdrawn))
	}
}

func TestWrapper_CatalogListError_Propagates(t *testing.T) {
	store := NewFakeRepository()
	emitter := &fakeEventEmitter{}
	catalogErr := fmt.Errorf("upstream timeout")
	catalog := &fakeCatalog{err: catalogErr}
	w := NewWrapper(catalog, store, emitter, discardLogger())

	err := w.WrapDetectedCharts(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, catalogErr) {
		t.Errorf("error = %v, want wrapping of %v", err, catalogErr)
	}
}
