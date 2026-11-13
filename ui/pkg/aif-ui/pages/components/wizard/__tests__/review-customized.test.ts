import { describe, expect, it } from 'vitest';
import {
  customizedOverrides, deployOrder, effectiveComponents, effectiveDeployOrder,
} from '../../../../utils/blueprint-customize';
import type { BlueprintComponent } from '../../../../types/blueprint-types';

const overlay = { dir: 'overlay', files: { 'overlay/kustomization.yaml': 'patches: []' } };

describe('install review: customized components and deploy order', () => {
  it('lists overlay-only and order-only overrides with what changed', () => {
    expect(customizedOverrides([
      { componentName: 'a', kustomize: overlay },
      { componentName: 'b', dependsOn: [] },
      { componentName: 'c', values: { x: 1 }, kustomize: overlay },
    ])).toEqual([
      { componentName: 'a', changes: ['overlay'] },
      { componentName: 'b', changes: ['order'] },
      { componentName: 'c', changes: ['values', 'overlay'] },
    ]);
  });

  it('omits excluded components and enable-only entries', () => {
    expect(customizedOverrides([
      { componentName: 'a', enabled: false, kustomize: overlay },
      { componentName: 'b', enabled: true },
    ])).toEqual([]);
  });

  it('computes waves from the effective order', () => {
    const components: BlueprintComponent[] = [
      { chartRepo: 'r', chartName: 'a', chartVersion: '1' },
      { chartRepo: 'r', chartName: 'b', chartVersion: '1', dependsOn: ['a'] },
    ];

    expect(deployOrder(components)).toEqual([['a'], ['b']]);
    expect(deployOrder(effectiveComponents(components, [{ componentName: 'b', dependsOn: [] }]))).toEqual([['a', 'b']]);
  });

  it('drops excluded components and their edges from the effective order', () => {
    const components: BlueprintComponent[] = [
      { chartRepo: 'r', chartName: 'a', chartVersion: '1' },
      { chartRepo: 'r', chartName: 'b', chartVersion: '1', dependsOn: ['a'] },
      { chartRepo: 'r', chartName: 'c', chartVersion: '1', dependsOn: ['b'] },
    ];

    expect(effectiveDeployOrder(components, [{ componentName: 'a', enabled: false }])).toEqual([['b'], ['c']]);
    expect(effectiveDeployOrder(components, [{ componentName: 'a', enabled: false }, { componentName: 'c', dependsOn: [] }])).toEqual([['b', 'c']]);
  });
});
