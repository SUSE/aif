// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { RadioGroup } from '@components/Form/Radio';
import { Banner } from '@components/Banner';
import KustomizeOverlayEditor from '../wizard/KustomizeOverlayEditor.vue';
import type { ComponentKustomize } from '../../../types/blueprint-types';

// CodeMirror needs a real layout engine; the tests only care what it is given.
vi.mock('@shell/components/YamlEditor', async(importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  default: {
    name:     'YamlEditorStub',
    props:    ['value', 'editorMode'],
    emits:    ['update:value'],
    template: '<pre class="yaml-stub">{{ value }}</pre>',
  },
}));
vi.mock('@shell/components/form/LabeledSelect', () => ({
  default: {
    name: 'LabeledSelectStub', props: ['value', 'options', 'label', 'mode'], emits: ['update:value'], template: '<div class="select-stub" />',
  },
}));

function mountEditor(props: Record<string, unknown> = {}) {
  return mount(KustomizeOverlayEditor, {
    props: {
      value: undefined, name: 'comp', gitRepoOptions: [], ...props,
    },
    // Rancher registers these globally; the editor passes plain labels, so
    // they are not exercised here.
    global: {
      // Rancher's global t(); a globalProperty, so the component's own t still wins.
      plugins:    [{
        install: (app) => {
          // The shell also types an options-object overload, which these components never use.
          app.config.globalProperties.t = ((key: string) => key) as unknown as typeof app.config.globalProperties.t;
        },
      }],
      directives: { 'clean-html': {}, 'clean-tooltip': {} },
      stubs:      { t: true },
    },
  });
}

function lastEmitted(wrapper: ReturnType<typeof mountEditor>): ComponentKustomize | undefined {
  const events = wrapper.emitted('update:value') || [];

  return events[events.length - 1]?.[0] as ComponentKustomize | undefined;
}

describe('KustomizeOverlayEditor', () => {
  it('treats an untouched inline starter as no overlay', async() => {
    const wrapper = mountEditor();

    await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'inline');

    expect(wrapper.emitted('update:value')).toBeTruthy();
    expect(lastEmitted(wrapper)).toBeUndefined();
  });

  it('emits the inline kustomization under the default overlay dir', async() => {
    const wrapper = mountEditor();

    await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'inline');
    await wrapper.findComponent({ name: 'YamlEditorStub' }).vm.$emit('update:value', 'patches: []');

    expect(lastEmitted(wrapper)).toEqual({ dir: 'overlay', files: { 'overlay/kustomization.yaml': 'patches: []' } });
  });

  it('preserves an existing overlay dir on re-emit', async() => {
    const wrapper = mountEditor({ value: { dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []' } } });

    await wrapper.findComponent({ name: 'YamlEditorStub' }).vm.$emit('update:value', 'commonLabels: {a: b}');

    expect(lastEmitted(wrapper)).toEqual({ dir: 'ov', files: { 'ov/kustomization.yaml': 'commonLabels: {a: b}' } });
  });

  it('explains that an overlay embeds the chart', async() => {
    const wrapper = mountEditor();

    await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'inline');

    const info = wrapper.findAllComponents(Banner).find((b) => b.props('color') === 'info');

    expect(info?.text()).toContain('embeds the chart in the Fleet bundle');
    expect(wrapper.findAllComponents(Banner).some((b) => b.props('color') === 'warning')).toBe(false);
  });

  it('points to AI Factory Settings when no custom git repo exists', async() => {
    const wrapper = mountEditor({ gitRepoOptions: [] });

    await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'git');

    const warning = wrapper.findAllComponents(Banner).find((b) => b.props('color') === 'warning');

    expect(warning?.text()).toContain('No Git-type repositories with an HTTPS URL found');
  });

  it('shows no empty-repo warning while repos load or when some exist', async() => {
    for (const gitRepoOptions of [null, ['my-overlays']]) {
      const wrapper = mountEditor({ gitRepoOptions });

      await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'git');

      expect(wrapper.findAllComponents(Banner).some((b) => b.props('color') === 'warning')).toBe(false);
    }
  });

  it('renders an inline overlay read-only in view mode', () => {
    const wrapper = mountEditor({ mode: 'view', value: { dir: 'overlay', files: { 'overlay/kustomization.yaml': 'patches: []' } } });

    expect(wrapper.findComponent(RadioGroup).exists()).toBe(false);
    expect(wrapper.findComponent({ name: 'YamlEditorStub' }).props('editorMode')).toBe('VIEW_CODE');
  });

  it('renders nothing in view mode without an overlay', () => {
    const wrapper = mountEditor({ mode: 'view' });

    expect(wrapper.findComponent(RadioGroup).exists()).toBe(false);
    expect(wrapper.findComponent({ name: 'YamlEditorStub' }).exists()).toBe(false);
  });
});
