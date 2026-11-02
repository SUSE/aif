import { isEqual, isPlainObject } from 'lodash';
import type { BlueprintComponent, ComponentKustomize } from '../types/blueprint-types';
import type { ComponentValueOverride } from '../types/aiworkload-types';

// deepMergeValues recursively merges override onto base: override wins on
// scalars, nested plain objects merge key-by-key, arrays/other types replace
// wholesale. Mirrors the operator's dario.cat/mergo(WithOverride) semantics
// so this preview matches what the operator actually renders.
export function deepMergeValues(base: Record<string, any>, override: Record<string, any>): Record<string, any> {
  const result: Record<string, any> = { ...base };
  for (const key of Object.keys(override)) {
    const baseVal = base[key];
    const overrideVal = override[key];
    result[key] = (isPlainObject(baseVal) && isPlainObject(overrideVal))
      ? deepMergeValues(baseVal, overrideVal)
      : overrideVal;
  }
  return result;
}

// componentOverride returns the override values the operator applies to a
// component: the first componentValues entry for it that carries values
// (resolveComponentValues takes the first match, so later duplicates are ignored).
export function componentOverride(overrides: ComponentValueOverride[] | undefined, chartName: string): Record<string, any> | undefined {
  return (overrides || []).find((o) => o.componentName === chartName && o.values != null)?.values;
}

// isComponentEnabled mirrors the operator's check of the same name: the first
// componentValues entry for the component decides, and no entry means enabled.
export function isComponentEnabled(overrides: ComponentValueOverride[] | undefined, chartName: string): boolean {
  return (overrides || []).find((o) => o.componentName === chartName)?.enabled !== false;
}

// seedComponentValues computes the starting per-component values shown in the
// Customize step: each Blueprint component's baked-in defaults, deep-merged
// with any existing override for that component. Passing no `existing` (a
// fresh install) yields the Blueprint defaults unchanged.
export function seedComponentValues(
  components: BlueprintComponent[],
  existing: ComponentValueOverride[] = [],
): Record<string, Record<string, any>> {
  const seed: Record<string, Record<string, any>> = {};
  for (const c of components) {
    const base = c.values || {};
    const override = componentOverride(existing, c.chartName);
    seed[c.chartName] = override ? deepMergeValues(base, override) : { ...base };
  }
  return seed;
}

// deepDiffValues compares base (blueprint defaults) and edited form values,
// returning a minimal delta containing only the keys that actually changed.
// Plain objects are diffed recursively. Arrays and scalar values that differ
// are replaced wholesale (mirroring Helm value merge semantics).
// Returns undefined if base and edited are structurally equal.
export function deepDiffValues(
  base: Record<string, any> | undefined,
  edited: Record<string, any> | undefined,
): Record<string, any> | undefined {
  if (isEqual(base, edited)) {
    return undefined;
  }
  if (!isPlainObject(base) || !isPlainObject(edited)) {
    return edited;
  }

  const baseObj = base as Record<string, any>;
  const editedObj = edited as Record<string, any>;
  const diff: Record<string, any> = {};

  for (const key of Object.keys(editedObj)) {
    const baseVal = baseObj[key];
    const editedVal = editedObj[key];

    if (!isEqual(baseVal, editedVal)) {
      if (isPlainObject(baseVal) && isPlainObject(editedVal)) {
        const nestedDiff = deepDiffValues(baseVal, editedVal);
        if (nestedDiff !== undefined) {
          diff[key] = nestedDiff;
        }
      } else {
        diff[key] = editedVal;
      }
    }
  }

  return Object.keys(diff).length > 0 ? diff : undefined;
}

// diffComponentValues compares the current per-component form values against
// the seed they started from and returns minimal override entries only for
// components that actually changed — untouched keys within components keep
// tracking the Blueprint defaults instead of freezing a stale snapshot.
export function diffComponentValues(
  seed: Record<string, Record<string, any>>,
  edited: Record<string, Record<string, any>>,
): ComponentValueOverride[] {
  const overrides: ComponentValueOverride[] = [];
  for (const chartName of Object.keys(edited)) {
    const editedValues = edited[chartName] || {};
    const seedValues = seed[chartName] || {};
    const diff = deepDiffValues(seedValues, editedValues);
    if (diff !== undefined) {
      overrides.push({ componentName: chartName, values: diff });
    }
  }
  return overrides;
}

// firstOverride returns the componentValues entry the operator applies to a
// component: the first one naming it (isComponentEnabled's convention).
function firstOverride(overrides: ComponentValueOverride[] | undefined, chartName: string): ComponentValueOverride | undefined {
  return (overrides || []).find((o) => o.componentName === chartName);
}

