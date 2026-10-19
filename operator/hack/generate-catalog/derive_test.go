package main

import "testing"

const (
	multimodalSafetyDisplayName = "Helm Chart for NVIDIA NIM for Multimodal Safety"
	ngcGroupGeneral             = "general" // NGC's catch-all label group key
)

func nvaieRes() ngcResource {
	return ngcResource{
		ResourceType: helmChart,
		ResourceID:   "nim/nvidia/multimodal-safety-nim",
		Name:         "multimodal-safety-nim",
		DisplayName:  multimodalSafetyDisplayName,
		Description:  multimodalSafetyDisplayName,
		OrgName:      "nim",
		TeamName:     "nvidia",
		DateModified: "2025-01-24T05:09:36.845Z",
		Labels: []ngcLabelGroup{{
			Key:              ngcGroupGeneral,
			UnresolvedValues: []string{"NIM", nvaieSupported, "NSPECT-Y18F-V3IX"},
		}},
		Attributes: []ngcAttribute{{Key: "logo", Value: "https://example.com/logo.jpg"}},
	}
}

func TestIsNVAIE(t *testing.T) {
	if !isNVAIE(nvaieRes()) {
		t.Fatal("nvaie_supported chart should be selected")
	}
	notSupported := ngcResource{Labels: []ngcLabelGroup{{
		Key: ngcGroupGeneral, UnresolvedValues: []string{"omniverse_supported"},
	}}}
	if isNVAIE(notSupported) {
		t.Fatal("omniverse_supported (not nvaie) must not be selected")
	}
}

func TestDeriveItem(t *testing.T) {
	got := deriveItem(nvaieRes())
	if got.Name != multimodalSafetyDisplayName {
		t.Errorf("Name = %q", got.Name)
	}
	if got.SlugName != "multimodal-safety-nim" {
		t.Errorf("SlugName = %q", got.SlugName)
	}
	if got.RepositoryURL != "https://helm.ngc.nvidia.com/nim/nvidia" {
		t.Errorf("RepositoryURL = %q", got.RepositoryURL)
	}
	if got.LogoURL != "https://example.com/logo.jpg" {
		t.Errorf("LogoURL = %q", got.LogoURL)
	}
	if got.LastUpdatedAt != "2025-01-24T05:09:36.845Z" {
		t.Errorf("LastUpdatedAt = %q", got.LastUpdatedAt)
	}
	if got.PackagingFormat != helmChart {
		t.Errorf("PackagingFormat = %q", got.PackagingFormat)
	}
	// NGC never supplies these — must be empty.
	if got.ProjectURL != "" || got.DocumentationURL != "" || got.SourceCodeURL != "" ||
		got.ChangelogURL != "" || got.ReferenceGuideURL != "" {
		t.Errorf("URL fields should be empty, got %+v", got)
	}
	// deriveItem does not set labels; merge does.
	if got.Labels != nil {
		t.Errorf("deriveItem must not set labels, got %+v", got.Labels)
	}
}

func TestNGCRepoURL_NoTeam(t *testing.T) {
	r := ngcResource{OrgName: "nvidia"}
	if got := ngcRepoURL(r); got != "https://helm.ngc.nvidia.com/nvidia" {
		t.Errorf("ngcRepoURL no-team = %q", got)
	}
	if got := ngcRepoPath(r); got != "/nvidia" {
		t.Errorf("ngcRepoPath no-team = %q", got)
	}
}

func TestNGCResourceKey(t *testing.T) {
	if got := ngcResourceKey(nvaieRes()); got != "nim/nvidia/multimodal-safety-nim" {
		t.Errorf("team key = %q", got)
	}
	if got := ngcResourceKey(ngcResource{OrgName: "nvidia", Name: "gpu-operator"}); got != "nvidia/gpu-operator" {
		t.Errorf("no-team key = %q", got)
	}
}

func TestNGCLatestVersion(t *testing.T) {
	r := ngcResource{Attributes: []ngcAttribute{{Key: ngcLatestVersionAttr, Value: " 1.5.0 "}}}
	if got := ngcLatestVersion(r); got != "1.5.0" {
		t.Errorf("version = %q", got)
	}
	if got := ngcLatestVersion(ngcResource{}); got != "" {
		t.Errorf("missing version = %q", got)
	}
}
