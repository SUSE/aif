// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import type { App } from 'vue';
import BlueprintCreate from '../BlueprintCreate.vue';
import { getBlueprint } from '../../utils/blueprint-api';
import type { Blueprint } from '../../types/blueprint-types';

vi.mock('@shell/components/Loading', () => ({ default: { template: '<div />' } }));
vi.mock('../components/BlueprintCreateWizard.vue', () => ({ default: { name: 'WizardStub', props: ['editName', 'fromVersion', 'prefill'], template: '<div />' } }));
vi.mock('../../utils/blueprint-api', async(importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  getBlueprint: vi.fn(),
}));

const runai: Blueprint = {
  apiVersion: 'ai-factory.suse.com/v1alpha1',
  kind:       'Blueprint',
  metadata:   { name: 'nvidia-runai-2-0-0' },
  spec:       {
    displayName: 'NVIDIA Run:ai',
    version:     '2.0.0',
    source:      'Nvidia',
    icon:        'https://example.com/runai.png',
    rollback:    'Disabled',
    components:  [{
      chartRepo: 'r', chartName: 'runai-cluster', chartVersion: '2.0.0', valuesFrom: [{ secretKeyRef: { name: 'runai-cluster-values' } }],
    }],
  },
};

async function mountWithQuery(query: Record<string, string>) {
  vi.mocked(getBlueprint).mockResolvedValue(runai);
  // A globalProperty rather than a VTU mock: the page reads $route during setup.
  const wrapper = mount(BlueprintCreate, {
    global: {
      plugins: [{
        install: (app: App) => {
          Object.assign(app.config.globalProperties, { $route: { query } });
        },
      }],
    },
  });

  await flushPromises();

  return wrapper.findComponent({ name: 'WizardStub' }).props('prefill');
}

describe('BlueprintCreate prefill', () => {
  it('carries rollback and valuesFrom into a copy, but not source or icon', async() => {
    const prefill = await mountWithQuery({ copyFrom: 'NVIDIA Run:ai', copyVersion: '2.0.0' });

    expect(prefill.rollback).toBe('Disabled');
    expect(prefill.components[0].valuesFrom).toEqual(runai.spec.components[0].valuesFrom);
    expect(prefill.source).toBeUndefined();
    expect(prefill.icon).toBeUndefined();
  });

  it('keeps rollback when editing', async() => {
    const prefill = await mountWithQuery({ editName: 'NVIDIA Run:ai', fromVersion: '2.0.0' });

    expect(prefill.rollback).toBe('Disabled');
    expect(prefill.version).toBe('2.0.1');
  });
});
