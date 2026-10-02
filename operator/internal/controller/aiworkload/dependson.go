package aiworkload

import (
	"fmt"
	"sort"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// validateComponentDependencies checks components[].dependsOn: every entry must
// name another component of the same blueprint and the dependencies must not
// form a cycle. Self-references are also rejected at admission by the CRD; the
// check is repeated here so a blueprint applied before the CRD carried the rule
// still fails cleanly. Components are visited in chartName order so the error
// for a given blueprint is deterministic.
func validateComponentDependencies(components []aiplatformv1alpha1.BlueprintComponent) error {
	byName := make(map[string]aiplatformv1alpha1.BlueprintComponent, len(components))
	for _, c := range components {
		byName[c.ChartName] = c
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		for _, dep := range byName[n].DependsOn {
			if dep == n {
				return fmt.Errorf("component %q depends on itself", n)
			}
			if _, ok := byName[dep]; !ok {
				return fmt.Errorf("component %q depends on %q, which is not a component of this blueprint", n, dep)
			}
		}
	}

	const (
		unvisited = iota
		visiting
		done
	)
	state := make(map[string]int, len(byName))
	var path []string
	var visit func(n string) error
	visit = func(n string) error {
		switch state[n] {
		case done:
			return nil
		case visiting:
			start := 0
			for i, p := range path {
				if p == n {
					start = i
					break
				}
			}
			cycle := append(append([]string{}, path[start:]...), n)
			return fmt.Errorf("component dependencies form a cycle: %s", strings.Join(cycle, " -> "))
		}
		state[n] = visiting
		path = append(path, n)
		deps := append([]string{}, byName[n].DependsOn...)
		sort.Strings(deps)
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[n] = done
		return nil
	}
	for _, n := range names {
		if err := visit(n); err != nil {
			return err
		}
	}
	return nil
}

// pruneDisabledDependencies drops dependsOn entries that name a component not
// in enabled. A workload may disable a component through componentValues; its
// bundle is then never created, so a dependency on it would hold the dependent
// forever. The input slice is not modified.
func pruneDisabledDependencies(enabled []aiplatformv1alpha1.BlueprintComponent) []aiplatformv1alpha1.BlueprintComponent {
	present := make(map[string]bool, len(enabled))
	for _, c := range enabled {
		present[c.ChartName] = true
	}
	out := make([]aiplatformv1alpha1.BlueprintComponent, len(enabled))
	for i, c := range enabled {
		if len(c.DependsOn) > 0 {
			kept := make([]string, 0, len(c.DependsOn))
			for _, dep := range c.DependsOn {
				if present[dep] {
					kept = append(kept, dep)
				}
			}
			if len(kept) == 0 {
				kept = nil
			}
			c.DependsOn = kept
		}
		out[i] = c
	}
	return out
}

// dependsOnRefs maps dependsOn chart names to Fleet BundleRefs. The HelmOp (or
// git-chart Bundle) for a dependency is named blueprintBundleName(workload,
// chartName) in the same Fleet namespace, and the Bundle Fleet creates for a
// HelmOp carries the HelmOp's name, so the name resolves in both fleet-local
// and fleet-default. Returns nil when there are no dependencies.
func dependsOnRefs(workloadName string, deps []string) []any {
	if len(deps) == 0 {
		return nil
	}
	sorted := append([]string{}, deps...)
	sort.Strings(sorted)
	refs := make([]any, 0, len(sorted))
	for _, dep := range sorted {
		refs = append(refs, map[string]any{"name": blueprintBundleName(workloadName, dep)})
	}
	return refs
}
