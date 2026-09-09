// Command generate-catalog regenerates the operator's bundled default-catalog.json
// from the NGC catalog search API. It rebuilds every generator-owned entry (those
// carrying the "Supported" chip) from the current NGC data — refreshing changed
// fields and dropping charts NGC no longer lists — while preserving hand-added
// entries and the suse-ai library. Pinned fields in catalog-overrides.json are
// applied on top of the fresh NGC values. Manual runs and the weekly
// refresh-catalog CI workflow both invoke it; commit the result. See README.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/SUSE/aif-operator/internal/catalog"
)

const ngcSearchBase = "https://api.ngc.nvidia.com/v2/search/catalog/resources/HELM_CHART"

func main() {
	catalogPath := flag.String("catalog", "internal/catalog/default-catalog.json", "path to default-catalog.json")
	overridesPath := flag.String("overrides", "internal/catalog/catalog-overrides.json",
		"path to catalog-overrides.json (pinned fields, generator-only input)")
	pageSize := flag.Int("page-size", 100, "NGC search page size")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resources, err := fetchAllResources(ctx, *pageSize)
	if err != nil {
		log.Fatalf("fetch NGC catalog: %v", err)
	}

	var derived []catalog.Item
	var skippedExcluded, skippedUnknown int
	for _, res := range resources {
		if !isNVAIE(res) {
			continue
		}
		switch kind := catalog.ClassifyNGCPath(ngcRepoPath(res)); kind {
		case catalog.NGCPathExcluded:
			skippedExcluded++
		case catalog.NGCPathUnknown:
			skippedUnknown++
			log.Printf("warning: NVAIE chart %q is under unclassified NGC path %q; "+
				"add it to ngc_repos.go before it can be listed", res.ResourceID, ngcRepoPath(res))
		default: // org, public, gated → deployable
			derived = append(derived, deriveItem(res))
		}
	}

	catIn, err := os.ReadFile(*catalogPath)
	if err != nil {
		log.Fatalf("read catalog: %v", err)
	}

	// The overrides file is optional: an absent file means no pinned fields, so a
	// fresh checkout without it still runs.
	ovRaw, err := os.ReadFile(*overridesPath)
	if err != nil && !os.IsNotExist(err) {
		log.Fatalf("read overrides: %v", err)
	}
	ov, err := loadOverrides(ovRaw)
	if err != nil {
		log.Fatalf("load overrides: %v", err)
	}

	out, added, removed, err := syncNVAIE(catIn, derived, ov)
	if err != nil {
		log.Fatalf("sync catalog: %v", err)
	}
	if err := os.WriteFile(*catalogPath, out, 0o644); err != nil {
		log.Fatalf("write catalog: %v", err)
	}

	fmt.Printf("updated %s (%d NGC resources, %d NVAIE deployable, %d added, %d removed, "+
		"%d skipped excluded, %d skipped unclassified)\n",
		*catalogPath, len(resources), len(derived), len(added), len(removed), skippedExcluded, skippedUnknown)
	for _, slug := range added {
		log.Printf("added: %s", slug)
	}
	for _, slug := range removed {
		log.Printf("removed: %s", slug)
	}
}

// fetchAllResources pages through the match-all HELM_CHART search until a page
// returns no new resources, then verifies the collected count against the
// resultTotal NGC reports so a truncated or degraded response fails loudly rather
// than silently regenerating a stripped catalog.
func fetchAllResources(ctx context.Context, pageSize int) ([]ngcResource, error) {
	var all []ngcResource
	seen := map[string]bool{}
	resultTotal := 0
	for page := 0; ; page++ {
		body, err := fetchPage(ctx, page, pageSize)
		if err != nil {
			return nil, err
		}
		res, total, err := parseResources(body)
		if err != nil {
			return nil, err
		}
		if page == 0 {
			resultTotal = total // NGC reports the query-wide total on every page.
		}
		added := 0
		for _, r := range res {
			if seen[r.ResourceID] {
				continue
			}
			seen[r.ResourceID] = true
			all = append(all, r)
			added++
		}
		if added == 0 {
			break
		}
	}
	if err := verifyComplete(len(all), resultTotal); err != nil {
		return nil, err
	}
	return all, nil
}

func fetchPage(ctx context.Context, page, pageSize int) ([]byte, error) {
	q := fmt.Sprintf(
		`{"query":"*","page":%d,"pageSize":%d,"filters":[{"field":"resourceType","value":"HELM_CHART"}]}`,
		page, pageSize,
	)
	u := ngcSearchBase + "?q=" + url.QueryEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}
