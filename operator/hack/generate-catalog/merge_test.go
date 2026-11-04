package main

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/SUSE/aif-operator/internal/catalog"
)

const (
	multimodalSafetyNIMSlug = "multimodal-safety-nim"
	deepstreamITSSlug       = "deepstream-its"
	gpuOperatorSlug         = "gpu-operator"
	gpuOperatorName         = "GPU Operator"
	gpuOperatorRenamed      = "GPU Operator (renamed)"
	embedSlug               = "embed"
	chartVersion            = "1.0.0" // version every chart fixture reports
	nvidiaHelmRepo          = "https://helm.ngc.nvidia.com/nvidia"
	nimHelmRepo             = "https://helm.ngc.nvidia.com/nim/nvidia"
	nemoHelmRepo            = "https://helm.ngc.nvidia.com/nvidia/nemo-microservices"
	unclassifiedTeam        = "zz-unclassified" // /nvidia/zz-unclassified is in no ngc_repos.go map
	unclassifiedPathName    = "/nvidia/" + unclassifiedTeam
)

// baseCatalog holds one suse-ai entry (Milvus), one generator-owned nvidia entry
// (gpu-operator: source ngc, Supported chip), and one hand-added unowned nvidia
// entry (deepstream-its: no source, no label).
const baseCatalog = `{
  "suse-ai": [
    {
      "name":"Milvus",
      "slug_name":"milvus",
      "packaging_format":"HELM_CHART",
      "repository_url":"oci://dp.apps.rancher.io/charts",
      "labels":[{"code":"supported","name":"Supported"}]
    }
  ],
  "nvidia": [
    {
      "name":"GPU Operator",
      "slug_name":"gpu-operator",
      "packaging_format":"HELM_CHART",
      "repository_url":"https://helm.ngc.nvidia.com/nvidia",
      "labels":[{"code":"supported","name":"Supported"}],
      "source":"ngc"
    },
    {
      "name":"DeepStream ITS",
      "slug_name":"deepstream-its",
      "packaging_format":"HELM_CHART",
      "repository_url":"https://helm.ngc.nvidia.com/nvidia"
    }
  ]
}`

// nvidiaCatalog wraps raw nvidia entries in a catalog document with an empty
// suse-ai library.
func nvidiaCatalog(entries ...string) string {
	return `{"suse-ai":[],"nvidia":[` + strings.Join(entries, ",") + `]}`
}

// chart builds an NGC HELM_CHART resource under org[/team]. supported adds the
// nvaie_supported code; every chart reports version 1.0.0.
func chart(org, team, name, display string, supported bool) ngcResource {
	r := ngcResource{
		ResourceType: helmChart,
		Name:         name,
		DisplayName:  display,
		OrgName:      org,
		TeamName:     team,
		DateModified: "2026-09-01T00:00:00.000Z",
		Attributes:   []ngcAttribute{{Key: ngcLatestVersionAttr, Value: chartVersion}},
	}
	r.ResourceID = ngcResourceKey(r)
	codes := []string{"soln_ai"}
	if supported {
		codes = append(codes, nvaieSupported)
	}
	r.Labels = []ngcLabelGroup{{Key: ngcGroupGeneral, UnresolvedValues: codes}}
	return r
}

func gpuOperator(supported bool) ngcResource {
	return chart("nvidia", "", gpuOperatorSlug, gpuOperatorName, supported)
}

func mustSync(t *testing.T, cat string, res []ngcResource, ov overrides) (catalogDoc, report, []byte) {
	t.Helper()
	return mustSyncDetails(t, cat, res, ov, nil)
}

func mustSyncDetails(
	t *testing.T, cat string, res []ngcResource, ov overrides, details detailsByChart,
) (catalogDoc, report, []byte) {
	t.Helper()
	out, rep, err := syncNVAIE([]byte(cat), res, ov, details)
	if err != nil {
		t.Fatal(err)
	}
	var doc catalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	return doc, rep, out
}

// findNVIDIA returns the single nvidia entry with the given slug, or nil.
func findNVIDIA(doc catalogDoc, slug string) *catalog.Item {
	for i := range doc.NVIDIA {
		if doc.NVIDIA[i].SlugName == slug {
			return &doc.NVIDIA[i]
		}
	}
	return nil
}

func appSlugs(apps []reportApp) []string {
	out := make([]string, 0, len(apps))
	for _, a := range apps {
		out = append(out, a.Slug)
	}
	return out
}

