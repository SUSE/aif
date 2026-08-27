package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SUSE/aif-operator/internal/catalog"
)

// catalogDoc is the fixed shape of default-catalog.json. A struct (not a map)
// preserves top-level key order — encoding/json sorts map keys, which would flip
// "suse-ai" and "nvidia" and churn the file. Add a field here if a new library
// is introduced.
type catalogDoc struct {
	SuseAI []catalog.Item `json:"suse-ai"`
	NVIDIA []catalog.Item `json:"nvidia"`
}

const supportedCode = "supported"

// Recognized top-level library keys in default-catalog.json. Must match
// catalogDoc's json tags; used to reject any unknown library the round-trip
// through catalogDoc would otherwise silently drop.
const (
	librarySuseAI = "suse-ai"
	libraryNVIDIA = "nvidia"
)

// supportedLabel is the single chip every NVAIE entry carries.
var supportedLabel = []catalog.Label{{Code: supportedCode, Name: "Supported"}}

// preservedNVIDIASlugs are hand-curated nvidia entries that carry the Supported
// chip but are NOT generator-owned: the reconcile must never rebuild or drop
// them, even though the chip would otherwise mark them machine-owned. Use this
// only for a genuinely supported app the NGC anonymous search API does not
// surface (e.g. a hand-authored blueprint), so it can still show the Supported
// chip without being deleted on the next run. A listed entry is preserved
// verbatim unless a derived NGC entry takes over its slug (promotion). Keep this
// empty whenever every Supported nvidia entry is machine-derived.
var preservedNVIDIASlugs = map[string]bool{}

// isOwned reports whether an existing nvidia entry is generator-managed, i.e. it
// carries the Supported chip and is not in preservedNVIDIASlugs. Owned entries
// are rebuilt from the NGC-derived set every run (so their fields refresh and
// stale ones are removed); unowned entries — hand-added apps that are not
// NVAIE-supported, plus any preserved slug — are kept verbatim.
func isOwned(e catalog.Item) bool {
	if preservedNVIDIASlugs[e.SlugName] {
		return false
	}
	for _, l := range e.Labels {
		if l.Code == supportedCode {
			return true
		}
	}
	return false
}

// ensureSupportedLabel guarantees the Supported chip is present. An override may
// set its own "labels" list, which json.Unmarshal replaces wholesale; re-asserting
// the chip here means an override can add labels but never strip the mandatory chip
// every generator-owned entry must carry. Existing labels are preserved.
func ensureSupportedLabel(labels []catalog.Label) []catalog.Label {
	for _, l := range labels {
		if l.Code == supportedCode {
			return labels
		}
	}
	return append([]catalog.Label{supportedLabel[0]}, labels...)
}

