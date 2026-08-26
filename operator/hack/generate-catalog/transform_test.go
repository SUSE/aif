package main

import (
	"os"
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
