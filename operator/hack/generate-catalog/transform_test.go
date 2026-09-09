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
	res, total, err := parseResources(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 resources, got %d", len(res))
	}
	if total != 2 {
		t.Fatalf("want resultTotal 2, got %d", total)
	}
	if res[0].ResourceID != "nim/nvidia/nvidia-nim-llama-nemotron-embed-vl-1b-v2" {
		t.Fatalf("unexpected first resource: %+v", res[0])
	}
}

func TestVerifyComplete(t *testing.T) {
	tests := []struct {
		name             string
		collected, total int
		wantErr          bool
	}{
		{name: "full response", collected: 5, total: 5, wantErr: false},
		{name: "over-fetch tolerated", collected: 6, total: 5, wantErr: false},
		{name: "empty/degraded response", collected: 0, total: 0, wantErr: true},
		{name: "truncated page walk", collected: 3, total: 5, wantErr: true},
		{name: "claims some but sent none", collected: 0, total: 5, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyComplete(tc.collected, tc.total)
			if (err != nil) != tc.wantErr {
				t.Fatalf("verifyComplete(%d, %d) err = %v, wantErr %v", tc.collected, tc.total, err, tc.wantErr)
			}
		})
	}
}
