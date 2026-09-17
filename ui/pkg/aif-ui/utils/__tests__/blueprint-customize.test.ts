import { describe, it, expect } from 'vitest';
import {
  deepMergeValues, seedComponentValues, diffComponentValues,
  seedComponentEnabled, diffComponentOverrides,
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
