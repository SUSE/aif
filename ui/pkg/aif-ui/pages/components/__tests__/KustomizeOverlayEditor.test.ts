// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { RadioGroup } from '@components/Form/Radio';
import { Banner } from '@components/Banner';
import { LabeledInput } from '@components/Form/LabeledInput';
import KustomizeOverlayEditor from '../wizard/KustomizeOverlayEditor.vue';
import { KUSTOMIZE_DRAFTS } from '../../../composables/useKustomizeDrafts';
import type { KustomizeDraft } from '../../../composables/useKustomizeDrafts';
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
// Every tab renders at once, so each file's editor is reachable by index.
vi.mock('@shell/components/Tabbed', () => ({
  default: {
    name:     'TabbedStub',
    emits:    ['changed'],
    methods:  {
      select(this: { $emit: (e: string, p: unknown) => void }, name: string) {
        this.$emit('changed', { selectedName: name });
      },
    },
    template: '<div class="tabbed-stub"><slot name="tab-row-extras" /><slot /></div>',
  },
}));
vi.mock('@shell/components/Tabbed/Tab', () => ({
  default: {
    name: 'TabStub', props: ['name', 'label', 'weight', 'displayAlertIcon', 'errorIconTooltip'], template: '<section :data-tab="name"><slot /></section>',
  },
}));
vi.mock('@shell/components/form/LabeledSelect', () => ({
  default: {
    name: 'LabeledSelectStub', props: ['value', 'options', 'label', 'mode'], emits: ['update:value'], template: '<div class="select-stub" />',
  },
}));

function mountEditor(props: Record<string, unknown> = {}, drafts?: Map<string, KustomizeDraft>) {
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
      provide:    drafts ? { [KUSTOMIZE_DRAFTS as symbol]: drafts } : {},
    },
  });
}

function lastEmitted(wrapper: ReturnType<typeof mountEditor>): ComponentKustomize | undefined {
  const events = wrapper.emitted('update:value') || [];

  return events[events.length - 1]?.[0] as ComponentKustomize | undefined;
}

