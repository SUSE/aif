import { describe, it, expect } from 'vitest';
import type { BlueprintComponent } from '../blueprint-types';

describe('BlueprintComponent kustomize/dependsOn', () => {
  it('accepts inline kustomize + dependsOn shape', () => {
    const c: BlueprintComponent = {
      chartRepo: 'r', chartName: 'n', chartVersion: '1.0.0',
      dependsOn: ['other'],
      kustomize: { dir: 'overlay', files: { 'overlay/kustomization.yaml': 'resources: []' } },
    };
    expect(c.kustomize?.dir).toBe('overlay');
    expect(c.dependsOn).toEqual(['other']);
  });
});
