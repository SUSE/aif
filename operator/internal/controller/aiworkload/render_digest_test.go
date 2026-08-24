package aiworkload

import "testing"

func TestPerHelmOpRenderDigest_DeterministicAcrossOrder(t *testing.T) {
	a := ComponentRenderInputs{
		ChartRepo: "r", ChartName: "c", ChartVersion: "1.0.0", Namespace: "ai", Vendor: "suse",
		RepoURL: "oci://x", Targets: []string{"c-b", "c-a"},
		Values: map[string]any{"b": 2, "a": map[string]any{"y": 1, "x": 2}},
	}
	b := ComponentRenderInputs{
		ChartRepo: "r", ChartName: "c", ChartVersion: "1.0.0", Namespace: "ai", Vendor: "suse",
		RepoURL: "oci://x", Targets: []string{"c-a", "c-b"}, // reordered set
		Values: map[string]any{"a": map[string]any{"x": 2, "y": 1}, "b": 2}, // reordered map
	}
	if perHelmOpRenderDigest(a) != perHelmOpRenderDigest(b) {
		t.Fatalf("digest not stable across set/map ordering")
	}
}

func TestPerHelmOpRenderDigest_ChangesOnValueChange(t *testing.T) {
	base := ComponentRenderInputs{ChartName: "c", ChartVersion: "1.0.0", Namespace: "ai", Values: map[string]any{"image": map[string]any{"tag": "good"}}}
	bad := ComponentRenderInputs{ChartName: "c", ChartVersion: "1.0.0", Namespace: "ai", Values: map[string]any{"image": map[string]any{"tag": "missing"}}}
	if perHelmOpRenderDigest(base) == perHelmOpRenderDigest(bad) {
		t.Fatalf("digest should change when a value (image.tag) changes")
	}
}

func TestAggregateRenderDigest_DeterministicAcrossEntryOrder(t *testing.T) {
	e1 := []AggregateEntry{{"fleet-default", "b", "d2"}, {"fleet-local", "a", "d1"}}
	e2 := []AggregateEntry{{"fleet-local", "a", "d1"}, {"fleet-default", "b", "d2"}}
	if aggregateRenderDigest(e1) != aggregateRenderDigest(e2) {
		t.Fatalf("aggregate digest not stable across entry order")
	}
}
