// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import type { VueWrapper } from '@vue/test-utils';
import type { App } from 'vue';
import { Banner } from '@components/Banner';
import BlueprintCreateWizard from '../BlueprintCreateWizard.vue';
import { createBlueprint } from '../../../utils/blueprint-api';
import type { BlueprintComponent, BlueprintSpec } from '../../../types/blueprint-types';

vi.mock('../../../utils/blueprint-api', async (original) => ({
  ...await original<typeof import('../../../utils/blueprint-api')>(), createBlueprint: vi.fn(),
}));
vi.mock('../wizard/BlueprintBasicInfoStep.vue', () => ({ default: { name: 'BasicInfoStub', props: ['form', 'nameDisabled'], template: '<div />' } }));
// The real application selector, so removing an application goes through its own cleanup.
vi.mock('@shell/components/form/LabeledSelect', () => ({ default: { template: '<div />' } }));
vi.mock('../../../services/app-collection', () => ({
  fetchSuseAiApps:          vi.fn(async() => ({ apps: [] })),
  fetchNvidiaApps:          vi.fn(async() => ({ apps: [] })),
  fetchCustomRepoApps:      vi.fn(async() => ({ apps: [] })),
  fetchManagedRepos:        vi.fn(async() => []),
  fetchSettingsOrNull:      vi.fn(async() => null),
  resolveInstallRepoName:   vi.fn(),
  getLibraryForClusterRepo: vi.fn(),
}));
vi.mock('../../../services/rancher-apps', () => ({
  listChartVersions:        vi.fn(async() => []),
  inferClusterRepoForChart: vi.fn(),
}));
// __esModule: defineAsyncComponent unwraps `default` only from ES module namespaces.
vi.mock('../wizard/BlueprintConfigStep.vue', () => ({
  __esModule: true,
  default:    {
    name: 'ConfigStub', props: ['components'], emits: ['update:components'], template: '<div />',
  },
}));
vi.mock('../wizard/BlueprintReviewCreateStep.vue', () => ({ __esModule: true, default: { name: 'ReviewStub', props: ['form', 'isEdit'], template: '<div />' } }));

const prefill = (rollback?: BlueprintSpec['rollback']): BlueprintSpec => ({
  displayName: 'Run:ai', version: '2.0.1', rollback, components: [],
});

function mountWizard(spec?: BlueprintSpec) {
  return mount(BlueprintCreateWizard, {
    props:  { prefill: spec },
    global: {
      plugins: [{
        install: (app: App) => {
          // Read during setup, so set as globalProperties rather than VTU mocks.
          Object.assign(app.config.globalProperties, {
            $route: { params: {} }, $router: { push: vi.fn() }, $store: { dispatch: vi.fn(async() => ({})), getters: {} },
          });
        },
      }],
    },
  });
}

function basicInfoForm(spec?: BlueprintSpec) {
  return mountWizard(spec).findComponent({ name: 'BasicInfoStub' }).props('form');
}

describe('BlueprintCreateWizard basic info', () => {
  it.each([
    [undefined, true],
    [prefill(), true],
    [prefill('Allowed'), true],
    [prefill('Disabled'), false],
  ])('prefills allowRollback from %j', (spec, allowRollback) => {
    expect(basicInfoForm(spec).allowRollback).toBe(allowRollback);
  });
});

const component = (chartName: string, extra: Partial<BlueprintComponent> = {}): BlueprintComponent => ({
  chartRepo: 'r', chartName, chartVersion: '1.0.0', ...extra,
});

describe('BlueprintCreateWizard configuration validity', () => {
  const button = (wrapper: VueWrapper, label: string) => wrapper.findAll('button').find((b) => b.text() === label)!;
  const activeStep = (wrapper: VueWrapper) => wrapper.find('.step-item.active .step-label').text();

  async function click(wrapper: VueWrapper, label: string) {
    await button(wrapper, label).trigger('click');
    await flushPromises();
  }

  async function configStep() {
    const wrapper = mountWizard({ displayName: 'Run:ai', version: '2.0.1', components: [component('a'), component('b')] });

    await click(wrapper, 'Next');
    await click(wrapper, 'Next');
    expect(activeStep(wrapper)).toBe('Configuration');

    return wrapper;
  }

  it.each([
    ['a dependsOn cycle', [component('a', { dependsOn: ['b'] }), component('b', { dependsOn: ['a'] })]],
    ['an invalid valuesFrom source', [component('a', { valuesFrom: [{ secretKeyRef: { name: 'Not_Valid' } }] }), component('b')]],
  ])('returns to Configuration after Previous with %s, but blocks Review', async(_, invalid) => {
    const wrapper = await configStep();

    wrapper.findComponent({ name: 'ConfigStub' }).vm.$emit('update:components', invalid);
    await flushPromises();
    expect(button(wrapper, 'Next').attributes('disabled')).toBeDefined();

    await click(wrapper, 'Previous');
    expect(activeStep(wrapper)).toBe('Select Applications');
    await click(wrapper, 'Next');
    expect(activeStep(wrapper)).toBe('Configuration');

    expect(button(wrapper, 'Next').attributes('disabled')).toBeDefined();
    await wrapper.findAll('.step-item')[3].trigger('click');
    expect(activeStep(wrapper)).toBe('Configuration');

    wrapper.findComponent({ name: 'ConfigStub' }).vm.$emit('update:components', [component('a'), component('b')]);
    await flushPromises();
    await click(wrapper, 'Next');
    expect(activeStep(wrapper)).toBe('Review');
    expect(button(wrapper, 'Create').attributes('disabled')).toBeUndefined();
  });

  it('reaches Review after removing an application another one deploys after', async() => {
    const wrapper = mountWizard({ displayName: 'Run:ai', version: '2.0.1', components: [component('a'), component('b', { dependsOn: ['a'] })] });

    await click(wrapper, 'Next');
    expect(activeStep(wrapper)).toBe('Select Applications');
    await wrapper.findAll('.btn-remove')[0].trigger('click');
    await click(wrapper, 'Next');
    await click(wrapper, 'Next');

    expect(activeStep(wrapper)).toBe('Review');
    expect(wrapper.findComponent({ name: 'ReviewStub' }).props('form').components).toEqual([component('b')]);
    expect(button(wrapper, 'Create').attributes('disabled')).toBeUndefined();
  });

  // Create is disabled while invalid; onCreate re-checks anyway, so call it directly.
  it('refuses to create with an invalid valuesFrom source', async() => {
    const wrapper = await configStep();

    (wrapper.vm as any).components = [component('a', { valuesFrom: [{ secretKeyRef: { name: 'ok', namespace: 'Bad_NS' } }] })];
    await (wrapper.vm as any).onCreate();
    await flushPromises();

    expect(createBlueprint).not.toHaveBeenCalled();
    expect(wrapper.findComponent(Banner).text()).toContain('a: the namespace of values source 1');
  });
});
