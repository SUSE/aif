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

package aiworkload

import (
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func bp(comps ...aiplatformv1alpha1.BlueprintComponent) *aiplatformv1alpha1.Blueprint {
	return &aiplatformv1alpha1.Blueprint{Spec: aiplatformv1alpha1.BlueprintSpec{Components: comps}}
}
func comp(name string, deps ...string) aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{ChartRepo: "r", ChartName: name, ChartVersion: "1.0.0", DependsOn: deps}
}

func TestValidateDependsOnUnknownRef(t *testing.T) {
	err := validateDependsOn(bp(comp("a", "ghost")), &aiplatformv1alpha1.AIWorkload{})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected unknown-ref error, got %v", err)
	}
}
func TestValidateDependsOnSelfRef(t *testing.T) {
	if validateDependsOn(bp(comp("a", "a")), &aiplatformv1alpha1.AIWorkload{}) == nil {
		t.Fatalf("expected self-ref error")
	}
}
func TestValidateDependsOnCycle(t *testing.T) {
	err := validateDependsOn(bp(comp("a", "b"), comp("b", "c"), comp("c", "a")), &aiplatformv1alpha1.AIWorkload{})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}
func TestValidateDependsOnDisabledTarget(t *testing.T) {
	w := &aiplatformv1alpha1.AIWorkload{Spec: aiplatformv1alpha1.AIWorkloadSpec{
		ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "b", Enabled: ptrBool(false)}},
	}}
	// A dependency on an excluded component is no longer a validation error:
	// resolveDependsOnNames drops the edge from the rendered spec.dependsOn instead,
	// and a non-blocking DependencyExcluded condition surfaces it.
	if err := validateDependsOn(bp(comp("a", "b"), comp("b")), w); err != nil {
		t.Fatalf("expected nil error for a dependency on an excluded component, got %v", err)
	}
}
func TestValidateDependsOnHappy(t *testing.T) {
	if err := validateDependsOn(bp(comp("a", "b"), comp("b")), &aiplatformv1alpha1.AIWorkload{}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
func ptrBool(b bool) *bool { return &b }
