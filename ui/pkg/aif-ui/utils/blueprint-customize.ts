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

// diffComponentValues compares the current per-component form values against
// the seed they started from and returns override entries only for
// components that actually changed — untouched components keep tracking the
// Blueprint's (or existing override's) values instead of freezing a stale
// snapshot.
export function diffComponentValues(
  seed: Record<string, Record<string, any>>,
  edited: Record<string, Record<string, any>>,
): ComponentValueOverride[] {
  const overrides: ComponentValueOverride[] = [];
  for (const chartName of Object.keys(edited)) {
    const editedValues = edited[chartName] || {};
    const seedValues = seed[chartName] || {};
    if (!isEqual(seedValues, editedValues)) {
      overrides.push({ componentName: chartName, values: editedValues });
    }
  }
  return overrides;
}
