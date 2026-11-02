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

package blueprintdeps

import (
	"reflect"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func comp(name string, deps ...string) aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{ChartRepo: "r", ChartName: name, ChartVersion: "1.0.0", DependsOn: deps}
}

func override(name string, deps ...string) aiplatformv1alpha1.ComponentValueOverride {
	d := append([]string{}, deps...)
	return aiplatformv1alpha1.ComponentValueOverride{ComponentName: name, DependsOn: &d}
}

func depsOf(components []aiplatformv1alpha1.BlueprintComponent, name string) []string {
	for _, c := range components {
		if c.ChartName == name {
			return c.DependsOn
		}
	}
	return nil
}

func TestCycle_NoneReturnsNil(t *testing.T) {
	graph := map[string][]string{"a": nil, "b": {"a"}, "c": {"a", "b"}}
	if got := Cycle([]string{"a", "b", "c"}, graph); got != nil {
		t.Fatalf("Cycle = %v, want nil", got)
	}
}

func TestCycle_ReportsClosedPath(t *testing.T) {
	graph := map[string][]string{"a": {"b"}, "b": {"a"}}
	got := Cycle([]string{"a", "b"}, graph)
	if want := []string{"a", "b", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Cycle = %v, want %v", got, want)
	}
}

func TestOverrideDependsOn_FirstMatchWins(t *testing.T) {
	overrides := []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "b"},
		override("a", "x"),
		override("a", "y"),
	}
	got, ok := OverrideDependsOn(overrides, "a")
	if !ok || !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("OverrideDependsOn = (%v, %v), want ([x], true)", got, ok)
	}
	// A first matching entry without DependsOn decides too: no override.
	overrides = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "a"}, override("a", "y")}
	if got, ok := OverrideDependsOn(overrides, "a"); ok || got != nil {
		t.Fatalf("OverrideDependsOn = (%v, %v), want (nil, false)", got, ok)
	}
}

func TestOverrideDependsOn_EmptyListIsSet(t *testing.T) {
	got, ok := OverrideDependsOn([]aiplatformv1alpha1.ComponentValueOverride{override("a")}, "a")
	if !ok || got == nil || len(got) != 0 {
		t.Fatalf("OverrideDependsOn = (%#v, %v), want ([]string{}, true)", got, ok)
	}
}

func TestApply_ReplacesPerComponent(t *testing.T) {
	components := []aiplatformv1alpha1.BlueprintComponent{comp("a"), comp("b", "a"), comp("c", "a")}
	got, notes := Apply(components, []aiplatformv1alpha1.ComponentValueOverride{override("c", "b")})
	if !reflect.DeepEqual(depsOf(got, "c"), []string{"b"}) {
		t.Fatalf("c deps = %v, want [b]", depsOf(got, "c"))
	}
	if !reflect.DeepEqual(depsOf(got, "b"), []string{"a"}) {
		t.Fatalf("b deps = %v, want untouched [a]", depsOf(got, "b"))
	}
	if len(notes) != 0 {
		t.Fatalf("notes = %v, want none", notes)
	}
}

func TestApply_EmptyOverrideClearsDeps(t *testing.T) {
	components := []aiplatformv1alpha1.BlueprintComponent{comp("a"), comp("b", "a")}
	got, _ := Apply(components, []aiplatformv1alpha1.ComponentValueOverride{override("b")})
	if len(depsOf(got, "b")) != 0 {
		t.Fatalf("b deps = %v, want none", depsOf(got, "b"))
	}
}

func TestApply_DropsUnknownWithNote(t *testing.T) {
	components := []aiplatformv1alpha1.BlueprintComponent{comp("a"), comp("b")}
	got, notes := Apply(components, []aiplatformv1alpha1.ComponentValueOverride{override("b", "a", "gone", "b")})
	if !reflect.DeepEqual(depsOf(got, "b"), []string{"a"}) {
		t.Fatalf("b deps = %v, want [a]", depsOf(got, "b"))
	}
	if len(notes) != 1 || notes[0] != "b: ignored unknown dependency gone" {
		t.Fatalf("notes = %q, want one unknown-dependency note", notes)
	}
}

func TestApply_CycleFallsBackToBlueprint(t *testing.T) {
	components := []aiplatformv1alpha1.BlueprintComponent{comp("a"), comp("b", "a")}
	got, notes := Apply(components, []aiplatformv1alpha1.ComponentValueOverride{override("a", "b")})
	if len(depsOf(got, "a")) != 0 {
		t.Fatalf("a deps = %v, want reverted to blueprint (none)", depsOf(got, "a"))
	}
	if !reflect.DeepEqual(depsOf(got, "b"), []string{"a"}) {
		t.Fatalf("b deps = %v, want [a]", depsOf(got, "b"))
	}
	if len(notes) != 1 {
		t.Fatalf("notes = %q, want one cycle fallback note for a", notes)
	}
	namesCycle := strings.Contains(notes[0], "a -> b -> a") || strings.Contains(notes[0], "b -> a -> b")
	if !namesCycle || !strings.HasPrefix(notes[0], "a: install order formed a cycle (") || !strings.HasSuffix(notes[0], "); reverted to blueprint order") {
		t.Fatalf("note = %q, want a cycle fallback note for a naming the cycle", notes[0])
	}
}

func TestApply_DoesNotMutateInputs(t *testing.T) {
	components := []aiplatformv1alpha1.BlueprintComponent{comp("a"), comp("b", "a")}
	overrides := []aiplatformv1alpha1.ComponentValueOverride{override("b", "gone"), override("a", "b")}
	wantComponents := []aiplatformv1alpha1.BlueprintComponent{comp("a"), comp("b", "a")}
	wantOverrides := []aiplatformv1alpha1.ComponentValueOverride{override("b", "gone"), override("a", "b")}

	got, _ := Apply(components, overrides)
	got[0].DependsOn = append(got[0].DependsOn, "mutated")

	if !reflect.DeepEqual(components, wantComponents) {
		t.Fatalf("components mutated: %+v", components)
	}
	if !reflect.DeepEqual(overrides, wantOverrides) {
		t.Fatalf("overrides mutated: %+v", overrides)
	}
}

func TestEffectiveGraph_StrictReplace(t *testing.T) {
	components := []aiplatformv1alpha1.BlueprintComponent{comp("a"), comp("b", "a")}
	got := EffectiveGraph(components, []aiplatformv1alpha1.ComponentValueOverride{override("a", "b", "gone")})
	want := map[string][]string{"a": {"b", "gone"}, "b": {"a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveGraph = %v, want %v", got, want)
	}
}
