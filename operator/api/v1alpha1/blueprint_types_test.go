/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
