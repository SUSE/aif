import { describe, it, expect } from 'vitest';
import {
  deepMergeValues, deepDiffValues, seedComponentValues, diffComponentValues,
  seedComponentEnabled, diffComponentOverrides,
  customizedComponentNames, resolveInitialFormState,
} from '../blueprint-customize';
import type { BlueprintComponent } from '../../types/blueprint-types';

describe('deepMergeValues', () => {
  it('overrides scalar keys', () => {
    expect(deepMergeValues({ replicas: 1 }, { replicas: 3 })).toEqual({ replicas: 3 });
  });

  it('deep-merges nested objects, preserving untouched sibling keys', () => {
    const base = { persistence: { size: '10Gi', storageClass: 'default' } };
    const override = { persistence: { size: '50Gi' } };
    expect(deepMergeValues(base, override)).toEqual({ persistence: { size: '50Gi', storageClass: 'default' } });
  });

  it('replaces arrays wholesale rather than concatenating', () => {
    expect(deepMergeValues({ tags: ['a', 'b'] }, { tags: ['c'] })).toEqual({ tags: ['c'] });
  });

  it('leaves base untouched when override is empty', () => {
    const base = { a: 1 };
    expect(deepMergeValues(base, {})).toEqual({ a: 1 });
  });
});

function comp(chartName: string, values?: Record<string, any>): BlueprintComponent {
  return { chartRepo: 'suse-ai', chartName, chartVersion: '1.0.0', values };
}

describe('seedComponentValues', () => {
  it('seeds from blueprint defaults when there is no existing override', () => {
    const components = [comp('milvus', { replicas: 1 }), comp('open-webui')];
    expect(seedComponentValues(components)).toEqual({ milvus: { replicas: 1 }, 'open-webui': {} });
  });

  it('merges an existing override onto the blueprint defaults for manage-mode seeding', () => {
    const components = [comp('milvus', { replicas: 1, persistence: { size: '10Gi' } })];
    const existing = [{ componentName: 'milvus', values: { persistence: { size: '50Gi' } } }];
    expect(seedComponentValues(components, existing)).toEqual({
      milvus: { replicas: 1, persistence: { size: '50Gi' } },
    });
  });

  it('ignores an override for a component not in the current blueprint version', () => {
    const components = [comp('milvus', { replicas: 1 })];
    const existing = [{ componentName: 'removed-component', values: { replicas: 9 } }];
    expect(seedComponentValues(components, existing)).toEqual({ milvus: { replicas: 1 } });
  });
});

describe('deepDiffValues', () => {
  it('returns undefined when base and edited are identical', () => {
    const obj = { a: 1, b: { c: 2 }, d: [1, 2] };
    expect(deepDiffValues(obj, { a: 1, b: { c: 2 }, d: [1, 2] })).toBeUndefined();
  });

  it('emits only changed scalar keys, omitting untouched sibling keys', () => {
    const base = { replicas: 1, port: 80, env: 'prod' };
    const edited = { replicas: 3, port: 80, env: 'prod' };
    expect(deepDiffValues(base, edited)).toEqual({ replicas: 3 });
  });

  it('recursively diffs nested objects, omitting untouched sibling nested keys', () => {
    const base = {
      replicas: 1,
      persistence: { size: '10Gi', storageClass: 'fast' },
    };
    const edited = {
      replicas: 1,
      persistence: { size: '50Gi', storageClass: 'fast' },
    };
    expect(deepDiffValues(base, edited)).toEqual({
      persistence: { size: '50Gi' },
    });
  });

  it('replaces arrays wholesale rather than element-by-element diffing', () => {
    const base = { tags: ['a', 'b'] };
    const edited = { tags: ['a', 'c'] };
    expect(deepDiffValues(base, edited)).toEqual({ tags: ['a', 'c'] });
  });

  it('includes newly added keys that did not exist in base', () => {
    const base = { replicas: 1 };
    const edited = { replicas: 1, extraConfig: { debug: true } };
    expect(deepDiffValues(base, edited)).toEqual({ extraConfig: { debug: true } });
  });

  it('omits deleted keys so the Blueprint defaults continue to win', () => {
    const base = { replicas: 1, env: 'prod' };
    const edited = { replicas: 3 };
    expect(deepDiffValues(base, edited)).toEqual({ replicas: 3 });
  });
});

describe('diffComponentValues', () => {
  it('returns no overrides when nothing changed', () => {
    const seed = { milvus: { replicas: 1 } };
    expect(diffComponentValues(seed, seed)).toEqual([]);
  });

  it('returns an override only for components that actually changed', () => {
    const seed = { milvus: { replicas: 1 }, 'open-webui': { replicas: 2 } };
    const edited = { milvus: { replicas: 1 }, 'open-webui': { replicas: 5 } };
    expect(diffComponentValues(seed, edited)).toEqual([{ componentName: 'open-webui', values: { replicas: 5 } }]);
  });

  it('is insensitive to key order', () => {
    const seed = { milvus: { a: 1, b: 2 } };
    const edited = { milvus: { b: 2, a: 1 } };
    expect(diffComponentValues(seed, edited)).toEqual([]);
  });

  it('emits minimal deltas rather than full merged objects (Claude feedback #1)', () => {
    const seed = {
      milvus: {
        replicas: 1,
        persistence: { size: '10Gi', storageClass: 'default' },
      },
    };
    const edited = {
      milvus: {
        replicas: 5,
        persistence: { size: '10Gi', storageClass: 'default' },
      },
    };
    expect(diffComponentValues(seed, edited)).toEqual([
      { componentName: 'milvus', values: { replicas: 5 } },
    ]);
  });
});

