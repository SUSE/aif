package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseResources(t *testing.T) {
	body, err := os.ReadFile("testdata/ngc_response.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := parseResources(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 resources, got %d", len(res))
	}
	if res[0].ResourceID != "nim/nvidia/nvidia-nim-llama-nemotron-embed-vl-1b-v2" {
		t.Fatalf("unexpected first resource: %+v", res[0])
	}
}

func TestProgramLabels(t *testing.T) {
	body, _ := os.ReadFile("testdata/ngc_response.json")
	res, _ := parseResources(body)
	names := programDisplayNames()
	hidden := hiddenPrograms()

	// NIM chart: nvaie_supported (general, _supported) + nv-ai-enterprise
	// (productNames group); the noise codes (NSPECT-*, nimmcro_*) are excluded.
	nim, need := programLabels(res[0], names, hidden)
	if len(nim) != 2 {
		t.Fatalf("want 2 labels, got %d: %+v", len(nim), nim)
	}
	// Sorted by code: nv-ai-enterprise, nvaie_supported.
	if nim[0].Code != "nv-ai-enterprise" || nim[0].Name != "NVIDIA AI Enterprise Essentials" {
		t.Fatalf("unexpected label[0]: %+v", nim[0])
	}
	if nim[1].Code != "nvaie_supported" || nim[1].Name != "NVIDIA AI Enterprise Supported" {
		t.Fatalf("unexpected label[1]: %+v", nim[1])
	}
	if len(need) != 0 {
		t.Fatalf("want no unnamed codes (all in map), got %v", need)
	}

	// Blueprint chart: no productNames and no _supported codes → no labels.
	if bp, _ := programLabels(res[1], names, hidden); len(bp) != 0 {
		t.Fatalf("want 0 labels for blueprint, got %+v", bp)
	}
}

// A new program not in the map is still surfaced: productNames-group codes are
// humanized (and reported), while an unknown *_supported code uses the API's
// resolved display value. Noise (soln_*) and hidden codes are excluded.
func TestProgramLabels_AutoSurfaceAndHidden(t *testing.T) {
	res := ngcResource{Labels: []ngcLabelGroup{
		{
			Key:              "general",
			Values:           []string{"Foo", "NVIDIA Widget Supported"},
			UnresolvedValues: []string{"soln_foo", "widget_supported"},
		},
		{
			Key:              "productNames",
			Values:           []string{"some-new-product", "secret-internal"},
			UnresolvedValues: []string{"some-new-product", "secret-internal"},
		},
	}}
	labels, need := programLabels(res, map[string]string{}, map[string]bool{"secret-internal": true})

	// soln_foo (noise) and secret-internal (hidden) excluded; sorted by code:
	// some-new-product, widget_supported.
	if len(labels) != 2 {
		t.Fatalf("want 2 labels, got %d: %+v", len(labels), labels)
	}
	if labels[0].Code != "some-new-product" || labels[0].Name != "Some New Product" {
		t.Fatalf("productNames code should be humanized: %+v", labels[0])
	}
	if labels[1].Code != "widget_supported" || labels[1].Name != "NVIDIA Widget Supported" {
		t.Fatalf("unknown _supported code should use API value: %+v", labels[1])
	}
	// Only the humanized productNames code needs a display name.
	if len(need) != 1 || need[0] != "some-new-product" {
		t.Fatalf("want need=[some-new-product], got %v", need)
	}
}

// A catalog entry with existing labels but no NGC match must have them cleared —
// the tool can remove labels, not just add them.
func TestApplyLabels_ClearsStale(t *testing.T) {
	catIn := []byte(`{"nvidia":[{"name":"Old","slug_name":"old",` +
		`"repository_url":"https://helm.ngc.nvidia.com/nvidia",` +
		`"labels":[{"code":"nvaie_supported","name":"NVIDIA AI Enterprise Supported"}]}]}`)
	out, unmatched, err := applyLabels(catIn, map[string][]catalogLabel{})
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatched) != 1 || unmatched[0] != "old" {
		t.Fatalf("want [old] unmatched, got %v", unmatched)
	}
	if strings.Contains(string(out), "nvaie_supported") || strings.Contains(string(out), `"labels"`) {
		t.Fatalf("stale labels not cleared: %s", out)
	}
}

func TestMatchKey(t *testing.T) {
	got := matchKey("https://helm.ngc.nvidia.com/nim/nvidia", "nvidia-nim-llama-nemotron-embed-vl-1b-v2")
	want := "nim/nvidia/nvidia-nim-llama-nemotron-embed-vl-1b-v2"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
	if got := matchKey("https://helm.ngc.nvidia.com/nvidia/", "gpu-operator"); got != "nvidia/gpu-operator" {
		t.Fatalf("trailing-slash/no-team case: got %q", got)
	}
}

func TestApplyLabels(t *testing.T) {
	catIn, err := os.ReadFile("testdata/catalog_in.json")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string][]catalogLabel{
		"nim/nvidia/nvidia-nim-llama-nemotron-embed-vl-1b-v2": {
			{Code: "nvaie_supported", Name: "NVIDIA AI Enterprise Supported"},
		},
	}
	out, unmatched, err := applyLabels(catIn, byKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "nvaie_supported") {
		t.Fatalf("labels not written into catalog: %s", out)
	}
	// The blueprint entry has no match and must be reported.
	if len(unmatched) != 1 || unmatched[0] != "nvidia-blueprint-rag" {
		t.Fatalf("want [nvidia-blueprint-rag] unmatched, got %v", unmatched)
	}
}
