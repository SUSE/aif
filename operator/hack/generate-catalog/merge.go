package main

import (
	"encoding/json"
	"fmt"
	"net/url"
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

// ensureSupportedLabel guarantees the Supported chip is present. An override may
// set its own "labels" list, which json.Unmarshal replaces wholesale; re-asserting
// the chip here means an override can add labels but never strip the mandatory chip
// every supported entry must carry. Existing labels are preserved.
func ensureSupportedLabel(labels []catalog.Label) []catalog.Label {
	for _, l := range labels {
		if l.Code == supportedCode {
			return labels
		}
	}
	return append([]catalog.Label{supportedLabel[0]}, labels...)
}

// isOwned reports whether an existing nvidia entry is generator-managed: only
// entries marked with source "ngc" are. The Supported chip says nothing about
// ownership, so a hand-authored entry may carry it and is never touched.
func isOwned(e catalog.Item) bool {
	return e.Source == catalog.SourceNGC
}

func hasSupported(labels []catalog.Label) bool {
	for _, l := range labels {
		if l.Code == supportedCode {
			return true
		}
	}
	return false
}

// withoutSupported drops the Supported chip, keeping any other labels. Returns nil
// when none remain so `omitempty` keeps the field absent.
func withoutSupported(labels []catalog.Label) []catalog.Label {
	out := make([]catalog.Label, 0, len(labels))
	for _, l := range labels {
		if l.Code != supportedCode {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// deployable reports whether charts under an NGC path can be listed.
func deployable(kind catalog.NGCPathKind) bool {
	return kind == catalog.NGCPathOrg || kind == catalog.NGCPathPublic || kind == catalog.NGCPathGated
}

// buildOwned derives a generator-owned entry from res: fresh NGC fields, pinned
// overrides on top, then the Supported chip and source marker set from NGC state
// so an override can neither add nor strip the chip, nor change ownership.
func buildOwned(res ngcResource, supported bool, ov overrides) (catalog.Item, error) {
	it := deriveItem(res)
	if supported {
		it.Labels = supportedLabel
	}
	if err := ov.apply(&it); err != nil {
		return catalog.Item{}, err
	}
	if supported {
		it.Labels = ensureSupportedLabel(it.Labels)
	} else {
		it.Labels = withoutSupported(it.Labels)
	}
	it.Source = catalog.SourceNGC
	return it, nil
}

// ownedResourceKey maps an owned entry to its NGC resource key and repo path. An
// owned entry must point at the NGC Helm host; anything else is a hand edit gone
// wrong, so the run fails instead of guessing.
func ownedResourceKey(e catalog.Item) (key, path string, err error) {
	repo := strings.TrimRight(strings.TrimSpace(e.RepositoryURL), "/")
	if !catalog.IsNGCURL(repo) {
		return "", "", fmt.Errorf("owned entry %q has non-NGC repository_url %q", e.SlugName, e.RepositoryURL)
	}
	u, err := url.Parse(repo)
	if err != nil {
		return "", "", fmt.Errorf("owned entry %q: parse repository_url: %w", e.SlugName, err)
	}
	return strings.TrimPrefix(u.Path, "/") + "/" + e.SlugName, u.Path, nil
}

// ngcState is the NGC catalog reduced to what the reconcile needs.
type ngcState struct {
	byKey        map[string]ngcResource         // every chart, by ngcResourceKey
	labeled      []catalog.Item                 // owned entries for supported charts on deployable paths
	unclassified map[string][]unclassifiedChart // supported charts on unknown paths, by path
}

func indexNGC(resources []ngcResource, ov overrides) (ngcState, error) {
	st := ngcState{
		byKey:        make(map[string]ngcResource, len(resources)),
		unclassified: map[string][]unclassifiedChart{},
	}
	for _, res := range resources {
		st.byKey[ngcResourceKey(res)] = res
		if !isNVAIE(res) {
			continue
		}
		path := ngcRepoPath(res)
		kind := catalog.ClassifyNGCPath(path)
		switch {
		case deployable(kind):
			it, err := buildOwned(res, true, ov)
			if err != nil {
				return ngcState{}, err
			}
			st.labeled = append(st.labeled, it)
		case kind == catalog.NGCPathUnknown:
			st.unclassified[path] = append(st.unclassified[path], unclassifiedChart{
				ResourceID: res.ResourceID, Name: res.DisplayName, Version: ngcLatestVersion(res),
			})
		}
	}
	// Collapse a chart published under several NGC repos to one entry (nim/nvidia wins).
	st.labeled = dedupeBySlug(st.labeled)
	return st, nil
}

// reconcileOwned rebuilds an owned entry no supported chart claims. It returns
// keep=false with a removal reason when NGC no longer publishes the chart or its
// path is no longer deployable; otherwise the chart is still published without
// nvaie_supported, so it stays, refreshed, without the Supported chip.
func reconcileOwned(e catalog.Item, st ngcState, ov overrides) (it catalog.Item, keep bool, reason string, err error) {
	key, path, err := ownedResourceKey(e)
	if err != nil {
		return catalog.Item{}, false, "", err
	}
	res, ok := st.byKey[key]
	if !ok {
		return catalog.Item{}, false, reasonNotOnNGC, nil
	}
	if !deployable(catalog.ClassifyNGCPath(path)) {
		return catalog.Item{}, false, reasonPathPrefix + path, nil
	}
	it, err = buildOwned(res, false, ov)
	return it, err == nil, "", err
}

// syncNVAIE reconciles the catalog's nvidia library against the full, verified
// NGC chart list:
//   - Supported charts on deployable paths become owned entries with the Supported
//     chip, taking over any existing entry with the same slug (refresh or promotion).
//   - Other owned entries stay, refreshed and without the chip, while NGC still
//     publishes them on a deployable path; otherwise they are removed.
//   - Unowned entries and the suse-ai library are preserved untouched.
//
// The nvidia array is sorted by slug_name so identical inputs give identical
// output. The report lists app-level changes and unclassified paths.
func syncNVAIE(catalogJSON []byte, resources []ngcResource, ov overrides) ([]byte, report, error) {
	// The tool round-trips the file through catalogDoc's fixed fields, so an
	// unrecognized top-level library key would be silently dropped on re-marshal.
	// Fail loudly instead: add a field to catalogDoc before regenerating.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(catalogJSON, &keys); err != nil {
		return nil, report{}, fmt.Errorf("parse catalog keys: %w", err)
	}
	for k := range keys {
		if k != librarySuseAI && k != libraryNVIDIA {
			return nil, report{}, fmt.Errorf(
				"unrecognized top-level catalog library %q: add it to catalogDoc before regenerating", k)
		}
	}
	var doc catalogDoc
	if err := json.Unmarshal(catalogJSON, &doc); err != nil {
		return nil, report{}, fmt.Errorf("parse catalog: %w", err)
	}

	st, err := indexNGC(resources, ov)
	if err != nil {
		return nil, report{}, err
	}
	labeledSlugs := make(map[string]bool, len(st.labeled))
	for i := range st.labeled {
		labeledSlugs[st.labeled[i].SlugName] = true
	}

	before := make(map[string]catalog.Item, len(doc.NVIDIA))
	reasons := map[string]string{}
	result := make([]catalog.Item, 0, len(doc.NVIDIA)+len(st.labeled))
	for _, e := range doc.NVIDIA {
		before[e.SlugName] = e
		switch {
		case labeledSlugs[e.SlugName]:
			// Replaced by the freshly built supported entry below.
		case !isOwned(e):
			result = append(result, e)
		default:
			it, keep, reason, err := reconcileOwned(e, st, ov)
			if err != nil {
				return nil, report{}, err
			}
			if keep {
				result = append(result, it)
			} else {
				reasons[e.SlugName] = reason
			}
		}
	}
	result = append(result, st.labeled...)
	// Defensive: collapse any residual slug collision among preserved entries.
	result = dedupeBySlug(result)
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].SlugName < result[j].SlugName
	})
	doc.NVIDIA = result

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, report{}, err
	}
	return append(b, '\n'), buildReport(before, result, reasons, st.unclassified), nil
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
