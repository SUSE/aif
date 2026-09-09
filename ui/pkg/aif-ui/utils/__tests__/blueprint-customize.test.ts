import { describe, it, expect } from 'vitest';
import { deepMergeValues, seedComponentValues, diffComponentValues } from '../blueprint-customize';
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
