import { describe, it, expect } from 'vitest';
import { catalogDisplayName, familiesInCatalog } from '../catalog-api';
import type { BlueprintCatalog } from '../../types/catalog-types';

function makeCatalog(overrides: Partial<BlueprintCatalog['spec']> = {}, name = 'suse-default'): BlueprintCatalog {
  return {
    apiVersion: 'ai-factory.suse.com/v1alpha1',
    kind:       'BlueprintCatalog',
    metadata:   { name },
    spec:       { ...overrides },
  };
}

describe('catalogDisplayName', () => {
  it('uses spec.displayName when set', () => {
    expect(catalogDisplayName(makeCatalog({ displayName: 'SUSE Default' }))).toBe('SUSE Default');
  });

  it('falls back to metadata.name when displayName is absent', () => {
    expect(catalogDisplayName(makeCatalog({}, 'suse-default'))).toBe('suse-default');
  });

  it('falls back to metadata.name when displayName is empty/whitespace', () => {
    expect(catalogDisplayName(makeCatalog({ displayName: '   ' }, 'suse-default'))).toBe('suse-default');
  });
});

describe('familiesInCatalog', () => {
  it('returns the set of member names', () => {
    const cat = makeCatalog({ blueprints: [{ name: 'llama' }, { name: 'stable-diffusion' }] });
    expect(familiesInCatalog(cat)).toEqual(new Set(['llama', 'stable-diffusion']));
  });

  it('returns an empty set when spec.blueprints is undefined', () => {
    expect(familiesInCatalog(makeCatalog({}))).toEqual(new Set());
  });
});
