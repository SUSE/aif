package apps

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/SUSE/aif/pkg/suse_registry"
)

func TestSUSERegistrySource_TranslatesToApp(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fake := &suse_registry.FakeProvider{
		Charts: map[string]suse_registry.SUSEChart{
			"foo:1.0.0": {
				ID:                 "foo:1.0.0",
				Chart:              "foo",
				Version:            "1.0.0",
				DisplayName:        "Foo Chart",
				Description:        "desc",
				UseCase:            "rag",
				ReferenceBlueprint: false,
				LastUpdatedAt:      &now,
				ChartRef:           "oci://registry.suse.com/ai/charts/foo:1.0.0",
			},
		},
	}
	s := NewSUSERegistrySource(fake, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	got, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 App, got %d", len(got))
	}
	a := got[0]
	if a.ID != "suse.registry.foo:1.0.0" {
		t.Errorf("ID: got %q, want suse.registry.foo:1.0.0", a.ID)
	}
	if a.Source != "suse" || a.Origin != "registry" {
		t.Errorf("Source/Origin: got %q/%q, want suse/registry", a.Source, a.Origin)
	}
	if a.DisplayName != "Foo Chart" {
		t.Errorf("DisplayName: %q", a.DisplayName)
	}
	if a.ChartRef.Repo == "" || a.ChartRef.Chart != "foo" || a.ChartRef.Version != "1.0.0" {
		t.Errorf("ChartRef parse wrong: %+v", a.ChartRef)
	}
}

func TestSUSERegistrySource_Name(t *testing.T) {
	s := NewSUSERegistrySource(&suse_registry.FakeProvider{}, nil, 0)
	if s.Name() != "suse.registry" {
		t.Errorf("Name: got %q, want suse.registry", s.Name())
	}
}
