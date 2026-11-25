// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { listGitClusterRepoNames } from '../../../services/rancher-apps';
import { Banner } from '@components/Banner';
import { RadioGroup } from '@components/Form/Radio';
import BlueprintCustomizeStep from '../wizard/BlueprintCustomizeStep.vue';
import KustomizeOverlayEditor from '../wizard/KustomizeOverlayEditor.vue';
import ValuesFromEditor from '../wizard/ValuesFromEditor.vue';
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
vi.mock('../wizard/ValuesStep.vue', () => ({ default: { name: 'ValuesStepStub', template: '<div class="values-stub" />' } }));
vi.mock('../../../services/rancher-apps', () => ({ listGitClusterRepoNames: vi.fn(async() => []) }));

const overlay = { dir: 'overlay', files: { 'overlay/kustomization.yaml': 'patches: []' } };

function component(chartName: string, extra: Partial<BlueprintComponent> = {}): BlueprintComponent {
  return {
    chartRepo: 'r', chartName, chartVersion: '1.0.0', ...extra,
  };
}

async function mountStep(components: BlueprintComponent[], existingValues: ComponentValueOverride[] = [], props: Record<string, any> = {}) {
  const store = { dispatch: vi.fn(async() => ({})), getters: {} };
  const wrapper = mount(BlueprintCustomizeStep, {
    props:  { components, existingValues, ...props },
    global: {
      mocks:      { $store: store },
      // Rancher's global t(); a globalProperty, so the component's own t still wins.
      plugins:    [{
        install: (app) => {
          // The shell also types an options-object overload, which these components never use.
          app.config.globalProperties.t = ((key: string, params?: any) => {
            if (params) {
              let result = key;
              Object.entries(params).forEach(([k, v]) => {
                result = result.replace(new RegExp(`\\{${k}\\}`, 'g'), String(v));
              });
              return result;
            }
            return key;
          }) as unknown as typeof app.config.globalProperties.t;
        },
      }],
      directives: { 'clean-html': {}, 'clean-tooltip': {}, 'stripped-aria-label': {} },
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
  const editable = { advancedEditable: true };

  it('seeds deploy order from the blueprint and emits an override when it changes', async() => {
    const wrapper = await mountStep([component('b', { dependsOn: ['a'] }), component('a')], [], editable);
    const select = wrapper.findComponent({ name: 'LabeledSelectStub' });

    expect(select.props('value')).toEqual(['a']);
    await select.vm.$emit('update:value', []);
    expect(lastModelValue(wrapper)).toEqual([{ componentName: 'b', dependsOn: [] }]);
  });

  it('shows the blueprint overlay read-only above an editable install overlay', async() => {
    const wrapper = await mountStep([component('b', { kustomize: overlay }), component('a')], [], editable);
    const editors = wrapper.findAllComponents(KustomizeOverlayEditor);

    expect(editors.map((e) => e.props('mode'))).toEqual(['view', 'edit']);
    expect(editors[0].props('value')).toEqual(overlay);
    expect(editors[1].props('value')).toBeUndefined();
  });

  it('warns about stale dependencies and emits overrides without them', async() => {
    const wrapper = await mountStep(
      [component('b', { dependsOn: ['a'] }), component('a')],
      [{ componentName: 'b', dependsOn: ['gone', 'a'] }],
      editable,
    );
    const warning = wrapper.findAllComponents(Banner).find((b) => b.props('color') === 'warning');

    expect(warning?.text()).toContain('will be removed when you save: gone');
    expect(JSON.stringify(lastModelValue(wrapper))).not.toContain('gone');
  });

  it('blocks Next when the edited order forms a cycle', async() => {
    const wrapper = await mountStep([component('a'), component('b', { dependsOn: ['a'] })], [], editable);

    await wrapper.findComponent({ name: 'LabeledSelectStub' }).vm.$emit('update:value', ['b']);

    const valid = wrapper.emitted('update:valid') || [];

    expect(valid[valid.length - 1]).toEqual([false]);
    expect(wrapper.findAllComponents(Banner).some((b) => b.props('color') === 'error')).toBe(true);
  });

  it('allows an install overlay on a chart from a git-backed repo', async() => {
    vi.mocked(listGitClusterRepoNames).mockResolvedValueOnce(['r']);
    const wrapper = await mountStep([component('b'), component('a')], [], editable);

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

  it('shows the blueprint valuesFrom read-only before the overlays', async() => {
    const valuesFrom = [{ secretKeyRef: { name: 'runai-cluster-values' } }];
    const wrapper = await mountStep([component('runai-cluster', { valuesFrom })], [], { advancedEditable: true });
    const advanced = wrapper.find('[data-tab="advanced"]');
    const editor = advanced.findComponent(ValuesFromEditor);

    expect(editor.props()).toMatchObject({ value: valuesFrom, name: 'runai-cluster', mode: 'view' });
    const overlayEl = advanced.findComponent(KustomizeOverlayEditor).element;

    expect(editor.element.compareDocumentPosition(overlayEl) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('shows no valuesFrom section for a component without one', async() => {
    const wrapper = await mountStep([component('a')]);

    expect(wrapper.findComponent(ValuesFromEditor).exists()).toBe(false);
  });
});

describe('BlueprintCustomizeStep Advanced tab without permission', () => {
  const readOnlyNote = 'Only users who can update Blueprints';
  const advanced = (wrapper: Awaited<ReturnType<typeof mountStep>>) => wrapper.find('[data-tab="advanced"]');
  const installEditor = (wrapper: Awaited<ReturnType<typeof mountStep>>) => wrapper.findAllComponents(KustomizeOverlayEditor).find((e) => e.props('name') === 'b');

  it('is read-only by default', async() => {
    const wrapper = await mountStep([component('b', { dependsOn: ['a'] }), component('a')]);

    expect(advanced(wrapper).text()).toContain(readOnlyNote);
    expect(wrapper.findComponent({ name: 'LabeledSelectStub' }).props('mode')).toBe('view');
    expect(installEditor(wrapper)).toBeUndefined();
  });

  it('is editable without a note when allowed', async() => {
    const wrapper = await mountStep([component('b', { dependsOn: ['a'] }), component('a')], [], { advancedEditable: true });

    expect(advanced(wrapper).text()).not.toContain(readOnlyNote);
    expect(wrapper.findComponent({ name: 'LabeledSelectStub' }).props('mode')).toBe('edit');
    expect(installEditor(wrapper)!.props('mode')).toBe('edit');
  });

  it('shows the current settings read-only with a note', async() => {
    const wrapper = await mountStep(
      [component('b', { dependsOn: ['a'] }), component('a')],
      [{ componentName: 'b', kustomize: overlay }],
      { advancedEditable: false },
    );
    const notes = wrapper.findAllComponents(Banner).filter((b) => b.text().includes(readOnlyNote));

    expect(notes).toHaveLength(1);
    expect(notes[0].props('color')).toBe('info');
    expect(wrapper.findComponent({ name: 'LabeledSelectStub' }).props()).toMatchObject({ value: ['a'], mode: 'view' });
    expect(installEditor(wrapper)!.props()).toMatchObject({ value: overlay, mode: 'view' });
    expect(advanced(wrapper).findComponent(RadioGroup).exists()).toBe(false);
    expect(wrapper.find('.accordion-header').text()).toContain('Patched');
    expect(wrapper.find('.accordion-header').text()).toContain('Ordered');
  });

  it('shows None instead of an editor when the deployment has no overlay', async() => {
    const wrapper = await mountStep([component('b'), component('a')], [], { advancedEditable: false });

    expect(installEditor(wrapper)).toBeUndefined();
    expect(advanced(wrapper).text()).toContain('None');
  });

  it.each([false, true])('prunes stale dependencies on save (advancedEditable %s)', async(advancedEditable) => {
    const wrapper = await mountStep(
      [component('b'), component('a')],
      [{ componentName: 'b', dependsOn: ['gone', 'a'] }],
      { mode: 'manage', status: {}, advancedEditable },
    );
    const warning = wrapper.findAllComponents(Banner).find((b) => b.props('color') === 'warning');

    expect(warning?.text()).toContain('will be removed when you save: gone');
    expect(lastModelValue(wrapper)).toEqual([{ componentName: 'b', dependsOn: ['a'] }]);

    await wrapper.findComponent({ name: 'ValuesStepStub' }).vm.$emit('update:values', { replicas: 2 });

    expect(lastModelValue(wrapper)).toEqual([{ componentName: 'b', values: { replicas: 2 }, dependsOn: ['a'] }]);
  });

  it('re-sends the existing overlay and deploy order untouched on a values edit in manage mode', async() => {
    const existing: ComponentValueOverride[] = [{ componentName: 'b', kustomize: overlay, dependsOn: [] }];
    const wrapper = await mountStep(
      [component('b', { dependsOn: ['a'] }), component('a')],
      existing,
      { mode: 'manage', status: {}, advancedEditable: false },
    );

    await wrapper.findComponent({ name: 'ValuesStepStub' }).vm.$emit('update:values', { replicas: 2 });

    expect(lastModelValue(wrapper)).toEqual([{
      componentName: 'b', values: { replicas: 2 }, kustomize: overlay, dependsOn: [],
    }]);
  });
});

describe('BlueprintCustomizeStep Values tab', () => {
  it('notes each valuesFrom source, with or without a namespace', async() => {
    const wrapper = await mountStep([component('runai-cluster', {
      valuesFrom: [
        { secretKeyRef: { name: 'runai-cluster-values', namespace: 'runai-backend', key: 'values.yaml' } },
        { configMapKeyRef: { name: 'runai-config' } },
      ],
    })]);
    const notes = wrapper.find('[data-tab="values"]').findAll('p.text-muted').map((p) => p.text());

    expect(notes).toEqual([
      'Some values are read at deploy time from Secret runai-backend/runai-cluster-values.',
      'Some values are read at deploy time from ConfigMap runai-config (release namespace).',
    ]);
  });

  it('shows no valuesFrom note for a component without one', async() => {
    const wrapper = await mountStep([component('a')]);

    expect(wrapper.text()).not.toContain('read at deploy time');
  });
});

describe('BlueprintCustomizeStep manage mode', () => {
  it('shows locked values banner with pins in manage mode', async() => {
    const wrapper = await mountStep(
      [component('runai', { values: { global: { domain: 'aif:auto-domain:runai' } } })],
      [],
      {
        mode: 'manage',
        status: {
          resolvedValues: [
            { componentName: 'runai', path: 'global.domain', value: 'runai.10.0.0.12.sslip.io', source: 'auto' },
            { componentName: 'runai', path: 'global.ingressClass', value: 'nginx', source: 'auto' },
          ],
        },
      },
    );

    const banner = wrapper.findAllComponents(Banner).find((b) => b.props('color') === 'info');
    expect(banner?.exists()).toBe(true);
    expect(banner?.text()).toContain('These values were set at install and are locked');
    expect(banner?.text()).toContain('runai');
    expect(banner?.text()).toContain('global.domain = runai.10.0.0.12.sslip.io');
    expect(banner?.text()).toContain('global.ingressClass = nginx');
    expect(wrapper.find('[data-tab="values"] .values-stub').exists()).toBe(true);
  });

  it('shows no locked values banner when no pins', async() => {
    const wrapper = await mountStep(
      [component('runai')],
      [],
      { mode: 'manage', status: {} },
    );

    expect(wrapper.text()).not.toContain('These values were set at install and are locked');
  });

  it('shows no locked values banner in install mode', async() => {
    const wrapper = await mountStep(
      [component('runai')],
      [],
      {
        mode: 'install',
        status: {
          resolvedValues: [
            { componentName: 'runai', path: 'global.domain', value: 'runai.10.0.0.12.sslip.io', source: 'auto' },
          ],
        },
      },
    );

    expect(wrapper.text()).not.toContain('These values were set at install and are locked');
  });

  it('shows automatic value hints in install mode', async() => {
    const wrapper = await mountStep(
      [component('runai', { values: { global: { domain: 'aif:auto-domain:runai', ingressClass: 'aif:auto-ingress-class' } } })],
      [],
      { mode: 'install' },
    );

    const text = wrapper.find('[data-tab="values"]').text();
    expect(text).toContain('Automatic: runai.<node-ip>.sslip.io');
    expect(text).toContain('set global.domain to use your own domain');
    expect(text).toContain('set global.ingressClass to choose one');
  });
});

describe('BlueprintCustomizeStep existing releases', () => {
  const existingReleases = {
    'cert-manager': [
      {
        clusterId: 'c-1', release: 'cert-manager', chart: 'cert-manager', version: 'v1.15.0', namespace: 'cert-manager',
      },
      {
        clusterId: 'c-2', release: 'certs', chart: 'cert-manager', version: '', namespace: 'security',
      },
    ],
  };
  const props = { existingReleases, clusterNames: { 'c-1': 'prod' } };
  const warning = (wrapper: Awaited<ReturnType<typeof mountStep>>) => wrapper.findAllComponents(Banner).find((b) => b.text().includes('Already installed'));

  it('warns on a matching component in install mode, listing each cluster', async() => {
    const wrapper = await mountStep([component('cert-manager'), component('gpu-operator')], [], props);
    const banners = wrapper.findAllComponents(Banner).filter((b) => b.text().includes('Already installed'));

    expect(banners).toHaveLength(1);
    expect(banners[0].props('color')).toBe('warning');
    expect(banners[0].text()).toContain('Already installed on prod, c-2. Consider excluding this component.');
    expect(banners[0].findAll('li').map((li) => li.text())).toEqual([
      'prod: cert-manager (cert-manager v1.15.0) in namespace cert-manager',
      'c-2: certs (cert-manager) in namespace security',
    ]);
    expect(lastModelValue(wrapper)).toEqual([]);
  });

  it('hides the warning once the component is excluded', async() => {
    const wrapper = await mountStep([component('cert-manager'), component('gpu-operator')], [{ componentName: 'cert-manager', enabled: false }], props);

    expect(warning(wrapper)).toBeUndefined();
  });

  it('shows no warning in manage mode', async() => {
    const wrapper = await mountStep([component('cert-manager')], [], { ...props, mode: 'manage', status: {} });

    expect(warning(wrapper)).toBeUndefined();
  });
});
