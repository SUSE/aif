// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import BlueprintInstallReviewStep from '../wizard/BlueprintInstallReviewStep.vue';
import { wizardMountGlobal } from './wizard-mount';
import type { BlueprintComponent, BlueprintSpec } from '../../../types/blueprint-types';

const components: BlueprintComponent[] = [{
  chartRepo:    'r',
  chartName:    'runai-cluster',
  chartVersion: '2.0.0',
  valuesFrom:   [
    { secretKeyRef: { name: 'runai-cluster-values', namespace: 'runai-backend', key: 'values.yaml' } },
    { configMapKeyRef: { name: 'runai-config' } },
  ],
}, { chartRepo: 'r', chartName: 'plain', chartVersion: '1.0.0' }];

function mountStep(rollback?: BlueprintSpec['rollback']) {
  return mount(BlueprintInstallReviewStep, {
    props: {
      workloadName: 'runai', namespace: 'runai-ns', displayName: 'Run:ai', version: '1.0.0', componentCount: 2, deployType: 'FleetBundle', clusters: ['c-1'], components, rollback,
    },
    global: wizardMountGlobal(),
  });
}

describe('BlueprintInstallReviewStep', () => {
  it('shows rollback as Allowed when unset and Disabled when disabled', () => {
    expect(mountStep().find('[data-testid="review-rollback"]').text()).toBe('Allowed');
    expect(mountStep('Disabled').find('[data-testid="review-rollback"]').text()).toBe('Disabled');
  });

  it('lists each component valuesFrom source', () => {
    const lines = mountStep().findAll('.comp-values-from').map((s) => s.text());

    expect(lines).toEqual([
      'Values from Secret runai-backend/runai-cluster-values (key values.yaml)',
      'Values from ConfigMap runai-config',
    ]);
  });
});
