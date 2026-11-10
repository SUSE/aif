package main

import (
	"encoding/json"
	"fmt"

	"github.com/SUSE/aif-operator/internal/catalog"
)

// enrichment carries the per-chart NGC details fetched for this run plus the
// overviews already in the catalog, so a chart whose details are unavailable
// (no API key, or a failed request) keeps its existing overview instead of
// losing it. The zero value applies no enrichment.
type enrichment struct {
	details      detailsByChart
	prevOverview map[string]string // slug_name -> overview of existing nvidia entries
}

// newEnrichment builds the enrichment for a run from the fetched details (may be
// nil) and the nvidia entries of the input catalog.
func newEnrichment(details detailsByChart, existing []catalog.Item) enrichment {
	prev := make(map[string]string, len(existing))
	for _, e := range existing {
		if e.Overview != "" {
			prev[e.SlugName] = e.Overview
		}
	}
	return enrichment{details: details, prevOverview: prev}
}

// applyOwned enriches a freshly derived generator-owned entry. The previous
// overview is carried over first; non-empty detail fields then replace it and
// the search API's version and date. Without details, version and date stay
// from the search API.
func (en enrichment) applyOwned(it *catalog.Item) {
	it.Overview = en.prevOverview[it.SlugName]
	if d, ok := en.details[chartKey(it.RepositoryURL, it.SlugName)]; ok {
		applyDetails(it, d)
	}
}

// applyUnowned enriches a hand-added entry: only the enrichment fields are
// written, and only when details were fetched; otherwise it is left untouched.
func (en enrichment) applyUnowned(it *catalog.Item) {
	if d, ok := en.details[chartKey(it.RepositoryURL, it.SlugName)]; ok {
		applyDetails(it, d)
	}
}

// applyDetails writes the enrichment fields from d. Each field is replaced only
// when NGC reported a non-empty value, so a sparse details response never blanks
// an existing overview, version or date.
func applyDetails(it *catalog.Item, d chartDetails) {
	if d.Overview != "" {
		it.Overview = d.Overview
	}
	if d.LatestVersion != "" {
		it.LatestVersion = d.LatestVersion
	}
	if d.UpdatedDate != "" {
		it.LastUpdatedAt = d.UpdatedDate
	}
}

// enrichmentTargets lists the items whose NGC details should be fetched: every
// existing nvidia entry (owned or not) plus an entry derived from every
// NVAIE-supported chart. fetchAllDetails skips non-NGC hosts and duplicates.
func enrichmentTargets(catalogJSON []byte, resources []ngcResource) ([]catalog.Item, error) {
	var doc catalogDoc
	if err := json.Unmarshal(catalogJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}
	targets := append([]catalog.Item(nil), doc.NVIDIA...)
	for _, res := range resources {
		if isNVAIE(res) {
			targets = append(targets, deriveItem(res))
		}
	}
	return targets, nil
}
