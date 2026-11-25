// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import BlueprintReviewCreateStep from '../wizard/BlueprintReviewCreateStep.vue';
import { wizardMountGlobal } from './wizard-mount';
import type { BlueprintSpec } from '../../../types/blueprint-types';

function mountStep(extra: Partial<BlueprintSpec> = {}) {
  const form: BlueprintSpec = {
    displayName: 'Run:ai',
    version:     '1.0.0',
    components:  [{
      chartRepo:    'r',
      chartName:    'runai-cluster',
      chartVersion: '2.0.0',
      valuesFrom:   [
        { secretKeyRef: { name: 'runai-cluster-values', namespace: 'runai-backend', key: 'values.yaml' } },
        { configMapKeyRef: { name: 'runai-config' } },
      ],
    }, { chartRepo: 'r', chartName: 'plain', chartVersion: '1.0.0' }],
    ...extra,
  };

  return mount(BlueprintReviewCreateStep, { props: { form }, global: wizardMountGlobal() });
}

describe('BlueprintReviewCreateStep', () => {
  it('shows rollback as Allowed when unset and Disabled when disabled', () => {
    expect(mountStep().find('[data-testid="review-rollback"]').text()).toBe('Allowed');
    expect(mountStep({ rollback: 'Disabled' }).find('[data-testid="review-rollback"]').text()).toBe('Disabled');
  });

  it('lists each component valuesFrom source', () => {
    const lines = mountStep().findAll('.comp-values-from').map((s) => s.text());

    expect(lines).toEqual([
      'Values from Secret runai-backend/runai-cluster-values (key values.yaml)',
      'Values from ConfigMap runai-config',
    ]);
  });
});
