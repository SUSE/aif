package apps

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

// staticSource serves a fixed []App and reports a configurable Name.
type staticSource struct {
	name string
	apps []App
}

func (s *staticSource) Name() string                          { return s.name }
func (s *staticSource) List(_ context.Context) ([]App, error) { return s.apps, nil }
func (s *staticSource) Refresh(_ context.Context) error       { return nil }
func (s *staticSource) UpdateSettings(_ EngineSettings)       {}

// TestCatalog_Get_MultiSegmentPrefix asserts that Catalog.Get
// dispatches correctly when Source.Name() returns a multi-segment
// prefix (e.g. "suse.appco"). Single-dot split would mis-route to
// the "suse" prefix; the new prefix-iteration parser must match
// the full Source.Name()+"." prefix.
func TestCatalog_Get_MultiSegmentPrefix(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := New(logger, 0)
	c.AddSource(&staticSource{
		name: "suse.appco",
		apps: []App{{ID: "suse.appco.milvus:2.4.1", Source: "suse", Origin: "appco"}},
	})
	c.AddSource(&staticSource{
		name: "suse.registry",
		apps: []App{{ID: "suse.registry.nginx:1.0.0", Source: "suse", Origin: "registry"}},
	})

	got, err := c.Get(context.Background(), "suse.registry.nginx:1.0.0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Origin != "registry" {
		t.Errorf("Get routed to wrong Source: %+v", got)
	}
}