describe('seedComponentEnabled', () => {
  it('defaults every component to enabled when there is no existing override', () => {
    const components = [comp('milvus'), comp('open-webui')];
    expect(seedComponentEnabled(components)).toEqual({ milvus: true, 'open-webui': true });
  });

  it('seeds false from an existing override that disabled a component', () => {
    const components = [comp('milvus'), comp('open-webui')];
    const existing = [{ componentName: 'milvus', enabled: false }];
    expect(seedComponentEnabled(components, existing)).toEqual({ milvus: false, 'open-webui': true });
  });
});

describe('diffComponentOverrides', () => {
  it('returns no overrides when nothing changed', () => {
    const seedValues = { milvus: { replicas: 1 } };
    const seedEnabled = { milvus: true };
    expect(diffComponentOverrides(seedValues, seedValues, seedEnabled, seedEnabled)).toEqual([]);
  });

  it('emits only enabled when a component is disabled with no value edits', () => {
    const values = { milvus: { replicas: 1 } };
    const seedEnabled = { milvus: true };
    const editedEnabled = { milvus: false };
    expect(diffComponentOverrides(values, values, seedEnabled, editedEnabled)).toEqual([
      { componentName: 'milvus', enabled: false },
    ]);
  });

  it('emits only values when values change with enabled untouched', () => {
    const seedValues = { milvus: { replicas: 1 } };
    const editedValues = { milvus: { replicas: 3 } };
    const enabled = { milvus: true };
    expect(diffComponentOverrides(seedValues, editedValues, enabled, enabled)).toEqual([
      { componentName: 'milvus', values: { replicas: 3 } },
    ]);
  });

  it('emits both fields when a component is disabled and its values were also edited', () => {
    const seedValues = { milvus: { replicas: 1 } };
    const editedValues = { milvus: { replicas: 3 } };
    const seedEnabled = { milvus: true };
    const editedEnabled = { milvus: false };
    expect(diffComponentOverrides(seedValues, editedValues, seedEnabled, editedEnabled)).toEqual([
      { componentName: 'milvus', values: { replicas: 3 }, enabled: false },
    ]);
  });
});

describe('customizedComponentNames', () => {
  const defaults = {
    comp1: { replicas: 1 },
    comp2: { port: 80 },
    comp3: { model: 'llama' },
  };

  it('returns empty set when no components have modified values', () => {
    const current = { ...defaults };
    const enabled = { comp1: true, comp2: true, comp3: true };
    expect(customizedComponentNames(defaults, current, enabled)).toEqual(new Set());
  });

  it('marks enabled component as customized when values differ', () => {
    const current = { ...defaults, comp1: { replicas: 3 } };
    const enabled = { comp1: true, comp2: true, comp3: true };
    expect(customizedComponentNames(defaults, current, enabled)).toEqual(new Set(['comp1']));
  });

  it('does NOT mark a component as customized when it is deselected/excluded', () => {
    const current = { ...defaults };
    const enabled = { comp1: true, comp2: true, comp3: false };
    expect(customizedComponentNames(defaults, current, enabled)).toEqual(new Set());
  });

  it('does NOT mark a component as customized if it is disabled even if values differ', () => {
    const current = { ...defaults, comp3: { model: 'mistral' } };
    const enabled = { comp1: true, comp2: true, comp3: false };
    expect(customizedComponentNames(defaults, current, enabled)).toEqual(new Set());
  });

  it('clears customized status when component values are restored to defaults', () => {
    const current = { ...defaults, comp1: { replicas: 1 } };
    const enabled = { comp1: true, comp2: true, comp3: true };
    expect(customizedComponentNames(defaults, current, enabled)).toEqual(new Set());
  });

  it('re-enabling a component with modified values restores its customized status', () => {
    const current = { ...defaults, comp3: { model: 'mistral' } };
    expect(customizedComponentNames(defaults, current, { comp1: true, comp2: true, comp3: false })).toEqual(new Set());
    expect(customizedComponentNames(defaults, current, { comp1: true, comp2: true, comp3: true })).toEqual(new Set(['comp3']));
  });
});

