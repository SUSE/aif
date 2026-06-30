package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestBlueprintTypesCompile(t *testing.T) {
	_ = Blueprint{}
	_ = BlueprintList{}
	_ = BlueprintComponent{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0"}
	_ = BlueprintSpec{
		DisplayName: "d",
		Version:     "1.0.0",
		Source:      BlueprintOriginCustom,
		Components:  []BlueprintComponent{{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0"}},
	}
	_ = BlueprintNameLabel
	_ = BlueprintVersionLabel
	_ = BlueprintOriginSUSE
	_ = BlueprintOriginNvidia
	_ = BlueprintOriginCustom
	// v2 preview content types
	_ = ComponentContentTypeHelm
	_ = ComponentContentTypeKustomize
	_ = ComponentContentTypeManifests
	_ = ComponentContentTypeGit
	_ = KustomizeSource{Path: "/path"}
	_ = ManifestSource{Path: "/path"}
	_ = BlueprintGitSource{RepoURL: "https://example.com/repo.git"}
}

func TestBlueprintComponentTargetNamespaceJSON(t *testing.T) {
	// Set value round-trips through JSON under the "targetNamespace" key.
	in := BlueprintComponent{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0", TargetNamespace: "ai-system"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out BlueprintComponent
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.TargetNamespace != "ai-system" {
		t.Errorf("expected targetNamespace round-trip, got %q from %s", out.TargetNamespace, b)
	}

	// Empty value is omitted from JSON (omitempty).
	empty, _ := json.Marshal(BlueprintComponent{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0"})
	if string(empty) == "" || jsonHasKey(empty, "targetNamespace") {
		t.Errorf("expected targetNamespace omitted when empty, got %s", empty)
	}
}

func TestKustomizeSourceJSON(t *testing.T) {
	// Set value round-trips through JSON
	in := KustomizeSource{Path: "/path/to/overlay", Overlays: []string{"prod", "staging"}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out KustomizeSource
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Path != "/path/to/overlay" {
		t.Errorf("expected Path round-trip, got %q", out.Path)
	}
	if len(out.Overlays) != 2 || out.Overlays[0] != "prod" {
		t.Errorf("expected Overlays round-trip, got %v", out.Overlays)
	}

	// Empty Overlays is omitted from JSON (omitempty)
	empty, _ := json.Marshal(KustomizeSource{Path: "/path"})
	if string(empty) == "" || jsonHasKey(empty, "overlays") {
		t.Errorf("expected overlays omitted when empty, got %s", empty)
	}
}

func TestManifestSourceJSON(t *testing.T) {
	// Set value round-trips through JSON
	in := ManifestSource{Path: "/path/to/manifests", Files: []string{"config.yaml", "deploy.yaml"}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out ManifestSource
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Path != "/path/to/manifests" {
		t.Errorf("expected Path round-trip, got %q", out.Path)
	}
	if len(out.Files) != 2 || out.Files[0] != "config.yaml" {
		t.Errorf("expected Files round-trip, got %v", out.Files)
	}

	// Empty Files is omitted from JSON (omitempty)
	empty, _ := json.Marshal(ManifestSource{Path: "/path"})
	if string(empty) == "" || jsonHasKey(empty, "files") {
		t.Errorf("expected files omitted when empty, got %s", empty)
	}
}

func TestBlueprintGitSourceJSON(t *testing.T) {
	// Set value round-trips through JSON
	in := BlueprintGitSource{RepoURL: "https://example.com/repo.git", Revision: "main", Path: "k8s/"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out BlueprintGitSource
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.RepoURL != "https://example.com/repo.git" {
		t.Errorf("expected RepoURL round-trip, got %q", out.RepoURL)
	}
	if out.Revision != "main" {
		t.Errorf("expected Revision round-trip, got %q", out.Revision)
	}
	if out.Path != "k8s/" {
		t.Errorf("expected Path round-trip, got %q", out.Path)
	}

	// Empty optional fields are omitted from JSON (omitempty)
	empty, _ := json.Marshal(BlueprintGitSource{RepoURL: "https://example.com/repo.git"})
	if string(empty) == "" || jsonHasKey(empty, "revision") || jsonHasKey(empty, "path") {
		t.Errorf("expected revision,path omitted when empty, got %s", empty)
	}
}

func jsonHasKey(b []byte, key string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	_, ok := m[key]
	return ok
}