// The package build type-checks the SFC's real props, under which mountEditor's
// loosely typed props leave `value` out of setProps' accepted keys.
function setValue(wrapper: ReturnType<typeof mountEditor>, value: ComponentKustomize | undefined) {
  return (wrapper.setProps as (props: Record<string, unknown>) => Promise<void>)({ value });
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

  it('adds a file as a new tab and emits it under dir', async() => {
    const wrapper = mountEditor({ value: { dir: 'ov', files: { 'ov/kustomization.yaml': 'resources:\n  - ns.yaml\n' } } });

    await wrapper.findAll('button').find((b) => b.text().includes('Add file'))!.trigger('click');
    await wrapper.findComponent(LabeledInput).vm.$emit('update:value', 'ns.yaml');
    await wrapper.findAll('button').find((b) => b.text() === 'Add')!.trigger('click');

    expect(wrapper.findAllComponents({ name: 'TabStub' }).map((t) => t.props('name'))).toEqual(['kustomization.yaml', 'ns.yaml']);
    await wrapper.findAllComponents({ name: 'YamlEditorStub' })[1].vm.$emit('update:value', 'kind: Namespace\n');
    expect(lastEmitted(wrapper)!.files).toEqual({ 'ov/kustomization.yaml': 'resources:\n  - ns.yaml\n', 'ov/ns.yaml': 'kind: Namespace\n' });
  });

  it('removes the active non-root file and offers no remove for the root', async() => {
    const wrapper = mountEditor({ value: { dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []', 'ov/ns.yaml': 'n' } } });
    const removeButton = () => wrapper.findAll('button').find((b) => b.text().startsWith('Remove'));

    expect(removeButton()).toBeUndefined();
    await wrapper.findComponent({ name: 'TabbedStub' }).vm.$emit('changed', { selectedName: 'ns.yaml' });
    await removeButton()!.trigger('click');

    expect(lastEmitted(wrapper)!.files).toEqual({ 'ov/kustomization.yaml': 'patches: []' });
    expect(removeButton()).toBeUndefined();
  });

  it('warns about and marks files the kustomization does not reference', () => {
    const wrapper = mountEditor({ value: { dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []', 'ov/ns.yaml': 'n' } } });
    const warning = wrapper.findAllComponents(Banner).find((b) => b.props('color') === 'warning');

    expect(warning?.text()).toContain('ns.yaml');
    expect(wrapper.findAllComponents({ name: 'TabStub' }).map((t) => t.props('displayAlertIcon'))).toEqual([false, true]);
  });

  it('keeps files outside dir unchanged on re-emit and warns about them', async() => {
    const wrapper = mountEditor({ value: { dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []', 'other/x.yaml': 'x' } } });

    expect(wrapper.findAllComponents(Banner).some((b) => b.props('color') === 'warning' && b.text().includes('other/x.yaml'))).toBe(true);
    await wrapper.findComponent({ name: 'YamlEditorStub' }).vm.$emit('update:value', 'patches: [] # edited');
    expect(lastEmitted(wrapper)!.files).toEqual({ 'ov/kustomization.yaml': 'patches: [] # edited', 'other/x.yaml': 'x' });
  });

  it('re-seeds from a value it did not emit', async() => {
    const wrapper = mountEditor({ mode: 'view', value: { dir: 'ov', files: { 'ov/kustomization.yaml': 'a: 1' } } });

    await setValue(wrapper, { dir: 'ov', files: { 'ov/kustomization.yaml': 'b: 2', 'ov/x.yaml': 'x' } });

    expect(wrapper.findAllComponents({ name: 'YamlEditorStub' }).map((e) => e.props('value'))).toEqual(['b: 2', 'x']);
  });

  it('keeps authoring state when the parent echoes the emitted value back', async() => {
    const wrapper = mountEditor({ value: { dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []' } } });

    await wrapper.findAll('button').find((b) => b.text().includes('Add file'))!.trigger('click');
    await wrapper.findComponent({ name: 'YamlEditorStub' }).vm.$emit('update:value', 'patches: [] # edited');
    await setValue(wrapper, structuredClone(lastEmitted(wrapper)));

    expect(wrapper.findComponent(LabeledInput).exists()).toBe(true);
  });

  it('renders nothing in view mode without an overlay', () => {
    const wrapper = mountEditor({ mode: 'view' });

    expect(wrapper.findComponent(RadioGroup).exists()).toBe(false);
    expect(wrapper.findComponent({ name: 'YamlEditorStub' }).exists()).toBe(false);
  });
});

// A wizard step unmounts the editor on navigation and remounts it with the
// value it last emitted; the wizard's draft store carries the rest.
describe('KustomizeOverlayEditor drafts across remounts', () => {
  async function addFile(wrapper: ReturnType<typeof mountEditor>, name: string, content?: string) {
    await wrapper.findAll('button').find((b) => b.text().includes('Add file'))!.trigger('click');
    await wrapper.findComponent(LabeledInput).vm.$emit('update:value', name);
    await wrapper.findAll('button').find((b) => b.text() === 'Add')!.trigger('click');
    if (content !== undefined) {
      const editors = wrapper.findAllComponents({ name: 'YamlEditorStub' });

      await editors[editors.length - 1].vm.$emit('update:value', content);
    }
  }
  function remount(wrapper: ReturnType<typeof mountEditor>, drafts?: Map<string, KustomizeDraft>) {
    const value = lastEmitted(wrapper);

    wrapper.unmount();

    return mountEditor({ value }, drafts);
  }
  const tabNames = (wrapper: ReturnType<typeof mountEditor>) => wrapper.findAllComponents({ name: 'TabStub' }).map((t) => t.props('name'));

  it.each([
    ['an empty new file', undefined],
    ['a comment-only new file', '# todo\n'],
    ['a new file with content', 'kind: ConfigMap\n'],
  ])('keeps %s, the example tab and the inline choice', async(_, content) => {
    const drafts = new Map<string, KustomizeDraft>();
    const wrapper = mountEditor({}, drafts);

    await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'inline');
    await addFile(wrapper, 'extra.yaml', content);
    const again = remount(wrapper, drafts);

    expect(again.findComponent(RadioGroup).props('value')).toBe('inline');
    expect(tabNames(again)).toEqual(['kustomization.yaml', 'configmap.yaml', 'extra.yaml']);
    expect(again.findAllComponents({ name: 'YamlEditorStub' })[2].props('value')).toBe(content ?? '');
  });

  it('restores the selected file tab', async() => {
    const drafts = new Map<string, KustomizeDraft>();
    const wrapper = mountEditor({}, drafts);

    await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'inline');
    await wrapper.findComponent({ name: 'TabbedStub' }).vm.$emit('changed', { selectedName: 'configmap.yaml' });
    const again = remount(wrapper, drafts);

    expect(again.findComponent({ name: 'TabbedStub' }).attributes('default-tab')).toBe('configmap.yaml');
  });

  it('ignores a draft that no longer matches the value', async() => {
    const drafts = new Map<string, KustomizeDraft>();
    const wrapper = mountEditor({}, drafts);

    await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'inline');
    await addFile(wrapper, 'extra.yaml');
    wrapper.unmount();
    const again = mountEditor({ value: { dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []' } } }, drafts);

    expect(tabNames(again)).toEqual(['kustomization.yaml']);
  });

  it('keeps drafts per editor name', async() => {
    const drafts = new Map<string, KustomizeDraft>();
    const wrapper = mountEditor({}, drafts);

    await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'inline');
    wrapper.unmount();

    expect(mountEditor({ name: 'other' }, drafts).findComponent(RadioGroup).props('value')).toBe('none');
  });

  it('starts over without a draft store, as outside a wizard', async() => {
    const wrapper = mountEditor();

    await wrapper.findComponent(RadioGroup).vm.$emit('update:value', 'inline');
    await addFile(wrapper, 'extra.yaml');

    expect(remount(wrapper).findComponent(RadioGroup).props('value')).toBe('none');
  });
});
