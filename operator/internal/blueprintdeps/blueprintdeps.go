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

// Package blueprintdeps is the single home for Blueprint component dependency
// graph logic shared by the API (strict validation of install-time overrides)
// and the AIWorkload controller (lenient application of those overrides at
// reconcile time, where upgrades can make a previously valid override stale).
package blueprintdeps

import (
	"fmt"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// Cycle returns the first dependency cycle found when visiting roots in order,
// as a closed path (e.g. ["a", "b", "a"]), or nil when graph is acyclic. Edges
// to nodes absent from graph are treated as leaves. Visiting roots in a fixed
// order keeps the reported cycle deterministic.
func Cycle(order []string, graph map[string][]string) []string {
	state := map[string]int{} // 0=unvisited, 1=on-stack, 2=done
	var path []string
	var visit func(n string) []string
	visit = func(n string) []string {
		state[n] = 1
		path = append(path, n)
		for _, m := range graph[n] {
			switch state[m] {
			case 1:
				start := 0
				for i, p := range path {
					if p == m {
						start = i
						break
					}
				}
				return append(append([]string{}, path[start:]...), m)
			case 0:
				if cycle := visit(m); cycle != nil {
					return cycle
				}
			}
		}
		path = path[:len(path)-1]
		state[n] = 2
		return nil
	}
	for _, n := range order {
		if state[n] == 0 {
			if cycle := visit(n); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// OverrideDependsOn returns the install-time dependsOn override for chartName.
// The first componentValues entry matching chartName decides (the convention
// shared with isComponentEnabled and resolveComponentValues): ok is true only
// when that entry sets DependsOn, which may be an empty list meaning "no
// dependencies".
func OverrideDependsOn(overrides []aiplatformv1alpha1.ComponentValueOverride, chartName string) (deps []string, ok bool) {
	for _, ov := range overrides {
		if ov.ComponentName != chartName {
			continue
		}
		if ov.DependsOn == nil {
			return nil, false
		}
		return *ov.DependsOn, true
	}
	return nil, false
}

// EffectiveGraph returns each component's dependsOn with install-time
// overrides applied as-is: no dropping and no fallback. The API uses it to
// reject invalid overrides at submit time.
func EffectiveGraph(components []aiplatformv1alpha1.BlueprintComponent, overrides []aiplatformv1alpha1.ComponentValueOverride) map[string][]string {
	graph := make(map[string][]string, len(components))
	for _, c := range components {
		deps := c.DependsOn
		if ov, ok := OverrideDependsOn(overrides, c.ChartName); ok {
			deps = ov
		}
		graph[c.ChartName] = append([]string(nil), deps...)
	}
	return graph
}

// Apply returns a copy of components with install-time dependsOn overrides
// applied leniently, plus human-readable notes describing every adjustment.
// An override replaces the component's Blueprint dependsOn. Entries naming an
// unknown component (possible after upgrading to a Blueprint version that
// removed it) or the component itself are dropped. If the result has a cycle,
// every overridden component on it reverts to its Blueprint dependsOn until
// the graph is acyclic; that terminates because the Blueprint's own graph is
// validated acyclic before Apply runs. Inputs are never mutated.
func Apply(components []aiplatformv1alpha1.BlueprintComponent, overrides []aiplatformv1alpha1.ComponentValueOverride) ([]aiplatformv1alpha1.BlueprintComponent, []string) {
	out := make([]aiplatformv1alpha1.BlueprintComponent, len(components))
	known := make(map[string]bool, len(components))
	order := make([]string, len(components))
	for i, c := range components {
		out[i] = c
		out[i].DependsOn = append([]string(nil), c.DependsOn...)
		known[c.ChartName] = true
		order[i] = c.ChartName
	}

	var notes []string
	overridden := map[string]int{} // chartName -> index in out
	for i, c := range components {
		deps, ok := OverrideDependsOn(overrides, c.ChartName)
		if !ok {
			continue
		}
		kept := make([]string, 0, len(deps))
		for _, d := range deps {
			switch {
			case d == c.ChartName:
				// Rejected at admission (CEL); dropped defensively.
			case !known[d]:
				notes = append(notes, fmt.Sprintf("%s: ignored unknown dependency %s", c.ChartName, d))
			default:
				kept = append(kept, d)
			}
		}
		out[i].DependsOn = kept
		overridden[c.ChartName] = i
	}

	for {
		graph := make(map[string][]string, len(out))
		for _, c := range out {
			graph[c.ChartName] = c.DependsOn
		}
		cycle := Cycle(order, graph)
		if cycle == nil {
			break
		}
		reverted := false
		for _, n := range cycle[:len(cycle)-1] {
			i, ok := overridden[n]
			if !ok {
				continue
			}
			out[i].DependsOn = append([]string(nil), components[i].DependsOn...)
			delete(overridden, n)
			notes = append(notes, fmt.Sprintf("%s: install order formed a cycle (%s); reverted to blueprint order", n, strings.Join(cycle, " -> ")))
			reverted = true
		}
		if !reverted {
			// The cycle is the Blueprint's own; validateDependsOn reports it.
			break
		}
	}
	return out, notes
}
