// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { Checkbox } from '@components/Form/Checkbox';
import BlueprintInstallBasicInfoStep from '../wizard/BlueprintInstallBasicInfoStep.vue';
import { wizardMountGlobal } from './wizard-mount';
import type { BlueprintSpec } from '../../../types/blueprint-types';

vi.mock('../wizard/NamespaceAutocomplete.vue', () => ({ default: { template: '<div />' } }));
vi.mock('../../../services/rancher-apps', () => ({ fetchUserNamespaces: vi.fn(async() => []) }));

async function mountStep(rollback?: BlueprintSpec['rollback']) {
  const wrapper = mount(BlueprintInstallBasicInfoStep, {
    props: {
      displayName: 'Run:ai', version: '1.0.0', componentCount: 1, workloadName: 'runai', namespace: 'runai-ns', components: [], rollback,
    },
    global: wizardMountGlobal(),
  });

  await flushPromises();

  return wrapper;
}

describe('BlueprintInstallBasicInfoStep rollback', () => {
  it.each([undefined, 'Allowed' as const])('shows rollback allowed, read-only, when the Blueprint sets %s', async(rollback) => {
    const checkbox = (await mountStep(rollback)).findComponent(Checkbox);

    expect(checkbox.props()).toMatchObject({ value: true, label: 'Allow rollback', mode: 'view' });
    expect(checkbox.props('description')).toBeFalsy();
  });

  it('shows rollback disabled, read-only, with the reason when the Blueprint disables it', async() => {
    const checkbox = (await mountStep('Disabled')).findComponent(Checkbox);

    expect(checkbox.props()).toMatchObject({ value: false, mode: 'view' });
    expect(checkbox.text()).toContain('Rollback is disabled for this Blueprint');
  });
});