// seedComponentDependsOn computes each component's starting deploy order:
// the existing override's dependsOn when it sets one ([] = no dependencies),
// else the Blueprint's. Names this Blueprint version no longer has are dropped
// (staleDependsOn reports them).
export function seedComponentDependsOn(components: BlueprintComponent[], existing: ComponentValueOverride[] = []): Record<string, string[]> {
  const known = new Set(components.map((c) => c.chartName));
  const seed: Record<string, string[]> = {};

  for (const c of components) {
    const deps = firstOverride(existing, c.chartName)?.dependsOn ?? c.dependsOn ?? [];

    seed[c.chartName] = deps.filter((d) => known.has(d) && d !== c.chartName);
  }

  return seed;
}

// staleDependsOn lists, per component, the override dependsOn names this
// Blueprint version no longer has (possible after an upgrade).
export function staleDependsOn(components: BlueprintComponent[], existing: ComponentValueOverride[] = []): Record<string, string[]> {
  const known = new Set(components.map((c) => c.chartName));
  const stale: Record<string, string[]> = {};

  for (const c of components) {
    const missing = (firstOverride(existing, c.chartName)?.dependsOn || []).filter((d) => !known.has(d));

    if (missing.length) {
      stale[c.chartName] = missing;
    }
  }

  return stale;
}

// seedComponentKustomize returns each component's install-time overlay (only
// components whose applied override sets one). Overrides for components this
// Blueprint version no longer has are ignored, like the other seed helpers, so
// they are never re-emitted (the API would reject them as unknown).
export function seedComponentKustomize(components: BlueprintComponent[], existing: ComponentValueOverride[] = []): Record<string, ComponentKustomize | undefined> {
  const seed: Record<string, ComponentKustomize | undefined> = {};

  for (const c of components) {
    const kustomize = firstOverride(existing, c.chartName)?.kustomize;

    if (kustomize) {
      seed[c.chartName] = kustomize;
    }
  }

  return seed;
}

// effectiveComponents returns components with install-time dependsOn
// overrides applied, for deploy-order display and validation.
export function effectiveComponents(components: BlueprintComponent[], overrides: ComponentValueOverride[] = []): BlueprintComponent[] {
  return components.map((c) => {
    const deps = firstOverride(overrides, c.chartName)?.dependsOn;

    return deps !== undefined ? { ...c, dependsOn: [...deps] } : c;
  });
}

// AdvancedEdits carries the install wizard's Advanced-tab state: the
// Blueprint's deploy order to diff against, the edited order, and the
// install-time overlays.
export interface AdvancedEdits {
  blueprintDependsOn: Record<string, string[]>;
  editedDependsOn:    Record<string, string[]>;
  editedKustomize:    Record<string, ComponentKustomize | undefined>;
}

function sameSet(a: string[] = [], b: string[] = []): boolean {
  return isEqual([...a].sort(), [...b].sort());
}

// dependsOnChanged reports whether a component's edited deploy order differs
// from the Blueprint's (as a set: order of entries does not matter).
function dependsOnChanged(advanced: AdvancedEdits | undefined, name: string): boolean {
  return !!advanced && name in advanced.editedDependsOn && !sameSet(advanced.editedDependsOn[name], advanced.blueprintDependsOn[name]);
}

// seedComponentEnabled computes the starting per-component enabled state shown
// in the Customize step, mirroring seedComponentValues: each component starts
// enabled unless an existing override explicitly disabled it.
export function seedComponentEnabled(
  components: BlueprintComponent[],
  existing: ComponentValueOverride[] = [],
): Record<string, boolean> {
  const seed: Record<string, boolean> = {};
  for (const c of components) {
    seed[c.chartName] = isComponentEnabled(existing, c.chartName);
  }
  return seed;
}

// diffComponentOverrides combines the values diff (diffComponentValues) with a
// per-component enabled diff into the single ComponentValueOverride[] shape the
// AIWorkload spec expects — one entry per component that changed either its
// values or its enabled state, carrying only the field(s) that actually changed.
export function diffComponentOverrides(
  seedValues: Record<string, Record<string, any>>,
  editedValues: Record<string, Record<string, any>>,
  seedEnabled: Record<string, boolean>,
  editedEnabled: Record<string, boolean>,
  advanced?: AdvancedEdits,
): ComponentValueOverride[] {
  const changedValues = new Map(diffComponentValues(seedValues, editedValues).map((o) => [o.componentName, o.values]));
  const names = new Set([
    ...Object.keys(seedEnabled), ...Object.keys(editedEnabled), ...changedValues.keys(),
    ...Object.keys(advanced?.editedDependsOn || {}), ...Object.keys(advanced?.editedKustomize || {}),
  ]);
  const overrides: ComponentValueOverride[] = [];
  for (const name of names) {
    const values = changedValues.get(name);
    const enabledChanged = (seedEnabled[name] ?? true) !== (editedEnabled[name] ?? true);
    const orderChanged = dependsOnChanged(advanced, name);
    const kustomize = advanced?.editedKustomize[name];
    if (values === undefined && !enabledChanged && !orderChanged && !kustomize) continue;
    overrides.push({
      componentName: name,
      ...(values !== undefined ? { values } : {}),
      ...(enabledChanged ? { enabled: editedEnabled[name] } : {}),
      ...(orderChanged ? { dependsOn: [...advanced!.editedDependsOn[name]] } : {}),
      ...(kustomize ? { kustomize } : {}),
    });
  }
  return overrides;
}

