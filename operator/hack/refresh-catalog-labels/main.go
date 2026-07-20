// Command refresh-catalog-labels regenerates the `labels` arrays in the operator's
// bundled default-catalog.json from the NGC catalog search API. Manual: a developer
// runs it and commits the result. See README.md.
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
	"strings"
	"time"
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

	names := programDisplayNames()
	hidden := hiddenPrograms()
	byKey := make(map[string][]catalogLabel, len(resources))
	needNames := map[string]bool{}
	for _, res := range resources {
		labels, need := programLabels(res, names, hidden)
		if len(labels) > 0 {
			byKey[strings.ToLower(res.ResourceID)] = labels
		}
		for _, c := range need {
			needNames[c] = true
		}
	}

	catIn, err := os.ReadFile(*catalogPath)
	if err != nil {
		log.Fatalf("read catalog: %v", err)
	}
	out, unmatched, err := applyLabels(catIn, byKey)
	if err != nil {
		log.Fatalf("apply labels: %v", err)
	}
	if err := os.WriteFile(*catalogPath, out, 0o644); err != nil {
		log.Fatalf("write catalog: %v", err)
	}

	for _, slug := range unmatched {
		log.Printf("note: no NGC program labels for catalog entry %q (left without labels)", slug)
	}
	for code := range needNames {
		log.Printf("note: program code %q has no display name; "+
			"add it to programDisplayNames() for a nicer label", code)
	}
	fmt.Printf("updated %s (%d NGC resources, %d labeled keys, %d entries without labels, %d codes need a display name)\n",
		*catalogPath, len(resources), len(byKey), len(unmatched), len(needNames))
}

// fetchAllResources pages through the match-all HELM_CHART search until all resources
// are collected. Note: matchKey in applyLabels compares against res.ResourceID; here we
// key byKey on the lower-cased ResourceID to match.
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
