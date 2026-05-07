package nvidia

import "testing"

// TestClassifyChart pins the chart-name → NIM type heuristic from
// ARCHITECTURE.md §13.1. The contract is intentionally narrow: regex on a
// few keywords yields VLM; anything else defaults to LLM.
func TestClassifyChart(t *testing.T) {
	tests := []struct {
		name  string
		chart string
		want  Type
	}{
		// VLM keywords (case-insensitive, substring match — per the spec table).
		{"explicit vlm chart", "nim-vlm", TypeVLM},
		{"vision keyword", "nim-vision-pro", TypeVLM},
		{"kosmos keyword", "kosmos-x", TypeVLM},
		{"neva keyword", "neva-1", TypeVLM},
		{"uppercase VLM is matched", "NIM-VLM", TypeVLM},
		{"mixed case Vision is matched", "Nim-Vision", TypeVLM},
		{"vlm as substring is matched (per spec)", "ovlm-x", TypeVLM},

		// LLM (default) — anything not matching the VLM regex.
		{"explicit llm chart", "nim-llm", TypeLLM},
		{"unknown chart defaults to llm", "embedding-model", TypeLLM},
		{"empty name defaults to llm", "", TypeLLM},
		{"llm-like substring is still llm", "nim-llm-something", TypeLLM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyChart(tt.chart)
			if got != tt.want {
				t.Errorf("classifyChart(%q) = %q, want %q", tt.chart, got, tt.want)
			}
		})
	}
}
