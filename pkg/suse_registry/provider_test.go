package suse_registry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

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
	if len(got) != 3 {
		t.Fatalf("want 3 entries (foo:1.0.0, foo:1.1.0, bar:2.0.0); got %d: %+v", len(got), got)
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
