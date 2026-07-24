package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/SUSE/aif-operator/internal/catalog"
)

// catalogLabel aliases the operator's catalog.Label so this package and its tests
// use one type that marshals to {"code","name"}.
type catalogLabel = catalog.Label

type ngcLabelGroup struct {
	Key              string   `json:"key"`
	Values           []string `json:"values"`
	UnresolvedValues []string `json:"unresolvedValues"`
}

type ngcResource struct {
	ResourceType string          `json:"resourceType"`
	ResourceID   string          `json:"resourceId"`
	Name         string          `json:"name"`
	DisplayName  string          `json:"displayName"`
	Labels       []ngcLabelGroup `json:"labels"`
}

type ngcResponse struct {
	ResultTotal int `json:"resultTotal"`
	Results     []struct {
		GroupValue string        `json:"groupValue"`
		Resources  []ngcResource `json:"resources"`
	} `json:"results"`
}

// parseResources returns the HELM_CHART group's resources, deduped by ResourceID.
func parseResources(body []byte) ([]ngcResource, error) {
	var resp ngcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse NGC response: %w", err)
	}
	seen := map[string]bool{}
	var out []ngcResource
	for _, g := range resp.Results {
		if g.GroupValue != "HELM_CHART" {
			continue
		}
		for _, r := range g.Resources {
			if seen[r.ResourceID] {
				continue
			}
			seen[r.ResourceID] = true
			out = append(out, r)
		}
	}
	return out, nil
}

// isProgramCode reports whether a label is a program/support designation worth
// surfacing: anything in the NGC "productNames" group (curated products and
// subscriptions), or any "*_supported" designation (which NGC keeps in the
// "general" group). Everything else (pltfm_*, soln_*, uscs_*, indus_*, NSPECT-*)
// is noise and skipped. Selecting by group/suffix — rather than an exact list —
// means a new NVIDIA program shows up automatically.
func isProgramCode(groupKey, code string) bool {
	return groupKey == "productNames" || strings.HasSuffix(code, "_supported")
}

// programLabels selects a resource's program/support labels (see isProgramCode)
// and resolves each display name: the curated names map wins, else the API's
// resolved value when it differs from the code, else a humanized code. Returns
// the labels sorted by code, plus the codes that fell back to a humanized name
// (so the caller can log them and a nicer name can be added to programDisplayNames).
func programLabels(
	res ngcResource,
	names map[string]string,
	hidden map[string]bool,
) (labels []catalogLabel, needNames []string) {
	type sel struct {
		name      string
		humanized bool
	}
	found := map[string]sel{}
	for _, g := range res.Labels {
		for i, code := range g.UnresolvedValues {
			if code == "" || hidden[code] || !isProgramCode(g.Key, code) {
				continue
			}
			name, humanized := names[code], false
			if name == "" {
				if i < len(g.Values) && g.Values[i] != "" && g.Values[i] != code {
					name = g.Values[i] // API-resolved display name
				} else {
					name, humanized = humanizeCode(code), true
				}
			}
			// A curated/API-resolved name beats a previously humanized one.
			if prev, ok := found[code]; !ok || (prev.humanized && !humanized) {
				found[code] = sel{name: name, humanized: humanized}
			}
		}
	}
	codes := make([]string, 0, len(found))
	for c := range found {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		labels = append(labels, catalogLabel{Code: c, Name: found[c].name})
		if found[c].humanized {
			needNames = append(needNames, c)
		}
	}
	return labels, needNames
}

// humanizeCode turns a code like "nv-ai-enterprise" into "Nv Ai Enterprise".
func humanizeCode(code string) string {
	parts := strings.FieldsFunc(code, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// matchKey normalizes a repository URL + chart name into an "org[/team]/chart" key
// comparable to an NGC resourceId.
func matchKey(repositoryURL, chart string) string {
	path := strings.TrimSpace(repositoryURL)
	if u, err := url.Parse(path); err == nil && u.Host != "" {
		path = u.Path
	} else {
		path = strings.TrimPrefix(strings.TrimPrefix(path, "https://"), "http://")
		if i := strings.IndexByte(path, '/'); i >= 0 {
			path = path[i:]
		} else {
			path = ""
		}
	}
	key := strings.Trim(path, "/") + "/" + chart
	return strings.ToLower(strings.Trim(key, "/"))
}

// applyLabels sets Labels on the catalog's nvidia entries by matchKey(repository_url,
// slug_name). It round-trips through catalog.Item so field order is stable. Returns the
// re-marshaled document and the slugs of nvidia entries with no NGC match.
func applyLabels(catalogJSON []byte, byKey map[string][]catalogLabel) ([]byte, []string, error) {
	// Round-trips through catalog.Item, so only fields declared on that struct
	// survive the write-back. Keep catalog.Item in sync if default-catalog.json
	// gains fields, or they will be silently dropped here.
	var doc map[string][]catalog.Item
	if err := json.Unmarshal(catalogJSON, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse catalog: %w", err)
	}
	var unmatched []string
	for i := range doc["nvidia"] {
		e := &doc["nvidia"][i]
		labels, ok := byKey[matchKey(e.RepositoryURL, e.SlugName)]
		if !ok || len(labels) == 0 {
			// Clear any stale labels: the tool is the source of truth, so an entry
			// whose program NGC no longer reports must lose its old labels, not keep them.
			e.Labels = nil
			unmatched = append(unmatched, e.SlugName)
			continue
		}
		e.Labels = labels
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(out, '\n'), unmatched, nil
}