func mustOverrides(t *testing.T, s string) overrides {
	t.Helper()
	ov, err := loadOverrides([]byte(s))
	if err != nil {
		t.Fatalf("loadOverrides: %v", err)
	}
	return ov
}

func TestSyncNVAIE_AddsNewLabeledChart(t *testing.T) {
	res := []ngcResource{
		gpuOperator(true),
		chart("nim", "nvidia", multimodalSafetyNIMSlug, "Multimodal Safety NIM", true),
	}
	doc, rep, out := mustSync(t, baseCatalog, res, overrides{})
	if got := appSlugs(rep.Added); !reflect.DeepEqual(got, []string{multimodalSafetyNIMSlug}) {
		t.Fatalf("added = %v", got)
	}
	if len(rep.Removed) != 0 {
		t.Fatalf("removed = %v", appSlugs(rep.Removed))
	}
	e := findNVIDIA(doc, multimodalSafetyNIMSlug)
	if e == nil {
		t.Fatal("new entry not added")
	}
	if !reflect.DeepEqual(e.Labels, supportedLabel) || e.Source != catalog.SourceNGC {
		t.Fatalf("new entry not owned+supported: labels %+v source %q", e.Labels, e.Source)
	}
	if e.RepositoryURL != nimHelmRepo {
		t.Fatalf("repo = %q", e.RepositoryURL)
	}
	if len(doc.SuseAI) != 1 || doc.SuseAI[0].SlugName != "milvus" {
		t.Fatalf("suse-ai changed: %+v", doc.SuseAI)
	}
	if strings.Index(string(out), `"suse-ai"`) > strings.Index(string(out), `"nvidia"`) {
		t.Fatal("top-level key order must stay suse-ai then nvidia")
	}
}

func TestSyncNVAIE_RefreshesOwnedLabeledEntry(t *testing.T) {
	r := gpuOperator(true)
	r.DisplayName = gpuOperatorRenamed
	r.Description = "fresh description"
	doc, rep, _ := mustSync(t, baseCatalog, []ngcResource{r}, overrides{})
	if !rep.empty() {
		t.Fatalf("refresh must not report app changes: %+v", rep)
	}
	e := findNVIDIA(doc, gpuOperatorSlug)
	if e == nil || e.Name != gpuOperatorRenamed || e.Description != "fresh description" {
		t.Fatalf("owned entry not refreshed: %+v", e)
	}
	if !reflect.DeepEqual(e.Labels, supportedLabel) || e.Source != catalog.SourceNGC {
		t.Fatalf("refreshed entry lost chip/source: %+v", e)
	}
}

func TestSyncNVAIE_OverridePinsFields(t *testing.T) {
	r := gpuOperator(true)
	r.DisplayName = gpuOperatorRenamed
	r.Description = "ngc description"
	ov := mustOverrides(t, `{"gpu-operator":{"description":"pinned description"}}`)
	doc, _, _ := mustSync(t, baseCatalog, []ngcResource{r}, ov)
	e := findNVIDIA(doc, gpuOperatorSlug)
	if e.Description != "pinned description" {
		t.Fatalf("Description not pinned: %q", e.Description)
	}
	if e.Name != gpuOperatorRenamed {
		t.Fatalf("non-pinned Name should refresh: %q", e.Name)
	}
}

