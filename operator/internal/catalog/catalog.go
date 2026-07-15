/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package catalog owns the static application catalog served to the AIF UI: the
// bundled default list (embedded here) and normalization of any remote catalog
// document into the flat, validated list the UI renders.
package catalog

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
)

//go:embed default-catalog.json
var bundledJSON []byte

// Item is a single application catalog entry (mirrors the UI's AppCollectionItem).
type Item struct {
	Name              string `json:"name"`
	SlugName          string `json:"slug_name"`
	Description       string `json:"description,omitempty"`
	ProjectURL        string `json:"project_url,omitempty"`
	DocumentationURL  string `json:"documentation_url,omitempty"`
	ReferenceGuideURL string `json:"reference_guide_url,omitempty"`
	SourceCodeURL     string `json:"source_code_url,omitempty"`
	LogoURL           string `json:"logo_url,omitempty"`
	ChangelogURL      string `json:"changelog_url,omitempty"`
	LastUpdatedAt     string `json:"last_updated_at,omitempty"`
	PackagingFormat   string `json:"packaging_format,omitempty"`
	RepositoryURL     string `json:"repository_url,omitempty"`
	Library           string `json:"library,omitempty"`
}

// bundled is normalized once at startup from the embedded default catalog.
var bundled = Normalize(bundledJSON)

// Bundled returns the normalized default catalog shipped with the operator.
func Bundled() []Item { return bundled }

// Normalize turns a raw catalog document into a flat, validated, sorted []Item.
// It accepts the three shapes the bundled file and remote catalogs may use:
//   - a library-keyed object: {"suse-ai":[...],"nvidia":[...]} — `library` is
//     stamped from each key (an entry may override it with its own `library`);
//   - a flat array of entries (each carrying its own `library`);
//   - a {"items":[...]} wrapper.
//
// Invalid entries (missing name/slug_name, or an unrecognized packaging_format)
// are dropped. Entries are sorted alphabetically by name within each library.
// Returns nil when nothing valid is found or the input is not valid JSON.
func Normalize(raw []byte) []Item {
	items, _ := NormalizeReport(raw)
	return items
}

// NormalizeReport is Normalize plus the count of candidate entries parsed from the
// raw document (before validation drops). Callers fetching admin-supplied remote
// catalogs use `parsed - len(items)` to log how many entries were dropped, which is
// otherwise silent and hard to debug.
func NormalizeReport(raw []byte) (items []Item, parsed int) {
	candidates := parse(raw)
	return finalize(candidates), len(candidates)
}

// parse extracts candidate entries from the three accepted document shapes, before
// any validation. Returns nil when the input is not one of the accepted shapes.
func parse(raw []byte) []Item {
	// Flat array form.
	var arr []Item
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}

	// Object form: either {"items":[...]} or a library-keyed object.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	if itemsRaw, ok := obj["items"]; ok {
		var items []Item
		if err := json.Unmarshal(itemsRaw, &items); err != nil {
			return nil
		}
		return items
	}

	var out []Item
	for library, entriesRaw := range obj {
		var entries []Item
		if err := json.Unmarshal(entriesRaw, &entries); err != nil {
			continue // skip non-array values (e.g. stray metadata keys)
		}
		for i := range entries {
			if entries[i].Library == "" {
				entries[i].Library = library
			}
			out = append(out, entries[i])
		}
	}
	return out
}

func finalize(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.Name == "" || it.SlugName == "" {
			continue
		}
		if it.PackagingFormat != "" && it.PackagingFormat != "HELM_CHART" && it.PackagingFormat != "CONTAINER" {
			continue
		}
		out = append(out, it)
	}
	// Alphabetical (case-insensitive) by name within each library.
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].Library), strings.ToLower(out[j].Library)
		if li != lj {
			return li < lj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) == 0 {
		return nil
	}
	return out
}
