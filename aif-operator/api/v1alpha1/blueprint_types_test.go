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

func TestBlueprintLifecycleJSON(t *testing.T) {
	// Full lifecycle with all fields set round-trips through JSON
	in := BlueprintLifecycle{
		Install: &LifecycleInstall{
			Strategy:         "parallel",
			PreflightRequired: true,
		},
		Upgrade: &LifecycleUpgrade{
			Strategy:        "safe",
			RequiresApproval: true,
		},
		Delete: &LifecycleDelete{
			RetainResources: []RetainResource{
				{Kind: "PersistentVolumeClaim", Reason: "preserve user data"},
				{Kind: "ConfigMap"},
			},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out BlueprintLifecycle
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Install == nil || out.Install.Strategy != "parallel" {
		t.Errorf("expected Install.Strategy round-trip, got %v", out.Install)
	}
	if out.Upgrade == nil || out.Upgrade.Strategy != "safe" {
		t.Errorf("expected Upgrade.Strategy round-trip, got %v", out.Upgrade)
	}
	if out.Delete == nil || len(out.Delete.RetainResources) != 2 {
		t.Errorf("expected Delete.RetainResources round-trip, got %v", out.Delete)
	}

	// Empty lifecycle omits all optional fields
	empty, _ := json.Marshal(BlueprintLifecycle{})
	if string(empty) == "" || jsonHasKey(empty, "install") || jsonHasKey(empty, "upgrade") || jsonHasKey(empty, "delete") {
		t.Errorf("expected all fields omitted when empty, got %s", empty)
	}
}

func TestLifecycleInstallJSON(t *testing.T) {
	// Set values round-trip
	in := LifecycleInstall{Strategy: "ordered", PreflightRequired: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out LifecycleInstall
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Strategy != "ordered" || out.PreflightRequired != true {
		t.Errorf("expected fields round-trip, got %+v", out)
	}

	// Empty LifecycleInstall omits optional fields
	empty, _ := json.Marshal(LifecycleInstall{})
	if string(empty) == "" || jsonHasKey(empty, "strategy") || jsonHasKey(empty, "preflightRequired") {
		t.Errorf("expected empty fields omitted, got %s", empty)
	}
}

func TestLifecycleUpgradeJSON(t *testing.T) {
	// Set values round-trip
	in := LifecycleUpgrade{Strategy: "force", RequiresApproval: false}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out LifecycleUpgrade
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Strategy != "force" || out.RequiresApproval != false {
		t.Errorf("expected fields round-trip, got %+v", out)
	}

	// Empty LifecycleUpgrade omits optional fields
	empty, _ := json.Marshal(LifecycleUpgrade{})
	if string(empty) == "" || jsonHasKey(empty, "strategy") || jsonHasKey(empty, "requiresApproval") {
		t.Errorf("expected empty fields omitted, got %s", empty)
	}
}

func TestLifecycleDeleteJSON(t *testing.T) {
	// With RetainResources
	in := LifecycleDelete{
		RetainResources: []RetainResource{
			{Kind: "PersistentVolumeClaim", Reason: "user data"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out LifecycleDelete
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.RetainResources) != 1 || out.RetainResources[0].Kind != "PersistentVolumeClaim" {
		t.Errorf("expected RetainResources round-trip, got %v", out.RetainResources)
	}

	// Empty RetainResources omitted
	empty, _ := json.Marshal(LifecycleDelete{})
	if string(empty) == "" || jsonHasKey(empty, "retainResources") {
		t.Errorf("expected retainResources omitted when empty, got %s", empty)
	}
}

func TestRetainResourceJSON(t *testing.T) {
	// Kind and Reason round-trip
	in := RetainResource{Kind: "Secret", Reason: "API credentials"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out RetainResource
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Kind != "Secret" || out.Reason != "API credentials" {
		t.Errorf("expected fields round-trip, got %+v", out)
	}

	// Empty Reason is omitted
	empty, _ := json.Marshal(RetainResource{Kind: "ConfigMap"})
	if string(empty) == "" || jsonHasKey(empty, "reason") {
		t.Errorf("expected reason omitted when empty, got %s", empty)
	}
}

func TestBlueprintComponentV2FieldsJSON(t *testing.T) {
	// Full v2 component with all optional fields
	in := BlueprintComponent{
		ChartRepo:       "bitnami",
		ChartName:       "nginx",
		ChartVersion:    "1.0.0",
		TargetNamespace: "web-system",
		Type:            ComponentContentTypeKustomize,
		Name:            "web-frontend",
		Kustomize: &KustomizeSource{
			Path:     "/overlays/prod",
			Overlays: []string{"security"},
		},
		DependsOn:        []string{"config-service", "auth-service"},
		ValuesFromInputs: []InputMapping{{Input: "domain", Path: "ingress.hostname"}},
	}
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
	if out.Type != ComponentContentTypeKustomize {
		t.Errorf("expected Type round-trip, got %q", out.Type)
	}
	if out.Name != "web-frontend" {
		t.Errorf("expected Name round-trip, got %q", out.Name)
	}
	if out.Kustomize == nil || out.Kustomize.Path != "/overlays/prod" {
		t.Errorf("expected Kustomize round-trip, got %v", out.Kustomize)
	}
	if len(out.DependsOn) != 2 || out.DependsOn[0] != "config-service" {
		t.Errorf("expected DependsOn round-trip, got %v", out.DependsOn)
	}
	if len(out.ValuesFromInputs) != 1 || out.ValuesFromInputs[0].Input != "domain" {
		t.Errorf("expected ValuesFromInputs round-trip, got %v", out.ValuesFromInputs)
	}

	// Minimal component with only required fields (v1 backward compat)
	minimal, _ := json.Marshal(BlueprintComponent{
		ChartRepo:    "bitnami",
		ChartName:    "nginx",
		ChartVersion: "1.0.0",
	})
	if jsonHasKey(minimal, "type") || jsonHasKey(minimal, "name") || jsonHasKey(minimal, "kustomize") ||
		jsonHasKey(minimal, "manifests") || jsonHasKey(minimal, "git") || jsonHasKey(minimal, "dependsOn") ||
		jsonHasKey(minimal, "valuesFromInputs") {
		t.Errorf("expected v2 fields omitted in minimal component, got %s", minimal)
	}
}

func TestBlueprintComponentWithManifestSourceJSON(t *testing.T) {
	// Component with Manifests source
	in := BlueprintComponent{
		ChartRepo:    "repo",
		ChartName:    "name",
		ChartVersion: "1.0.0",
		Type:         ComponentContentTypeManifests,
		Name:         "manifests-component",
		Manifests: &ManifestSource{
			Path:  "/k8s/base",
			Files: []string{"deploy.yaml", "service.yaml"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out BlueprintComponent
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Type != ComponentContentTypeManifests {
		t.Errorf("expected Type=Manifests, got %q", out.Type)
	}
	if out.Manifests == nil || out.Manifests.Path != "/k8s/base" {
		t.Errorf("expected Manifests round-trip, got %v", out.Manifests)
	}
}

func TestBlueprintComponentWithGitSourceJSON(t *testing.T) {
	// Component with Git source
	in := BlueprintComponent{
		ChartRepo:    "repo",
		ChartName:    "name",
		ChartVersion: "1.0.0",
		Type:         ComponentContentTypeGit,
		Name:         "git-component",
		Git: &BlueprintGitSource{
			RepoURL:  "https://github.com/example/config.git",
			Revision: "v1.0.0",
			Path:     "k8s/overlays/prod",
		},
		DependsOn: []string{"base-component"},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out BlueprintComponent
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Type != ComponentContentTypeGit {
		t.Errorf("expected Type=Git, got %q", out.Type)
	}
	if out.Git == nil || out.Git.RepoURL != "https://github.com/example/config.git" {
		t.Errorf("expected Git round-trip, got %v", out.Git)
	}
}

func TestBlueprintTypesCompileWithLifecycle(t *testing.T) {
	// Verify all new lifecycle types compile and instantiate
	_ = BlueprintLifecycle{}
	_ = LifecycleInstall{Strategy: "ordered"}
	_ = LifecycleUpgrade{Strategy: "safe"}
	_ = LifecycleDelete{}
	_ = RetainResource{Kind: "PersistentVolumeClaim"}
}

func jsonHasKey(b []byte, key string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	_, ok := m[key]
	return ok
}
