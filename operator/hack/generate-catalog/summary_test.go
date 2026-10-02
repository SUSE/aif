package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func emptyReport() report {
	return report{
		Added: []reportApp{}, Removed: []reportApp{}, Delabeled: []reportApp{},
		Relabeled: []reportApp{}, Unclassified: []unclassifiedPath{},
	}
}

func fullReport() report {
	r := emptyReport()
	r.Unclassified = []unclassifiedPath{{Path: "/nvidia/new-team", Charts: []unclassifiedChart{
		{ResourceID: "nvidia/new-team/new-chart", Name: "New Chart", Version: "1.5.0"},
	}}}
	r.Added = []reportApp{{Slug: "added-app", Name: "Added App", RepositoryURL: nvidiaHelmRepo}}
	r.Removed = []reportApp{{Slug: "gone-app", Name: "Gone App", RepositoryURL: nvidiaHelmRepo, Reason: reasonNotOnNGC}}
	r.Delabeled = []reportApp{{Slug: "lost-app", Name: "Lost App", RepositoryURL: nvidiaHelmRepo}}
	r.Relabeled = []reportApp{{Slug: "back-app", Name: "Back App", RepositoryURL: nvidiaHelmRepo}}
	return r
}

func TestRenderSummary_Full(t *testing.T) {
	want := summaryHeader + `

## Unclassified NGC paths (action needed)

` + unclassifiedHelp + `

| Path | Chart | Version |
|---|---|---|
| ` + "`/nvidia/new-team`" + ` | New Chart (` + "`nvidia/new-team/new-chart`" + `) | 1.5.0 |

## Added

- Added App (` + "`added-app`" + `)

## Removed

- Gone App (` + "`gone-app`" + `): no longer published on NGC

## Lost Supported label

- Lost App (` + "`lost-app`" + `)

## Regained Supported label

- Back App (` + "`back-app`" + `)
`
	if got := renderSummary(fullReport()); got != want {
		t.Fatalf("summary mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderSummary_NoFindings(t *testing.T) {
	want := summaryHeader + "\n\n" + noFindings + "\n"
	if got := renderSummary(emptyReport()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderSummary_UnclassifiedOnly(t *testing.T) {
	r := emptyReport()
	r.Unclassified = fullReport().Unclassified
	got := renderSummary(r)
	if !strings.Contains(got, "## Unclassified NGC paths (action needed)") {
		t.Fatalf("missing unclassified section:\n%s", got)
	}
	for _, s := range []string{"## Added", "## Removed", "## Lost Supported label", noFindings} {
		if strings.Contains(got, s) {
			t.Fatalf("unexpected %q in unclassified-only summary:\n%s", s, got)
		}
	}
}

func TestRenderSummary_EscapesTableCells(t *testing.T) {
	r := emptyReport()
	r.Unclassified = []unclassifiedPath{{Path: "/nvidia/x", Charts: []unclassifiedChart{
		{ResourceID: "nvidia/x/c", Name: "A | B\nC", Version: ""},
	}}}
	got := renderSummary(r)
	if !strings.Contains(got, `| A \| B C (`+"`nvidia/x/c`"+`) | unknown |`) {
		t.Fatalf("table cell not escaped:\n%s", got)
	}
}

func TestWriteReport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	if err := writeReport(dir, fullReport()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, fullReport()) {
		t.Fatalf("report.json round-trip = %+v", got)
	}
	md, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(md) != renderSummary(fullReport()) {
		t.Fatal("summary.md does not match renderSummary")
	}
}

func TestWriteReport_EmptyListsAreArrays(t *testing.T) {
	dir := t.TempDir()
	if err := writeReport(dir, emptyReport()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"unclassified": []`) {
		t.Fatalf("empty list must serialize as [] for jq: %s", raw)
	}
}