describe('resolveInitialFormState', () => {
  const components = [
    comp('comp1', { replicas: 1 }),
    comp('comp2', { port: 80 }),
    comp('comp3', { model: 'llama' }),
  ];

  it('initializes from blueprint defaults on install when no overrides exist', () => {
    const state = resolveInitialFormState(components, [], undefined);
    expect(state.values).toEqual({
      comp1: { replicas: 1 },
      comp2: { port: 80 },
      comp3: { model: 'llama' },
    });
    expect(state.enabled).toEqual({ comp1: true, comp2: true, comp3: true });
  });

  it('restores existing overrides in manage mode when currentOverrides is undefined', () => {
    const existing = [
      { componentName: 'comp1', values: { replicas: 5 } },
      { componentName: 'comp3', enabled: false },
    ];
    const state = resolveInitialFormState(components, existing, undefined);
    expect(state.values.comp1).toEqual({ replicas: 5 });
    expect(state.values.comp2).toEqual({ port: 80 });
    expect(state.enabled.comp3).toBe(false);
  });

  it('respects explicitly empty currentOverrides ([]) without resurrecting existing overrides (Claude #2)', () => {
    const existing = [
      { componentName: 'comp1', values: { replicas: 5 } },
    ];
    const state = resolveInitialFormState(components, existing, []);
    expect(state.values.comp1).toEqual({ replicas: 1 });
  });

  it('preserves in-progress currentOverrides across wizard step navigation', () => {
    const inProgress = [
      { componentName: 'comp1', values: { replicas: 3 } },
      { componentName: 'comp3', enabled: false },
    ];
    const state = resolveInitialFormState(components, [], inProgress);
    expect(state.values.comp1).toEqual({ replicas: 3 });
    expect(state.values.comp2).toEqual({ port: 80 });
    expect(state.enabled.comp3).toBe(false);
  });
});

describe('wizard multi-component workflows and navigation (bug fixes 1 and 2)', () => {
  const components = [
    comp('comp1', { replicas: 1 }),
    comp('comp2', { port: 80 }),
    comp('comp3', { model: 'llama' }),
  ];
  const defaults = seedComponentValues(components, []);
  const defaultsEnabled = seedComponentEnabled(components, []);

  it('Bug 1: deselecting a component does NOT show Customized badge, persists across steps, and re-enabling does not show it', () => {
    let formState = resolveInitialFormState(components, [], undefined);

    // Step A: Deselect comp3 -> checkbox is deselected, Customized badge does NOT appear
    formState.enabled.comp3 = false;
    let overrides = diffComponentOverrides(defaults, formState.values, defaultsEnabled, formState.enabled);
    expect(overrides).toEqual([{ componentName: 'comp3', enabled: false }]);
    let customized = customizedComponentNames(defaults, formState.values, formState.enabled);
    expect(customized.has('comp3')).toBe(false);

    // Step B: Move to Review and return -> comp3 remains deselected, badge is NOT present
    formState = resolveInitialFormState(components, [], overrides);
    expect(formState.enabled.comp3).toBe(false);
    customized = customizedComponentNames(defaults, formState.values, formState.enabled);
    expect(customized.has('comp3')).toBe(false);

    // Step C: Select comp3 back -> comp3 is re-enabled, Customized badge does NOT appear (back to default)
    formState.enabled.comp3 = true;
    overrides = diffComponentOverrides(defaults, formState.values, defaultsEnabled, formState.enabled);
    expect(overrides).toEqual([]);
    customized = customizedComponentNames(defaults, formState.values, formState.enabled);
    expect(customized.has('comp3')).toBe(false);
  });

  it('Bug 2: editing comp1, navigating, then excluding comp3 preserves BOTH comp1 edits and comp3 exclusion', () => {
    let formState = resolveInitialFormState(components, [], undefined);

    // Step A: Edit comp1 values -> comp1 is customized
    formState.values.comp1 = { replicas: 3 };
    let overrides = diffComponentOverrides(defaults, formState.values, defaultsEnabled, formState.enabled);
    expect(overrides).toEqual([{ componentName: 'comp1', values: { replicas: 3 } }]);
    let customized = customizedComponentNames(defaults, formState.values, formState.enabled);
    expect(customized).toEqual(new Set(['comp1']));

    // Step B: Move to Review and return to Configuration -> comp1 edits persist
    formState = resolveInitialFormState(components, [], overrides);
    expect(formState.values.comp1).toEqual({ replicas: 3 });
    customized = customizedComponentNames(defaults, formState.values, formState.enabled);
    expect(customized).toEqual(new Set(['comp1']));

    // Step C: Exclude comp3 -> diff contains BOTH comp1 and comp3; comp1 is Customized, comp3 is deselected (no Customized badge)
    formState.enabled.comp3 = false;
    overrides = diffComponentOverrides(defaults, formState.values, defaultsEnabled, formState.enabled);
    expect(overrides).toEqual([
      { componentName: 'comp1', values: { replicas: 3 } },
      { componentName: 'comp3', enabled: false },
    ]);
    customized = customizedComponentNames(defaults, formState.values, formState.enabled);
    expect(customized).toEqual(new Set(['comp1']));

    // Step D: Move to Review and return again -> BOTH persist; comp1 customized, comp3 deselected
    formState = resolveInitialFormState(components, [], overrides);
    expect(formState.values.comp1).toEqual({ replicas: 3 });
    expect(formState.enabled.comp3).toBe(false);
    customized = customizedComponentNames(defaults, formState.values, formState.enabled);
    expect(customized).toEqual(new Set(['comp1']));
  });
});
