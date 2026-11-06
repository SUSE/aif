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

func TestComponentValueOverrideDependsOnJSON(t *testing.T) {
	// Absent DependsOn marshals without a "dependsOn" key.
	absent := ComponentValueOverride{ComponentName: "a"}
	absentJSON, err := json.Marshal(absent)
	if err != nil {
		t.Fatalf("marshal absent: %v", err)
	}
	if jsonHasKey(absentJSON, "dependsOn") {
		t.Errorf("expected absent DependsOn to be omitted, got %s", absentJSON)
	}

	// &[]string{} marshals as "dependsOn":[]
	emptySlice := ComponentValueOverride{ComponentName: "a", DependsOn: &[]string{}}
	emptyJSON, err := json.Marshal(emptySlice)
	if err != nil {
		t.Fatalf("marshal empty slice: %v", err)
	}
	if !jsonHasKey(emptyJSON, "dependsOn") {
		t.Errorf("expected empty slice to include dependsOn key, got %s", emptyJSON)
	}
	var emptyOut ComponentValueOverride
	if err := json.Unmarshal(emptyJSON, &emptyOut); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if emptyOut.DependsOn == nil || len(*emptyOut.DependsOn) != 0 {
		t.Errorf("expected non-nil pointer to empty slice, got %+v", emptyOut.DependsOn)
	}

	// Unmarshalling {"componentName":"a","dependsOn":[]} yields non-nil pointer to empty slice.
	var explicitEmpty ComponentValueOverride
	if err := json.Unmarshal([]byte(`{"componentName":"a","dependsOn":[]}`), &explicitEmpty); err != nil {
		t.Fatalf("unmarshal explicit empty: %v", err)
	}
	if explicitEmpty.DependsOn == nil {
		t.Errorf("expected non-nil pointer when unmarshalling empty array, got nil")
	}
	if len(*explicitEmpty.DependsOn) != 0 {
		t.Errorf("expected empty slice, got %+v", *explicitEmpty.DependsOn)
	}

	// Unmarshalling {"componentName":"a"} yields nil.
	var nilOut ComponentValueOverride
	if err := json.Unmarshal([]byte(`{"componentName":"a"}`), &nilOut); err != nil {
		t.Fatalf("unmarshal nil: %v", err)
	}
	if nilOut.DependsOn != nil {
		t.Errorf("expected nil DependsOn when unmarshalling without key, got %+v", nilOut.DependsOn)
	}
}
