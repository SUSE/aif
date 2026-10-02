package main

import (
	"encoding/json"
	"reflect"
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
	out, rep, err := syncNVAIE([]byte(cat), res, ov)
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

func TestSyncNVAIE_OverrideCannotChangeSource(t *testing.T) {
	ov := mustOverrides(t, `{"gpu-operator":{"source":""}}`)
	doc, _, _ := mustSync(t, baseCatalog, []ngcResource{gpuOperator(true)}, ov)
	if e := findNVIDIA(doc, gpuOperatorSlug); e.Source != catalog.SourceNGC {
		t.Fatalf("override changed ownership: %q", e.Source)
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
	_, _, err := syncNVAIE([]byte(cat), []ngcResource{gpuOperator(true)}, overrides{})
	if err == nil || !strings.Contains(err.Error(), "non-NGC repository_url") {
		t.Fatalf("want non-NGC repository_url error, got %v", err)
	}
}

func TestSyncNVAIE_RejectsUnknownLibrary(t *testing.T) {
	_, _, err := syncNVAIE([]byte(`{"nvidia":[],"other":[]}`), nil, overrides{})
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
