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

// mergeNVAIE merges derived NVAIE entries into the catalog's nvidia library:
//   - an entry matching an existing nvidia entry (by repo-path + slug) has its
//     Labels set to the single Supported chip; no other field is touched.
//   - an entry with no match is appended, carrying the Supported chip.
//
// Existing entries' non-label fields and the entire suse-ai library are never
// modified. The nvidia array is sorted by slug_name so output is deterministic
// and re-runs with the same NGC data produce no diff. Returns the re-marshaled
// document and the slugs of newly appended entries.
func mergeNVAIE(catalogJSON []byte, derived []catalog.Item) (out []byte, added []string, err error) {
	// The tool round-trips the file through catalogDoc's fixed fields, so an
	// unrecognized top-level library key would be silently dropped on re-marshal.
	// Fail loudly instead: add a field to catalogDoc before regenerating.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(catalogJSON, &keys); err != nil {
		return nil, nil, fmt.Errorf("parse catalog keys: %w", err)
	}
	for k := range keys {
		if k != librarySuseAI && k != libraryNVIDIA {
			return nil, nil, fmt.Errorf("unrecognized top-level catalog library %q: add it to catalogDoc before regenerating", k)
		}
	}

	var doc catalogDoc
	if err := json.Unmarshal(catalogJSON, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse catalog: %w", err)
	}

	// Slugs already in the catalog before this merge. "added" is measured against
	// slug novelty (what the UI keys tiles/routing on), not repo+slug: the same
	// chart re-published under a new NGC repo is not a new app, and dedupe will
	// collapse it back to the existing entry.
	originalSlugs := make(map[string]bool, len(doc.NVIDIA))
	for i := range doc.NVIDIA {
		originalSlugs[doc.NVIDIA[i].SlugName] = true
	}

	idx := make(map[string]int, len(doc.NVIDIA))
	for i := range doc.NVIDIA {
		idx[matchKey(doc.NVIDIA[i].RepositoryURL, doc.NVIDIA[i].SlugName)] = i
	}

	for _, d := range derived {
		k := matchKey(d.RepositoryURL, d.SlugName)
		if i, ok := idx[k]; ok {
			doc.NVIDIA[i].Labels = supportedLabel
			continue
		}
		d.Labels = supportedLabel
		doc.NVIDIA = append(doc.NVIDIA, d)
		idx[k] = len(doc.NVIDIA) - 1
	}

	doc.NVIDIA = dedupeBySlug(doc.NVIDIA)

	sort.SliceStable(doc.NVIDIA, func(i, j int) bool {
		return doc.NVIDIA[i].SlugName < doc.NVIDIA[j].SlugName
	})

	// Report slugs present after the merge that were absent before it.
	seen := make(map[string]bool, len(doc.NVIDIA))
	for i := range doc.NVIDIA {
		s := doc.NVIDIA[i].SlugName
		if originalSlugs[s] || seen[s] {
			continue
		}
		seen[s] = true
		added = append(added, s)
	}
	sort.Strings(added)

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(b, '\n'), added, nil
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
			if repoRank(it.RepositoryURL) > repoRank(out[i].RepositoryURL) {
				out[i] = it
			}
			continue
		}
		pos[it.SlugName] = len(out)
		out = append(out, it)
	}
	return out
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
