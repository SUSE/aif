package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
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

// parseResources returns the HELM_CHART group's resources, deduped by ResourceID.
func parseResources(body []byte) ([]ngcResource, error) {
	var resp ngcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse NGC response: %w", err)
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
	return out, nil
}

// matchKey normalizes a repository URL + chart name into an "org[/team]/chart" key
// comparable to an NGC resourceId.
func matchKey(repositoryURL, chart string) string {
	path := strings.TrimSpace(repositoryURL)
	if u, err := url.Parse(path); err == nil && u.Host != "" {
		path = u.Path
	} else {
		path = strings.TrimPrefix(strings.TrimPrefix(path, "https://"), "http://")
		if i := strings.IndexByte(path, '/'); i >= 0 {
			path = path[i:]
		} else {
			path = ""
		}
	}
	key := strings.Trim(path, "/") + "/" + chart
	return strings.ToLower(strings.Trim(key, "/"))
}