func TestSyncNVAIE_KeepsDelabeledChartWithoutChip(t *testing.T) {
	r := gpuOperator(false)
	r.DisplayName = "GPU Operator (fresh)"
	doc, rep, _ := mustSync(t, baseCatalog, []ngcResource{r}, overrides{})
	e := findNVIDIA(doc, gpuOperatorSlug)
	if e == nil {
		t.Fatal("delabeled chart was removed")
	}
	if len(e.Labels) != 0 || e.Source != catalog.SourceNGC {
		t.Fatalf("delabeled entry: labels %+v source %q", e.Labels, e.Source)
	}
	if e.Name != "GPU Operator (fresh)" {
		t.Fatalf("delabeled entry not refreshed: %q", e.Name)
	}
	if got := appSlugs(rep.Delabeled); !reflect.DeepEqual(got, []string{gpuOperatorSlug}) {
		t.Fatalf("delabeled = %v", got)
	}
	if len(rep.Removed) != 0 || len(rep.Added) != 0 || len(rep.Relabeled) != 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestSyncNVAIE_RelabelsChartThatRegainsSupport(t *testing.T) {
	cat := nvidiaCatalog(`{"name":"GPU Operator","slug_name":"gpu-operator",` +
		`"repository_url":"https://helm.ngc.nvidia.com/nvidia","source":"ngc"}`)
	doc, rep, _ := mustSync(t, cat, []ngcResource{gpuOperator(true)}, overrides{})
	e := findNVIDIA(doc, gpuOperatorSlug)
	if e == nil || !reflect.DeepEqual(e.Labels, supportedLabel) {
		t.Fatalf("chip not restored: %+v", e)
	}
	if got := appSlugs(rep.Relabeled); !reflect.DeepEqual(got, []string{gpuOperatorSlug}) {
		t.Fatalf("relabeled = %v", got)
	}
	if len(rep.Delabeled) != 0 || len(rep.Added) != 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestSyncNVAIE_RemovesOwnedChartGoneFromNGC(t *testing.T) {
	doc, rep, _ := mustSync(t, baseCatalog, nil, overrides{})
	if findNVIDIA(doc, gpuOperatorSlug) != nil {
		t.Fatal("unpublished owned chart not removed")
	}
	want := []reportApp{{
		Slug: gpuOperatorSlug, Name: gpuOperatorName, RepositoryURL: nvidiaHelmRepo, Reason: reasonNotOnNGC,
	}}
	if !reflect.DeepEqual(rep.Removed, want) {
		t.Fatalf("removed = %+v", rep.Removed)
	}
}

func TestSyncNVAIE_RemovesOwnedChartOnExcludedPath(t *testing.T) {
	cat := nvidiaCatalog(`{"name":"Snow","slug_name":"snow",` +
		`"repository_url":"https://helm.ngc.nvidia.com/nim/snowflake","source":"ngc"}`)
	res := []ngcResource{chart("nim", "snowflake", "snow", "Snow", false)}
	doc, rep, _ := mustSync(t, cat, res, overrides{})
	if findNVIDIA(doc, "snow") != nil {
		t.Fatal("entry on excluded path not removed")
	}
	if len(rep.Removed) != 1 || rep.Removed[0].Reason != reasonPathPrefix+"/nim/snowflake" {
		t.Fatalf("removed = %+v", rep.Removed)
	}
	if len(rep.Unclassified) != 0 {
		t.Fatalf("excluded paths must not be reported as unclassified: %+v", rep.Unclassified)
	}
}

func TestSyncNVAIE_OwnedChartOnUnclassifiedPathIsRemovedAndReported(t *testing.T) {
	cat := nvidiaCatalog(`{"name":"Dyn","slug_name":"dyn",` +
		`"repository_url":"https://helm.ngc.nvidia.com/nvidia/zz-unclassified",` +
		`"labels":[{"code":"supported","name":"Supported"}],"source":"ngc"}`)
	res := []ngcResource{chart("nvidia", unclassifiedTeam, "dyn", "Dyn", true)}
	doc, rep, _ := mustSync(t, cat, res, overrides{})
	if findNVIDIA(doc, "dyn") != nil {
		t.Fatal("entry on unclassified path not removed")
	}
	if len(rep.Removed) != 1 || rep.Removed[0].Reason != reasonPathPrefix+unclassifiedPathName {
		t.Fatalf("removed = %+v", rep.Removed)
	}
	want := []unclassifiedPath{{Path: unclassifiedPathName, Charts: []unclassifiedChart{{
		ResourceID: "nvidia/zz-unclassified/dyn", Name: "Dyn", Version: chartVersion,
	}}}}
	if !reflect.DeepEqual(rep.Unclassified, want) {
		t.Fatalf("unclassified = %+v", rep.Unclassified)
	}
}

func TestSyncNVAIE_ReportsUnclassifiedLabeledCharts(t *testing.T) {
	res := []ngcResource{
		gpuOperator(true),
		chart("nvidia", unclassifiedTeam, "z-chart", "Z Chart", true),
		chart("nvidia", unclassifiedTeam, "a-chart", "A Chart", true),
		chart("nvidia", unclassifiedTeam, "plain", "Plain", false), // not labeled: not reported
	}
	doc, rep, _ := mustSync(t, baseCatalog, res, overrides{})
	if findNVIDIA(doc, "a-chart") != nil || findNVIDIA(doc, "z-chart") != nil {
		t.Fatal("charts on unclassified paths must not be listed")
	}
	want := []unclassifiedPath{{Path: unclassifiedPathName, Charts: []unclassifiedChart{
		{ResourceID: "nvidia/zz-unclassified/a-chart", Name: "A Chart", Version: chartVersion},
		{ResourceID: "nvidia/zz-unclassified/z-chart", Name: "Z Chart", Version: chartVersion},
	}}}
	if !reflect.DeepEqual(rep.Unclassified, want) {
		t.Fatalf("unclassified = %+v", rep.Unclassified)
	}
	if len(rep.Added) != 0 || len(rep.Removed) != 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestSyncNVAIE_RepoMove(t *testing.T) {
	cat := nvidiaCatalog(`{"name":"Embed","slug_name":"embed",` +
		`"repository_url":"https://helm.ngc.nvidia.com/nvidia/nemo-microservices",` +
		`"labels":[{"code":"supported","name":"Supported"}],"source":"ngc"}`)

	t.Run("unlabeled at new repo", func(t *testing.T) {
		res := []ngcResource{chart("nim", "nvidia", embedSlug, "Embed", false)}
		doc, rep, _ := mustSync(t, cat, res, overrides{})
		if findNVIDIA(doc, embedSlug) != nil {
			t.Fatal("old-location entry must be removed")
		}
		if len(rep.Removed) != 1 || rep.Removed[0].Reason != reasonNotOnNGC {
			t.Fatalf("removed = %+v", rep.Removed)
		}
	})

	t.Run("labeled at new repo", func(t *testing.T) {
		res := []ngcResource{chart("nim", "nvidia", embedSlug, "Embed", true)}
		doc, rep, _ := mustSync(t, cat, res, overrides{})
		e := findNVIDIA(doc, embedSlug)
		if e == nil || e.RepositoryURL != nimHelmRepo {
			t.Fatalf("entry not moved to the new repo: %+v", e)
		}
		if !rep.empty() {
			t.Fatalf("same slug, still supported: no app change expected: %+v", rep)
		}
	})
}

func TestSyncNVAIE_PreservesUnownedEntries(t *testing.T) {
	// A hand-authored entry may carry the Supported chip; without source it is
	// never touched, even when NGC lists nothing.
	cat := strings.Replace(baseCatalog, `"nvidia": [`, `"nvidia": [
    {
      "name":"Hand Supported",
      "slug_name":"hand-supported",
      "repository_url":"https://helm.ngc.nvidia.com/nvidia",
      "labels":[{"code":"supported","name":"Supported"}]
    },`, 1)
	doc, rep, _ := mustSync(t, cat, nil, overrides{})
	ds := findNVIDIA(doc, deepstreamITSSlug)
	if ds == nil || ds.Name != "DeepStream ITS" || len(ds.Labels) != 0 || ds.Source != "" {
		t.Fatalf("unowned entry modified: %+v", ds)
	}
	hs := findNVIDIA(doc, "hand-supported")
	if hs == nil || !reflect.DeepEqual(hs.Labels, supportedLabel) || hs.Source != "" {
		t.Fatalf("hand-authored supported entry modified: %+v", hs)
	}
	if got := appSlugs(rep.Removed); !reflect.DeepEqual(got, []string{gpuOperatorSlug}) {
		t.Fatalf("removed = %v", got)
	}
}

func TestSyncNVAIE_PromotesUnownedWhenNowSupported(t *testing.T) {
	r := chart("nvidia", "", deepstreamITSSlug, "DeepStream ITS", true)
	r.Description = "now supported"
	doc, rep, _ := mustSync(t, baseCatalog, []ngcResource{gpuOperator(true), r}, overrides{})
	if !rep.empty() {
		t.Fatalf("promotion of a pre-existing slug must not be reported: %+v", rep)
	}
	n := 0
	for _, e := range doc.NVIDIA {
		if e.SlugName == deepstreamITSSlug {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want 1 deepstream-its entry, got %d", n)
	}
	e := findNVIDIA(doc, deepstreamITSSlug)
	if e.Source != catalog.SourceNGC || !reflect.DeepEqual(e.Labels, supportedLabel) {
		t.Fatalf("promoted entry not owned: %+v", e)
	}
	if e.Description != "now supported" {
		t.Fatalf("promoted entry not from NGC: %q", e.Description)
	}
}

func TestSyncNVAIE_DeduplicatesSlugPreferNim(t *testing.T) {
	res := []ngcResource{
		chart("nvidia", "nemo-microservices", embedSlug, "Embed", true),
		chart("nim", "nvidia", embedSlug, "Embed", true),
	}
	doc, _, _ := mustSync(t, baseCatalog, res, overrides{})
	n := 0
	for _, e := range doc.NVIDIA {
		if e.SlugName == embedSlug {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly 1 entry for %q, got %d", embedSlug, n)
	}
	if e := findNVIDIA(doc, embedSlug); e.RepositoryURL != nimHelmRepo {
		t.Fatalf("dedupe kept the wrong repo: %q", e.RepositoryURL)
	}
}

func TestSyncNVAIE_OverrideSurvivesDedupe(t *testing.T) {
	res := []ngcResource{
		chart("nvidia", "nemo-microservices", embedSlug, "Embed", true),
		chart("nim", "nvidia", embedSlug, "Embed", true),
	}
	ov := mustOverrides(t, `{"embed":{"description":"pinned"}}`)
	doc, _, _ := mustSync(t, baseCatalog, res, ov)
	e := findNVIDIA(doc, embedSlug)
	if e == nil || e.RepositoryURL != nimHelmRepo || e.Description != "pinned" {
		t.Fatalf("override lost through dedupe: %+v", e)
	}
}

func TestSyncNVAIE_OverrideDoesNotBlockRemoval(t *testing.T) {
	ov := mustOverrides(t, `{"gpu-operator":{"description":"pinned"}}`)
	doc, rep, _ := mustSync(t, baseCatalog, nil, ov)
	if findNVIDIA(doc, gpuOperatorSlug) != nil {
		t.Fatal("override resurrected an unpublished chart")
	}
	if got := appSlugs(rep.Removed); !reflect.DeepEqual(got, []string{gpuOperatorSlug}) {
		t.Fatalf("removed = %v", got)
	}
}

func TestSyncNVAIE_OverrideCannotStripChipFromLabeled(t *testing.T) {
	ov := mustOverrides(t, `{"gpu-operator":{"labels":[]}}`)
	doc, _, _ := mustSync(t, baseCatalog, []ngcResource{gpuOperator(true)}, ov)
	if e := findNVIDIA(doc, gpuOperatorSlug); !reflect.DeepEqual(e.Labels, supportedLabel) {
		t.Fatalf("override stripped the Supported chip: %+v", e.Labels)
	}
}

func TestSyncNVAIE_OverrideCannotAddChipToDelabeled(t *testing.T) {
	ov := mustOverrides(t, `{"gpu-operator":{"labels":[`+
		`{"code":"supported","name":"Supported"},{"code":"extra","name":"Extra"}]}}`)
	doc, _, _ := mustSync(t, baseCatalog, []ngcResource{gpuOperator(false)}, ov)
	e := findNVIDIA(doc, gpuOperatorSlug)
	want := []catalog.Label{{Code: "extra", Name: "Extra"}}
	if !reflect.DeepEqual(e.Labels, want) {
		t.Fatalf("labels = %+v, want %+v", e.Labels, want)
	}
}

func TestSyncNVAIE_DedupeEqualRankIsDeterministic(t *testing.T) {
	// Two gated, non-nim repos collide on a slug (equal repo rank). Regardless of
	// the order NGC returns them, the lexicographically smaller URL wins.
	const slug = "collide"
	a := chart("nvidia", "riva", slug, "Collide", true)
	b := chart("nvidia", "runai", slug, "Collide", true)
	outs := make([]string, 0, 2)
	for _, order := range [][]ngcResource{{a, b}, {b, a}} {
		doc, _, out := mustSync(t, baseCatalog, append([]ngcResource{gpuOperator(true)}, order...), overrides{})
		if e := findNVIDIA(doc, slug); e == nil || e.RepositoryURL != "https://helm.ngc.nvidia.com/nvidia/riva" {
			t.Fatalf("equal-rank dedupe kept %+v", e)
		}
		outs = append(outs, string(out))
	}
	if outs[0] != outs[1] {
		t.Fatal("dedupe is order-dependent for equal-rank collisions")
	}
}

func TestSyncNVAIE_FailsOnOwnedEntryWithNonNGCURL(t *testing.T) {
	cat := nvidiaCatalog(`{"name":"Odd","slug_name":"odd",` +
		`"repository_url":"oci://example.com/charts","source":"ngc"}`)
	_, _, err := syncNVAIE([]byte(cat), []ngcResource{gpuOperator(true)}, overrides{}, nil)
	if err == nil || !strings.Contains(err.Error(), "non-NGC repository_url") {
		t.Fatalf("want non-NGC repository_url error, got %v", err)
	}
}

func TestSyncNVAIE_RejectsUnknownLibrary(t *testing.T) {
	_, _, err := syncNVAIE([]byte(`{"nvidia":[],"other":[]}`), nil, overrides{}, nil)
	if err == nil || !strings.Contains(err.Error(), "unrecognized top-level catalog library") {
		t.Fatalf("want unknown library error, got %v", err)
	}
}

func TestSyncNVAIE_Idempotent(t *testing.T) {
	cat := strings.Replace(baseCatalog, `"nvidia": [`, `"nvidia": [
    {
      "name":"Old",
      "slug_name":"old",
      "repository_url":"https://helm.ngc.nvidia.com/nvidia",
      "labels":[{"code":"supported","name":"Supported"}],
      "source":"ngc"
    },`, 1)
	res := []ngcResource{
		gpuOperator(true),
		chart("nim", "nvidia", multimodalSafetyNIMSlug, "Multimodal Safety NIM", true),
		chart("nvidia", "", "old", "Old", false), // delabeled on the first run
		chart("nvidia", unclassifiedTeam, "dyn", "Dyn", true),
	}
	ov := mustOverrides(t, `{"gpu-operator":{"description":"pinned"}}`)
	_, _, out1 := mustSync(t, cat, res, ov)
	_, rep2, out2 := mustSync(t, string(out1), res, ov)
	if string(out1) != string(out2) {
		t.Fatal("second run produced a different document (not idempotent)")
	}
	if len(rep2.Added)+len(rep2.Removed)+len(rep2.Delabeled)+len(rep2.Relabeled) != 0 {
		t.Fatalf("second run must report no app changes: %+v", rep2)
	}
	if len(rep2.Unclassified) != 1 {
		t.Fatalf("unclassified paths must be reported on every run: %+v", rep2.Unclassified)
	}
}

const (
	ownSlug    = "own"
	handSlug   = "hand"
	searchDate = "2026-10-01T00:00:00Z" // dateModified the search API reports in enrichment tests
)

func TestSyncNVAIE_AppliesDetailsToOwnedAndHandAdded(t *testing.T) {
	cat := nvidiaCatalog(`{"name":"Hand","slug_name":"hand",` +
		`"repository_url":"https://helm.ngc.nvidia.com/nvidia","documentation_url":"https://d"}`)
	res := []ngcResource{chart("nvidia", "", ownSlug, "Own", true)}
	details := detailsByChart{
		chartKey(nvidiaHelmRepo, ownSlug): {
			Overview: "own overview", LatestVersion: "3.0", UpdatedDate: "2026-10-02T00:00:00Z",
		},
		chartKey(nvidiaHelmRepo, handSlug): {
			Overview: "hand overview", LatestVersion: "4.0", UpdatedDate: "2026-10-03T00:00:00Z",
		},
	}
	doc, _, _ := mustSyncDetails(t, cat, res, overrides{}, details)

	own := findNVIDIA(doc, ownSlug)
	if own == nil || own.Overview != "own overview" || own.LatestVersion != "3.0" ||
		own.LastUpdatedAt != "2026-10-02T00:00:00Z" {
		t.Fatalf("owned entry not enriched: %+v", own)
	}
	hand := findNVIDIA(doc, handSlug)
	if hand == nil || hand.Overview != "hand overview" || hand.LatestVersion != "4.0" ||
		hand.LastUpdatedAt != "2026-10-03T00:00:00Z" {
		t.Fatalf("hand-added entry not enriched: %+v", hand)
	}
	if hand.DocumentationURL != "https://d" || hand.Name != "Hand" || hand.Source != "" {
		t.Fatalf("hand-curated fields changed: %+v", hand)
	}
}

func TestSyncNVAIE_NoDetailsCarriesOverview(t *testing.T) {
	cat := nvidiaCatalog(
		`{"name":"Own","slug_name":"own","repository_url":"https://helm.ngc.nvidia.com/nvidia",`+
			`"overview":"old","latest_version":"1.0","source":"ngc"}`,
		`{"name":"Lost","slug_name":"lost","repository_url":"https://helm.ngc.nvidia.com/nvidia",`+
			`"overview":"lost old","source":"ngc"}`,
		`{"name":"Hand","slug_name":"hand","repository_url":"https://helm.ngc.nvidia.com/nvidia",`+
			`"overview":"h"}`,
	)
	own := chart("nvidia", "", ownSlug, "Own", true)
	own.Attributes = []ngcAttribute{{Key: ngcLatestVersionAttr, Value: "2.0"}}
	own.DateModified = searchDate
	res := []ngcResource{own, chart("nvidia", "", "lost", "Lost", false)}
	doc, _, _ := mustSyncDetails(t, cat, res, overrides{}, nil)

	e := findNVIDIA(doc, ownSlug)
	if e == nil || e.Overview != "old" || e.LatestVersion != "2.0" || e.LastUpdatedAt != searchDate {
		t.Fatalf("owned entry: want carried overview and fresh version/date, got %+v", e)
	}
	if l := findNVIDIA(doc, "lost"); l == nil || l.Overview != "lost old" || len(l.Labels) != 0 {
		t.Fatalf("delabeled owned entry did not carry overview: %+v", l)
	}
	if h := findNVIDIA(doc, handSlug); h == nil || h.Overview != "h" {
		t.Fatalf("hand-added overview changed: %+v", h)
	}
}

func TestSyncNVAIE_OverrideBeatsDetails(t *testing.T) {
	res := []ngcResource{chart("nvidia", "", ownSlug, "Own", true)}
	details := detailsByChart{chartKey(nvidiaHelmRepo, ownSlug): {Overview: "ngc", LatestVersion: "3.0"}}
	ov := mustOverrides(t, `{"own":{"overview":"pinned overview"}}`)
	doc, _, _ := mustSyncDetails(t, nvidiaCatalog(), res, ov, details)
	e := findNVIDIA(doc, ownSlug)
	if e == nil || e.Overview != "pinned overview" || e.LatestVersion != "3.0" {
		t.Fatalf("override must win over details: %+v", e)
	}
}

func TestEnrichmentTargets(t *testing.T) {
	cat := nvidiaCatalog(
		`{"name":"X","slug_name":"x","repository_url":"https://helm.ngc.nvidia.com/nvidia","source":"ngc"}`,
		`{"name":"H","slug_name":"h","repository_url":"https://helm.ngc.nvidia.com/nvidia"}`,
	)
	res := []ngcResource{
		chart("nvidia", "", "d", "D", true),
		chart("nvidia", "", "u", "U", false),
	}
	targets, err := enrichmentTargets([]byte(cat), res)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(targets))
	for _, it := range targets {
		got = append(got, it.SlugName)
	}
	sort.Strings(got)
	if want := []string{"d", "h", "x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("target slugs = %v, want %v", got, want)
	}
}

func TestSyncNVAIE_EmptyDetailFieldsKeepExistingValues(t *testing.T) {
	cat := nvidiaCatalog(
		`{"name":"Own","slug_name":"own","repository_url":"https://helm.ngc.nvidia.com/nvidia",`+
			`"overview":"old","latest_version":"1.0","source":"ngc"}`,
		`{"name":"Hand","slug_name":"hand","repository_url":"https://helm.ngc.nvidia.com/nvidia",`+
			`"overview":"h","latest_version":"1.0","last_updated_at":"2026-01-01T00:00:00Z"}`,
	)
	own := chart("nvidia", "", ownSlug, "Own", true)
	own.Attributes = []ngcAttribute{{Key: ngcLatestVersionAttr, Value: "2.0"}}
	own.DateModified = searchDate
	details := detailsByChart{
		chartKey(nvidiaHelmRepo, ownSlug):  {},
		chartKey(nvidiaHelmRepo, handSlug): {},
	}
	doc, _, _ := mustSyncDetails(t, cat, []ngcResource{own}, overrides{}, details)

	e := findNVIDIA(doc, ownSlug)
	if e == nil || e.Overview != "old" || e.LatestVersion != "2.0" || e.LastUpdatedAt != searchDate {
		t.Fatalf("owned entry: empty details must keep carried overview and search version/date, got %+v", e)
	}
	h := findNVIDIA(doc, handSlug)
	if h == nil || h.Overview != "h" || h.LatestVersion != "1.0" || h.LastUpdatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("hand-added entry changed by empty details: %+v", h)
	}
}
