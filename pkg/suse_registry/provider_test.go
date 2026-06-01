package suse_registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/SUSE/aif/pkg/oci"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRefresh_WalksAndFiltersNvidia(t *testing.T) {
	walker := &oci.FakeWalker{
		Catalog: map[string][]string{
			"ai/charts/nvidia/nim-llm": {"1.0.0"},
			"ai/charts/foo":            {"1.0.0", "1.1.0"},
			"ai/charts/bar":            {"2.0.0"},
			"other/skip":               {"9.9.9"},
		},
	}
	p := newProvider(silentLogger(), walker, &nullAnnotationReader{})
	p.UpdateSettings(EngineSettings{RegistryEndpoint: "registry.suse.com"})
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	got, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries (foo:1.1.0, bar:2.0.0) after semver dedupe; got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Chart == "nvidia/nim-llm" || c.ID == "nvidia/nim-llm:1.0.0" {
			t.Errorf("nvidia subtree leaked: %+v", c)
		}
	}
}

func TestRefresh_NotConfigured(t *testing.T) {
	p := newProvider(silentLogger(), &oci.FakeWalker{}, &nullAnnotationReader{})
	err := p.Refresh(context.Background())
	if err == nil || !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

type nullAnnotationReader struct{}

func (nullAnnotationReader) ChartAnnotations(_ context.Context, _, _ string) (map[string]string, error) {
	return nil, nil
}

// TestRefresh_DedupesToHighestSemver asserts a chart with N semver tags
// produces exactly one entry, at the highest semver.
func TestRefresh_DedupesToHighestSemver(t *testing.T) {
	walker := &oci.FakeWalker{
		Catalog: map[string][]string{
			"ai/charts/foo": {"1.0.0", "1.2.0", "1.1.0", "2.0.0", "1.0.0-rc1"},
		},
	}
	p := newProvider(silentLogger(), walker, &nullAnnotationReader{})
	p.UpdateSettings(EngineSettings{RegistryEndpoint: "registry.suse.com"})
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	got, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(got), got)
	}
	if got[0].Version != "2.0.0" {
		t.Errorf("want version 2.0.0, got %q", got[0].Version)
	}
	if got[0].ID != "foo:2.0.0" {
		t.Errorf("want ID foo:2.0.0, got %q", got[0].ID)
	}
}

// TestRefresh_DropsChartsWithNoSemverTags asserts charts whose tags
// are entirely non-semver (mutable like "latest", "main") are omitted
// from the cache rather than surfacing with a meaningless version.
func TestRefresh_DropsChartsWithNoSemverTags(t *testing.T) {
	walker := &oci.FakeWalker{
		Catalog: map[string][]string{
			"ai/charts/foo":     {"1.0.0"},
			"ai/charts/mutable": {"latest", "main", "dev"},
		},
	}
	p := newProvider(silentLogger(), walker, &nullAnnotationReader{})
	p.UpdateSettings(EngineSettings{RegistryEndpoint: "registry.suse.com"})
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	got, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry (foo:1.0.0), got %d: %+v", len(got), got)
	}
	if got[0].Chart != "foo" {
		t.Errorf("want chart foo, got %q", got[0].Chart)
	}
}

// slowAnnotationReader sleeps before returning so the errgroup
// fan-out in enrichWithAnnotations actually interleaves under -race.
type slowAnnotationReader struct{}

func (slowAnnotationReader) ChartAnnotations(_ context.Context, _, _ string) (map[string]string, error) {
	time.Sleep(2 * time.Millisecond)
	return map[string]string{"ai.suse.com/display-name": "x"}, nil
}

// TestEnrichWithAnnotations_NoConcurrentMapAccess is a regression net
// for an earlier bug: enrichWithAnnotations's fan-out goroutines were
// reading entries[k] without holding mu while other goroutines wrote
// to entries[k] under mu, which trips Go's map race detector.
// Must be run with -race to catch the regression.
func TestEnrichWithAnnotations_NoConcurrentMapAccess(t *testing.T) {
	p := newProvider(silentLogger(), nil, slowAnnotationReader{})
	entries := make(map[string]SUSEChart, 64)
	for i := 0; i < 64; i++ {
		k := fmt.Sprintf("chart-%d:1.0.0", i)
		entries[k] = SUSEChart{ID: k, Chart: fmt.Sprintf("chart-%d", i), Version: "1.0.0"}
	}
	p.enrichWithAnnotations(context.Background(), entries)
	for k, e := range entries {
		if e.DisplayName != "x" {
			t.Fatalf("entries[%q].DisplayName = %q, want %q", k, e.DisplayName, "x")
		}
	}
}
