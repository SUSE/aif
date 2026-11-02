// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { listGitClusterRepoNames } from '../../../services/rancher-apps';
import { Banner } from '@components/Banner';
import BlueprintCustomizeStep from '../wizard/BlueprintCustomizeStep.vue';
import KustomizeOverlayEditor from '../wizard/KustomizeOverlayEditor.vue';
import type { BlueprintComponent } from '../../../types/blueprint-types';
import type { ComponentValueOverride } from '../../../types/aiworkload-types';

vi.mock('@shell/components/YamlEditor', async(importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  default: { props: ['value', 'editorMode'], template: '<pre class="yaml-stub">{{ value }}</pre>' },
}));
vi.mock('@shell/components/form/LabeledSelect', () => ({
  default: {
    name: 'LabeledSelectStub', props: ['value', 'options', 'label', 'mode'], emits: ['update:value'], template: '<div class="select-stub" />',
  },
}));
// Tab switching is Rancher's; render both tabs so their content is inspectable.
vi.mock('@shell/components/Tabbed', () => ({ default: { name: 'TabbedStub', template: '<div class="tabbed-stub"><slot /></div>' } }));
vi.mock('@shell/components/Tabbed/Tab', () => ({ default: { props: ['name', 'label'], template: '<section :data-tab="name"><slot /></section>' } }));
vi.mock('../wizard/ValuesStep.vue', () => ({ default: { template: '<div class="values-stub" />' } }));
vi.mock('../../../services/rancher-apps', () => ({ listGitClusterRepoNames: vi.fn(async() => []) }));

const overlay = { dir: 'overlay', files: { 'overlay/kustomization.yaml': 'patches: []' } };

function component(chartName: string, extra: Partial<BlueprintComponent> = {}): BlueprintComponent {
  return {
    chartRepo: 'r', chartName, chartVersion: '1.0.0', ...extra,
  };
}

async function mountStep(components: BlueprintComponent[], existingValues: ComponentValueOverride[] = []) {
  const store = { dispatch: vi.fn(async() => ({})), getters: {} };
  const wrapper = mount(BlueprintCustomizeStep, {
    props:  { components, existingValues },
    global: {
      mocks:      { $store: store },
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

  await flushPromises();

  return wrapper;
}

function lastModelValue(wrapper: Awaited<ReturnType<typeof mountStep>>): ComponentValueOverride[] {
  const events = wrapper.emitted('update:modelValue') || [];

  return (events[events.length - 1]?.[0] || []) as ComponentValueOverride[];
}

describe('BlueprintCustomizeStep Advanced tab', () => {
  it('seeds deploy order from the blueprint and emits an override when it changes', async() => {
    const wrapper = await mountStep([component('b', { dependsOn: ['a'] }), component('a')]);
    const select = wrapper.findComponent({ name: 'LabeledSelectStub' });

    expect(select.props('value')).toEqual(['a']);
    await select.vm.$emit('update:value', []);
    expect(lastModelValue(wrapper)).toEqual([{ componentName: 'b', dependsOn: [] }]);
  });

  it('shows the blueprint overlay read-only above an editable install overlay', async() => {
    const wrapper = await mountStep([component('b', { kustomize: overlay }), component('a')]);
    const editors = wrapper.findAllComponents(KustomizeOverlayEditor);

    expect(editors.map((e) => e.props('mode'))).toEqual(['view', 'edit']);
    expect(editors[0].props('value')).toEqual(overlay);
    expect(editors[1].props('value')).toBeUndefined();
  });

  it('warns about stale dependencies and emits overrides without them', async() => {
    const wrapper = await mountStep(
      [component('b', { dependsOn: ['a'] }), component('a')],
      [{ componentName: 'b', dependsOn: ['gone', 'a'] }],
    );
    const warning = wrapper.findAllComponents(Banner).find((b) => b.props('color') === 'warning');

    expect(warning?.text()).toContain('gone');
    expect(JSON.stringify(lastModelValue(wrapper))).not.toContain('gone');
  });

  it('blocks Next when the edited order forms a cycle', async() => {
    const wrapper = await mountStep([component('a'), component('b', { dependsOn: ['a'] })]);

    await wrapper.findComponent({ name: 'LabeledSelectStub' }).vm.$emit('update:value', ['b']);

    const valid = wrapper.emitted('update:valid') || [];

    expect(valid[valid.length - 1]).toEqual([false]);
    expect(wrapper.findAllComponents(Banner).some((b) => b.props('color') === 'error')).toBe(true);
  });

  it('allows an install overlay on a chart from a git-backed repo', async() => {
    vi.mocked(listGitClusterRepoNames).mockResolvedValueOnce(['r']);
    const wrapper = await mountStep([component('b'), component('a')]);

    await wrapper.findAllComponents(KustomizeOverlayEditor)[0].vm.$emit('update:value', overlay);

    const valid = wrapper.emitted('update:valid') || [];

    expect(valid[valid.length - 1]).toEqual([true]);
    expect(lastModelValue(wrapper)).toEqual([{ componentName: 'b', kustomize: overlay }]);
  });

  it('shows the excluded note instead of tabs for an excluded component', async() => {
    const wrapper = await mountStep([component('b'), component('a')], [{ componentName: 'b', enabled: false }]);

    expect(wrapper.text()).toContain('Excluded');
    expect(wrapper.findComponent({ name: 'TabbedStub' }).exists()).toBe(false);
  });
});
