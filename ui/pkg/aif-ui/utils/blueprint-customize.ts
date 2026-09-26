import { isEqual, isPlainObject } from 'lodash';
import type { BlueprintComponent } from '../types/blueprint-types';
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

// seedComponentValues computes the starting per-component values shown in the
// Customize step: each Blueprint component's baked-in defaults, deep-merged
// with any existing override for that component. Passing no `existing` (a
// fresh install) yields the Blueprint defaults unchanged.
export function seedComponentValues(
  components: BlueprintComponent[],
  existing: ComponentValueOverride[] = [],
): Record<string, Record<string, any>> {
  const overrideByName = new Map(existing.map((o) => [o.componentName, o.values || {}]));
  const seed: Record<string, Record<string, any>> = {};
  for (const c of components) {
    const base = c.values || {};
    const override = overrideByName.get(c.chartName);
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

// seedComponentEnabled computes the starting per-component enabled state shown
// in the Customize step, mirroring seedComponentValues: each component starts
// enabled unless an existing override explicitly disabled it.
export function seedComponentEnabled(
  components: BlueprintComponent[],
  existing: ComponentValueOverride[] = [],
): Record<string, boolean> {
  const overrideByName = new Map(existing.map((o) => [o.componentName, o]));
  const seed: Record<string, boolean> = {};
  for (const c of components) {
    seed[c.chartName] = overrideByName.get(c.chartName)?.enabled ?? true;
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
): ComponentValueOverride[] {
  const changedValues = new Map(diffComponentValues(seedValues, editedValues).map((o) => [o.componentName, o.values]));
  const names = new Set([...Object.keys(seedEnabled), ...Object.keys(editedEnabled), ...changedValues.keys()]);
  const overrides: ComponentValueOverride[] = [];
  for (const name of names) {
    const values = changedValues.get(name);
    const enabledChanged = (seedEnabled[name] ?? true) !== (editedEnabled[name] ?? true);
    if (values === undefined && !enabledChanged) continue;
    overrides.push({
      componentName: name,
      ...(values !== undefined ? { values } : {}),
      ...(enabledChanged ? { enabled: editedEnabled[name] } : {}),
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
): Set<string> {
  const diffs = diffComponentValues(blueprintDefaults, currentValues);
  return new Set(
    diffs
      .filter((o) => currentEnabled[o.componentName] !== false)
      .map((o) => o.componentName),
  );
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
} {
  const overrides = currentOverrides !== undefined ? currentOverrides : existingOverrides;
  return {
    values: seedComponentValues(components, overrides),
    enabled: seedComponentEnabled(components, overrides),
  };
}
