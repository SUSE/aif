package main

import (
	"encoding/json"
	"fmt"
)

type ngcLabelGroup struct {
	Key              string   `json:"key"`
	Values           []string `json:"values"`
	UnresolvedValues []string `json:"unresolvedValues"`
}

type ngcAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ngcResource struct {
	ResourceType string          `json:"resourceType"`
	ResourceID   string          `json:"resourceId"`
	Name         string          `json:"name"`
	DisplayName  string          `json:"displayName"`
	Description  string          `json:"description"`
	OrgName      string          `json:"orgName"`
	TeamName     string          `json:"teamName"`
	DateModified string          `json:"dateModified"`
	Labels       []ngcLabelGroup `json:"labels"`
	Attributes   []ngcAttribute  `json:"attributes"`
}

type ngcResponse struct {
	ResultTotal int `json:"resultTotal"`
	Results     []struct {
		GroupValue string        `json:"groupValue"`
		Resources  []ngcResource `json:"resources"`
	} `json:"results"`
}

// parseResources returns the HELM_CHART group's resources (deduped by ResourceID)
// and the resultTotal NGC reports for the query. The count lets the caller detect
// a truncated or degraded response before it silently strips the catalog.
func parseResources(body []byte) (res []ngcResource, resultTotal int, err error) {
	var resp ngcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("parse NGC response: %w", err)
	}
	seen := map[string]bool{}
	var out []ngcResource
	for _, g := range resp.Results {
		if g.GroupValue != helmChart {
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
	return out, resp.ResultTotal, nil
}

// verifyComplete guards against a partial or degraded NGC response silently
// gutting the catalog: NGC reports the total match count for the query, so if we
// collected fewer HELM_CHART resources than it claims (a truncated page walk) or it
// reported none at all (an empty/degraded 200), fail loudly instead of regenerating
// from incomplete data. Reconcile-on-run would otherwise drop every owned entry.
func verifyComplete(collected, resultTotal int) error {
	if resultTotal <= 0 {
		return fmt.Errorf(
			"NGC returned no HELM_CHART results (resultTotal=%d); refusing to regenerate from an empty response",
			resultTotal)
	}
	if collected < resultTotal {
		return fmt.Errorf(
			"incomplete NGC response: collected %d of %d HELM_CHART resources; refusing to regenerate from a partial response",
			collected, resultTotal)
	}
	return nil
}
