import { describe, it, expect } from 'vitest';
import type { Blueprint, BlueprintComponent } from '../blueprint-types';

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

describe('BlueprintComponent valuesFrom', () => {
  it('round-trips a Blueprint with valuesFrom', () => {
    const bp: Blueprint = {
      apiVersion: 'ai-factory.suse.com/v1alpha1',
      kind:       'Blueprint',
      metadata:   { name: 'runai-1-0-0' },
      spec:       {
        displayName: 'Run:ai',
        version:     '1.0.0',
        components:  [{
          chartRepo:  'r', chartName: 'runai-cluster', chartVersion: '2.18.0',
          valuesFrom: [{ secretKeyRef: { name: 'runai-cluster-values', namespace: 'runai-backend', key: 'values.yaml' } }],
        }],
      },
    };
    const out: Blueprint = JSON.parse(JSON.stringify(bp));

    expect(out).toEqual(bp);
    expect(out.spec.components[0].valuesFrom?.[0].secretKeyRef?.name).toBe('runai-cluster-values');
  });
});