// customizedComponentNames returns the set of component names whose Helm values
// differ from the Blueprint default values, excluding components that are disabled
// (which are deselected via the checkbox instead of labeled 'Customized').
export function customizedComponentNames(
  blueprintDefaults: Record<string, Record<string, any>>,
  currentValues: Record<string, Record<string, any>>,
  currentEnabled: Record<string, boolean>,
  advanced?: AdvancedEdits,
): Set<string> {
  const names = diffComponentValues(blueprintDefaults, currentValues).map((o) => o.componentName);
  if (advanced) {
    names.push(
      ...Object.keys(advanced.editedDependsOn).filter((name) => dependsOnChanged(advanced, name)),
      ...Object.keys(advanced.editedKustomize).filter((name) => !!advanced.editedKustomize[name]),
    );
  }
  return new Set(names.filter((name) => currentEnabled[name] !== false));
}

export type OverrideChange = 'values' | 'overlay' | 'order';

// customizedOverrides lists, for the install review, each enabled component
// whose override changes something, and what: Helm values, an install-time
// overlay, or the deploy order. Excluded components show as "Excluded" instead.
export function customizedOverrides(overrides: ComponentValueOverride[] = []): { componentName: string; changes: OverrideChange[] }[] {
  return overrides
    .filter((ov) => ov.enabled !== false)
    .map((ov) => ({
      componentName: ov.componentName,
      changes:       [
        ...(ov.values !== undefined ? ['values' as const] : []),
        ...(ov.kustomize ? ['overlay' as const] : []),
        ...(ov.dependsOn !== undefined ? ['order' as const] : []),
      ],
    }))
    .filter((row) => row.changes.length > 0);
}

// deployOrder groups components into dependency waves for read-only display in
// the install wizard (Task 8): each wave contains components whose dependsOn
// are all already resolved by earlier waves. Assumes validated acyclic input
// from authoring (Tasks 6-7); the cycle guard below only protects display
// against unexpected bad data rather than being a validation mechanism.
export function deployOrder(components: BlueprintComponent[]): string[][] {
  const remaining = new Map(components.map((c) => [c.chartName, new Set(c.dependsOn || [])]));
  const waves: string[][] = [];
  while (remaining.size) {
    const wave = [...remaining.entries()].filter(([, deps]) => [...deps].every((d) => !remaining.has(d))).map(([n]) => n);
    if (!wave.length) { waves.push([...remaining.keys()]); break; } // cycle guard
    wave.forEach((n) => remaining.delete(n));
    waves.push(wave.sort());
  }
  return waves;
}

// effectiveDeployOrder is the order the controller actually deploys in:
// install-time dependsOn overrides applied and excluded components dropped.
// deployOrder treats a dependency on a dropped component as already resolved,
// matching the controller dropping edges to excluded components.
export function effectiveDeployOrder(components: BlueprintComponent[], overrides: ComponentValueOverride[] = []): string[][] {
  const enabled = components.filter((c) => isComponentEnabled(overrides, c.chartName));

  return deployOrder(effectiveComponents(enabled, overrides));
}

// resolveInitialFormState resolves the starting editedValues and editedEnabled
// for the Customize step, prioritizing any active in-progress overrides (e.g.
// across wizard step navigation) over the baseline overrides from the workload.
export function resolveInitialFormState(
  components: BlueprintComponent[],
  existingOverrides: ComponentValueOverride[] = [],
  currentOverrides?: ComponentValueOverride[],
): {
  values: Record<string, Record<string, any>>;
  enabled: Record<string, boolean>;
  dependsOn: Record<string, string[]>;
  kustomize: Record<string, ComponentKustomize | undefined>;
} {
  const overrides = currentOverrides !== undefined ? currentOverrides : existingOverrides;
  return {
    values: seedComponentValues(components, overrides),
    enabled: seedComponentEnabled(components, overrides),
    dependsOn: seedComponentDependsOn(components, overrides),
    kustomize: seedComponentKustomize(components, overrides),
  };
}
