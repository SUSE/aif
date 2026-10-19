package main

import (
	"sort"

	"github.com/SUSE/aif-operator/internal/catalog"
)

// Reasons recorded on removed apps.
const (
	reasonNotOnNGC   = "no longer published on NGC"
	reasonPathPrefix = "NGC path no longer deployable: "
)

// reportApp is one app-level change in a refresh run.
type reportApp struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	RepositoryURL string `json:"repository_url"`
	Reason        string `json:"reason,omitempty"`
}

// unclassifiedChart is an NVAIE-supported chart left out because its NGC repo
// path is in no ngc_repos.go map.
type unclassifiedChart struct {
	ResourceID string `json:"resource_id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
}

type unclassifiedPath struct {
	Path   string              `json:"path"`
	Charts []unclassifiedChart `json:"charts"`
}

// report summarizes a refresh run for reviewers. Every list is non-nil and sorted
// (apps by slug, paths by path, charts by resource ID) so report.json is stable.
type report struct {
	Added        []reportApp        `json:"added"`
	Removed      []reportApp        `json:"removed"`
	Delabeled    []reportApp        `json:"delabeled"`
	Relabeled    []reportApp        `json:"relabeled"`
	Unclassified []unclassifiedPath `json:"unclassified"`
}

func (r report) empty() bool {
	return len(r.Added) == 0 && len(r.Removed) == 0 && len(r.Delabeled) == 0 &&
		len(r.Relabeled) == 0 && len(r.Unclassified) == 0
}

func appOf(e catalog.Item) reportApp {
	return reportApp{Slug: e.SlugName, Name: e.Name, RepositoryURL: e.RepositoryURL}
}

// buildReport compares the nvidia library before and after a run. Delabeled and
// relabeled only consider entries owned on both sides, so promotion of a
// hand-added entry is not reported as a support change.
func buildReport(
	before map[string]catalog.Item, after []catalog.Item,
	reasons map[string]string, unclassified map[string][]unclassifiedChart,
) report {
	rep := report{
		Added: []reportApp{}, Removed: []reportApp{}, Delabeled: []reportApp{},
		Relabeled: []reportApp{}, Unclassified: []unclassifiedPath{},
	}
	present := make(map[string]bool, len(after))
	for _, e := range after {
		present[e.SlugName] = true
		b, existed := before[e.SlugName]
		switch {
		case !existed:
			rep.Added = append(rep.Added, appOf(e))
		case isOwned(b) && isOwned(e) && hasSupported(b.Labels) && !hasSupported(e.Labels):
			rep.Delabeled = append(rep.Delabeled, appOf(e))
		case isOwned(b) && isOwned(e) && !hasSupported(b.Labels) && hasSupported(e.Labels):
			rep.Relabeled = append(rep.Relabeled, appOf(e))
		}
	}
	for slug, b := range before {
		if present[slug] {
			continue
		}
		a := appOf(b)
		a.Reason = reasons[slug]
		rep.Removed = append(rep.Removed, a)
	}
	for _, apps := range [][]reportApp{rep.Added, rep.Removed, rep.Delabeled, rep.Relabeled} {
		sort.Slice(apps, func(i, j int) bool { return apps[i].Slug < apps[j].Slug })
	}
	for path, charts := range unclassified {
		sort.Slice(charts, func(i, j int) bool { return charts[i].ResourceID < charts[j].ResourceID })
		rep.Unclassified = append(rep.Unclassified, unclassifiedPath{Path: path, Charts: charts})
	}
	sort.Slice(rep.Unclassified, func(i, j int) bool { return rep.Unclassified[i].Path < rep.Unclassified[j].Path })
	return rep
}
