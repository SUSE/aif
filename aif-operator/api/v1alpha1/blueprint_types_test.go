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

func jsonHasKey(b []byte, key string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	_, ok := m[key]
	return ok
}