// syncNVAIE reconciles the catalog's nvidia library against the NGC-derived set:
//   - Generator-owned entries (those carrying the Supported chip) are discarded
//     and rebuilt from `derived`, so every NGC-derivable field refreshes and an
//     owned entry NGC no longer lists is removed (stale).
//   - Each rebuilt entry carries the Supported chip and has its pinned override
//     fields applied on top of the fresh NGC values.
//   - Unowned existing entries (hand-added, not NVAIE-supported) and the entire
//     suse-ai library are preserved untouched — unless a derived entry takes over
//     the same slug, i.e. NGC now reports that chart as supported (promotion).
//
// The nvidia array is sorted by slug_name so output is deterministic and re-runs
// with the same NGC data and overrides produce no diff. Returns the re-marshaled
// document, the slugs newly present, and the slugs removed since the prior run.
func syncNVAIE(
	catalogJSON []byte, derived []catalog.Item, ov overrides,
) (out []byte, added, removed []string, err error) {
	// The tool round-trips the file through catalogDoc's fixed fields, so an
	// unrecognized top-level library key would be silently dropped on re-marshal.
	// Fail loudly instead: add a field to catalogDoc before regenerating.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(catalogJSON, &keys); err != nil {
		return nil, nil, nil, fmt.Errorf("parse catalog keys: %w", err)
	}
	for k := range keys {
		if k != librarySuseAI && k != libraryNVIDIA {
			return nil, nil, nil, fmt.Errorf(
				"unrecognized top-level catalog library %q: add it to catalogDoc before regenerating", k)
		}
	}

	var doc catalogDoc
	if err := json.Unmarshal(catalogJSON, &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("parse catalog: %w", err)
	}

	// Slugs already in the catalog before this run, measured by slug novelty (what
	// the UI keys tiles/routing on), so added/removed report app-level changes.
	originalSlugs := make(map[string]bool, len(doc.NVIDIA))
	for i := range doc.NVIDIA {
		originalSlugs[doc.NVIDIA[i].SlugName] = true
	}

	// Build the owned set fresh from the NGC-derived entries: stamp the Supported
	// chip, then apply any pinned overrides on top. Dedupe collapses a chart
	// published under several NGC repos to one entry (nim/nvidia wins).
	ownedBuilt := make([]catalog.Item, 0, len(derived))
	for _, d := range derived {
		d.Labels = supportedLabel
		if err := ov.apply(&d); err != nil {
			return nil, nil, nil, err
		}
		// Re-assert the chip in case an override's "labels" key replaced it.
		d.Labels = ensureSupportedLabel(d.Labels)
		ownedBuilt = append(ownedBuilt, d)
	}
	ownedBuilt = dedupeBySlug(ownedBuilt)

	ownedSlugs := make(map[string]bool, len(ownedBuilt))
	for i := range ownedBuilt {
		ownedSlugs[ownedBuilt[i].SlugName] = true
	}

	// Preserve unowned existing entries, except any slug a derived owned entry now
	// takes over (promotion). Owned existing entries are never carried over: they
	// are rebuilt from `derived` above, or dropped when NGC no longer lists them.
	result := make([]catalog.Item, 0, len(doc.NVIDIA)+len(ownedBuilt))
	for i := range doc.NVIDIA {
		e := doc.NVIDIA[i]
		if isOwned(e) || ownedSlugs[e.SlugName] {
			continue
		}
		result = append(result, e)
	}
	result = append(result, ownedBuilt...)

	// Defensive: collapse any residual slug collision among preserved entries.
	result = dedupeBySlug(result)

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].SlugName < result[j].SlugName
	})
	doc.NVIDIA = result

	finalSlugs := make(map[string]bool, len(doc.NVIDIA))
	for i := range doc.NVIDIA {
		finalSlugs[doc.NVIDIA[i].SlugName] = true
	}
	for s := range finalSlugs {
		if !originalSlugs[s] {
			added = append(added, s)
		}
	}
	for s := range originalSlugs {
		if !finalSlugs[s] {
			removed = append(removed, s)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, nil, nil, err
	}
	return append(b, '\n'), added, removed, nil
}

// dedupeBySlug collapses entries sharing a slug_name to one, since the UI keys
// tiles and routing on slug_name. The same chart can be published under several
// NGC repos (e.g. nim/nvidia and nvidia/nemo-microservices); the nim/nvidia copy
// wins. Order is by first appearance, so a later sort by slug stays stable.
func dedupeBySlug(items []catalog.Item) []catalog.Item {
	pos := make(map[string]int, len(items))
	out := make([]catalog.Item, 0, len(items))
	for _, it := range items {
		if i, ok := pos[it.SlugName]; ok {
			if preferOver(it, out[i]) {
				out[i] = it
			}
			continue
		}
		pos[it.SlugName] = len(out)
		out = append(out, it)
	}
	return out
}

// preferOver reports whether candidate should replace the current dedupe winner
// for a shared slug: higher repo rank wins, and on equal rank the lexicographically
// smaller RepositoryURL wins. The tiebreak makes the survivor independent of the
// order NGC returns collided repos, so the output stays byte-identical across runs.
func preferOver(candidate, current catalog.Item) bool {
	cr, or := repoRank(candidate.RepositoryURL), repoRank(current.RepositoryURL)
	if cr != or {
		return cr > or
	}
	return candidate.RepositoryURL < current.RepositoryURL
}

// nimNvidiaRepoURL is the preferred NGC repo for a chart published under several.
const nimNvidiaRepoURL = ngcHelmBase + "/nim/nvidia"

// repoRank ranks a repository URL for dedupe precedence: the nim/nvidia repo
// outranks every other NGC repo.
func repoRank(repositoryURL string) int {
	if strings.TrimRight(repositoryURL, "/") == nimNvidiaRepoURL {
		return 1
	}
	return 0
}
