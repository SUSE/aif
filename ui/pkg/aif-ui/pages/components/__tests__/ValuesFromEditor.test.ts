// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import ValuesFromEditor from '../wizard/ValuesFromEditor.vue';
import { wizardMountGlobal } from './wizard-mount';
import type { ComponentValuesFrom } from '../../../types/blueprint-types';

vi.mock('@shell/components/form/LabeledSelect', () => ({
  default: {
    name: 'LabeledSelectStub', props: ['value', 'options', 'label', 'mode'], emits: ['update:value'], template: '<div class="select-stub" />',
  },
}));

const sources: ComponentValuesFrom[] = [
  { secretKeyRef: { name: 'runai-cluster-values', namespace: 'runai-backend', key: 'values.yaml' } },
  { configMapKeyRef: { name: 'other-values' } },
];

async function mountEditor(mode?: 'edit' | 'view') {
  const wrapper = mount(ValuesFromEditor, {
    props:  { value: sources, name: 'runai-cluster', ...(mode ? { mode } : {}) },
    global: wizardMountGlobal(),
  });

  await flushPromises();

  return wrapper;
}

const input = (wrapper: Awaited<ReturnType<typeof mountEditor>>, testid: string) => wrapper.find(`input[data-testid="${ testid }"]`);

describe('ValuesFromEditor edit mode', () => {
  it('renders each source with add and remove controls', async() => {
    const wrapper = await mountEditor();

    expect((input(wrapper, 'values-from-runai-cluster-name-1').element as HTMLInputElement).value).toBe('other-values');
    expect(wrapper.find('[data-testid="values-from-runai-cluster-button"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="values-from-runai-cluster-remove-item-0"]').exists()).toBe(true);
    expect(wrapper.text()).toContain('Leave Namespace empty');
  });

  it('emits the edited sources right away', async() => {
    const wrapper = await mountEditor();

    await input(wrapper, 'values-from-runai-cluster-key-0').setValue('custom.yaml');

    expect(wrapper.emitted('update:value')?.at(-1)?.[0]).toEqual([
      { secretKeyRef: { name: 'runai-cluster-values', namespace: 'runai-backend', key: 'custom.yaml' } },
      { configMapKeyRef: { name: 'other-values' } },
    ]);
  });
});

describe('ValuesFromEditor view mode', () => {
  it('renders the sources read-only, without add or remove', async() => {
    const wrapper = await mountEditor('view');

    expect(wrapper.find('[data-testid="values-from-runai-cluster-button"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="values-from-runai-cluster-remove-item-0"]').exists()).toBe(false);
    for (const field of ['name', 'namespace', 'key']) {
      expect((input(wrapper, `values-from-runai-cluster-${ field }-0`).element as HTMLInputElement).disabled).toBe(true);
    }
    expect((input(wrapper, 'values-from-runai-cluster-name-0').element as HTMLInputElement).value).toBe('runai-cluster-values');
    expect(wrapper.findAllComponents({ name: 'LabeledSelectStub' }).map((s) => s.props('mode'))).toEqual(['view', 'view']);
  });

  it('explains the sources to an installer and emits nothing', async() => {
    const wrapper = await mountEditor('view');

    expect(wrapper.text()).toContain('Set by the Blueprint');
    expect(wrapper.text()).not.toContain('Leave Namespace empty');
    await flushPromises();
    expect(wrapper.emitted('update:value')).toBeUndefined();
  });
});
