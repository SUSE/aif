package nvidia

import "regexp"

// vlmKeywordPattern matches a chart name that should be classified as VLM,
// per ARCHITECTURE.md §13.1. Case-insensitive, substring-matching.
// Compiled once at package load.
var vlmKeywordPattern = regexp.MustCompile(`(?i)vlm|vision|kosmos|neva`)

// classifyChart returns the inferred NIM Type for a chart name. It is the
// only piece of logic that distinguishes a VLM chart from any other chart
// the catalog surfaces; any other classification (embed, etc.) is the
// deployer's responsibility, not discovery's.
//
// Pure function: deterministic, no I/O, safe to call from any goroutine.
func classifyChart(chartName string) Type {
	if vlmKeywordPattern.MatchString(chartName) {
		return TypeVLM
	}
	return TypeLLM
}
