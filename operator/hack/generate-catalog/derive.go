package main

import (
	"strings"

	"github.com/SUSE/aif-operator/internal/catalog"
)

const (
	ngcHelmBase = "https://helm.ngc.nvidia.com"

	// helmChart is the NGC resourceType / search group value for Helm charts.
	helmChart = "HELM_CHART"

	// nvaieSupported is the NGC label code marking a resource as NVIDIA AI
	// Enterprise supported — the sole selection gate for catalog inclusion.
	nvaieSupported = "nvaie_supported"
)

// isNVAIE reports whether a resource carries the nvaie_supported designation
// (in any label group's unresolved values). This is the sole selection gate.
func isNVAIE(res ngcResource) bool {
	for _, g := range res.Labels {
		for _, code := range g.UnresolvedValues {
			if code == nvaieSupported {
				return true
			}
		}
	}
	return false
}

// ngcRepoPath returns the URL path of a resource's NGC Helm repo, e.g.
// "/nim/nvidia" or "/nvidia" when there is no team.
func ngcRepoPath(res ngcResource) string {
	p := "/" + res.OrgName
	if res.TeamName != "" {
		p += "/" + res.TeamName
	}
	return p
}

// ngcRepoURL is the full https NGC Helm repo URL for a resource.
func ngcRepoURL(res ngcResource) string {
	return ngcHelmBase + ngcRepoPath(res)
}

// ngcLogoURL returns the resource's logo attribute value, or "" if absent.
func ngcLogoURL(res ngcResource) string {
	for _, a := range res.Attributes {
		if a.Key == "logo" {
			return strings.TrimSpace(a.Value)
		}
	}
	return ""
}

// deriveItem builds a catalog entry from the NGC-only fields the anonymous search
// API provides. It does NOT set Labels (merge stamps the single "Supported" chip)
// and leaves the five URL fields NGC cannot supply empty.
func deriveItem(res ngcResource) catalog.Item {
	return catalog.Item{
		Name:            res.DisplayName,
		SlugName:        res.Name,
		Description:     res.Description,
		LogoURL:         ngcLogoURL(res),
		LastUpdatedAt:   res.DateModified,
		PackagingFormat: helmChart,
		RepositoryURL:   ngcRepoURL(res),
	}
}
