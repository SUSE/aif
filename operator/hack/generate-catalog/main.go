// Command generate-catalog regenerates the operator's bundled default-catalog.json
// from the NGC catalog search API. It adds every nvaie_supported Helm chart (that
// lives under a deployable NGC repo path) as a catalog entry with a single
// "Supported" chip, preserving all existing entries. Manual runs and the weekly
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
	out, added, err := mergeNVAIE(catIn, derived)
	if err != nil {
		log.Fatalf("merge catalog: %v", err)
	}
	if err := os.WriteFile(*catalogPath, out, 0o644); err != nil {
		log.Fatalf("write catalog: %v", err)
	}

	fmt.Printf("updated %s (%d NGC resources, %d NVAIE deployable, %d newly added, "+
		"%d skipped excluded, %d skipped unclassified)\n",
		*catalogPath, len(resources), len(derived), len(added), skippedExcluded, skippedUnknown)
	for _, slug := range added {
		log.Printf("added: %s", slug)
	}
}

// fetchAllResources pages through the match-all HELM_CHART search until a page
// returns no new resources.
func fetchAllResources(ctx context.Context, pageSize int) ([]ngcResource, error) {
	var all []ngcResource
	seen := map[string]bool{}
	for page := 0; ; page++ {
		body, err := fetchPage(ctx, page, pageSize)
		if err != nil {
			return nil, err
		}
		res, err := parseResources(body)
		if err != nil {
			return nil, err
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
