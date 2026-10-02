package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	summaryHeader    = "Automated NVAIE static catalog refresh by the refresh-catalog workflow."
	unclassifiedHelp = "These NVAIE-supported charts are under NGC repo paths missing from " +
		"`operator/internal/catalog/ngc_repos.go`, so they are not listed in the catalog. " +
		"Add each path to the matching map there (org, public, gated or excluded), then re-run the workflow."
	noFindings = "No apps were added, removed, or changed support status."
)

// renderSummary renders the report as markdown for the refresh PR body and the
// job summary. Only non-empty sections are emitted.
func renderSummary(r report) string {
	var b strings.Builder
	b.WriteString(summaryHeader + "\n")
	if len(r.Unclassified) > 0 {
		b.WriteString("\n## Unclassified NGC paths (action needed)\n\n" + unclassifiedHelp + "\n\n")
		b.WriteString("| Path | Chart | Version |\n|---|---|---|\n")
		for _, p := range r.Unclassified {
			for _, c := range p.Charts {
				version := c.Version
				if version == "" {
					version = "unknown"
				}
				fmt.Fprintf(&b, "| `%s` | %s (`%s`) | %s |\n", p.Path, tableCell(c.Name), c.ResourceID, tableCell(version))
			}
		}
	}
	writeApps(&b, "Added", r.Added)
	writeApps(&b, "Removed", r.Removed)
	writeApps(&b, "Lost Supported label", r.Delabeled)
	writeApps(&b, "Regained Supported label", r.Relabeled)
	if r.empty() {
		b.WriteString("\n" + noFindings + "\n")
	}
	return b.String()
}

func writeApps(b *strings.Builder, title string, apps []reportApp) {
	if len(apps) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for _, a := range apps {
		fmt.Fprintf(b, "- %s (`%s`)", a.Name, a.Slug)
		if a.Reason != "" {
			fmt.Fprintf(b, ": %s", a.Reason)
		}
		b.WriteString("\n")
	}
}

// tableCell keeps NGC-supplied text from breaking a markdown table row.
func tableCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.Join(strings.Fields(s), " ")
}

// writeReport writes report.json and summary.md into dir, creating it if needed.
func writeReport(dir string, r report) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	j, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), append(j, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "summary.md"), []byte(renderSummary(r)), 0o644)
}
