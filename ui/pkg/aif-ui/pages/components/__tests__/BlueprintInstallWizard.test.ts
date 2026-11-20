// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import type { ComponentCustomProperties } from 'vue';
import type { Router, RouteLocationNormalizedLoaded } from 'vue-router';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import BlueprintInstallWizard from '../BlueprintInstallWizard.vue';
import BlueprintInstallBasicInfoStep from '../wizard/BlueprintInstallBasicInfoStep.vue';
import BlueprintCustomizeStep from '../wizard/BlueprintCustomizeStep.vue';
import TargetStep from '../wizard/TargetStep.vue';
import { getBlueprint } from '../../../utils/blueprint-api';
import { listAIWorkloads, getRegistryStatus, getCapabilities } from '../../../utils/operator-api';
import { fetchUserNamespaces } from '../../../services/rancher-apps';
import { listCatalogApps } from '../../../services/cluster-service';

vi.mock('../../../utils/blueprint-api', async (original) => ({
  ...await original<typeof import('../../../utils/blueprint-api')>(), getBlueprint: vi.fn(),
}));
vi.mock('../../../utils/operator-api', () => ({
  listAIWorkloads:        vi.fn(),
  getRegistryStatus:      vi.fn().mockResolvedValue(null),
  getCapabilities:        vi.fn(),
  getSettings:            vi.fn().mockResolvedValue({}),
  createAIWorkload:       vi.fn(),
  updateAIWorkload:       vi.fn(),
}));
vi.mock('../../../services/rancher-apps', async (original) => ({
  ...await original<typeof import('../../../services/rancher-apps')>(),
  fetchUserNamespaces: vi.fn(),
  getAllClusters:      vi.fn().mockResolvedValue([]),
}));

vi.mock('../../../services/cluster-service', () => ({ listCatalogApps: vi.fn().mockResolvedValue([]) }));

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../../l10n/en-us.yaml'), 'utf8'));
const t = (key: string) => key.split('.').reduce<any>((value, part) => value?.[part], translations);
const RANDOM_NAMESPACE = /^my-blueprint-[a-z0-9]{7}$/;
const mounted: ReturnType<typeof mount>[] = [];

async function setup(mode: 'install' | 'manage' = 'install', query: Record<string, string> = {}) {
  const store = { getters: { 'i18n/t': t }, dispatch: vi.fn(), commit: vi.fn() };
  const router = { push: vi.fn() } as unknown as Router;
  const route = { params: { cluster: 'local' }, query } as unknown as RouteLocationNormalizedLoaded;
  const wrapper = mount(BlueprintInstallWizard, {
    props:  { blueprintName: 'My Blueprint', blueprintVersion: '1.0.0', mode },
    global: {
      // Cast like the route/router: the shell augments these with t, $t and $fetchState, which the wizards don't use.
      config: { globalProperties: { $store: store, $route: route, $router: router } as unknown as ComponentCustomProperties },
      directives: { 'clean-html': {} },
      stubs:      {
        TargetStep: true, BlueprintCustomizeStep: true, RouterLink: true, Loading: true, Banner: { template: '<div><slot /></div>' }, t: true,
      },
    },
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

const namespaceInput = (wrapper: ReturnType<typeof mount>) => (wrapper.get('.ns-input').element as HTMLInputElement).value;
const button = (wrapper: ReturnType<typeof mount>, label: string) => wrapper.findAll('button').find((b) => b.text() === label)!;

beforeEach(() => {
  vi.mocked(getBlueprint).mockReset().mockResolvedValue({ spec: { displayName: 'My Blueprint', components: [] } } as any);
  vi.mocked(listAIWorkloads).mockReset().mockResolvedValue({ items: [] } as any);
  vi.mocked(listCatalogApps).mockReset().mockResolvedValue([]);
  vi.mocked(getCapabilities).mockReset().mockResolvedValue({ advancedCustomization: true });
  vi.mocked(fetchUserNamespaces).mockReset().mockImplementation(async (_store, suggested) => (
    [suggested, 'team-a'].filter(Boolean).map((ns) => ({ label: ns, value: ns }))
  ));
});
afterEach(() => mounted.splice(0).forEach((wrapper) => wrapper.unmount()));

describe('blueprint install wizard namespace', () => {
  it('suggests one random namespace and keeps it when the step re-mounts', async () => {
    const wrapper = await setup();
    const suggested = namespaceInput(wrapper);
    expect(suggested).toMatch(RANDOM_NAMESPACE);
    expect(fetchUserNamespaces).toHaveBeenLastCalledWith(expect.anything(), suggested);

    // Leaving and returning to the first step re-mounts it.
    await button(wrapper, 'Next').trigger('click');
    expect(wrapper.find('.ns-input').exists()).toBe(false);
    await button(wrapper, 'Previous').trigger('click');
    await flushPromises();

    expect(namespaceInput(wrapper)).toBe(suggested);
    expect(fetchUserNamespaces).toHaveBeenCalledTimes(2);
    expect(fetchUserNamespaces).toHaveBeenLastCalledWith(expect.anything(), suggested);
  });

  it('suggests a different namespace for each install', async () => {
    const first = namespaceInput(await setup());
    const second = namespaceInput(await setup());
    expect(second).toMatch(RANDOM_NAMESPACE);
    expect(second).not.toBe(first);
  });

  it('keeps the namespace of the managed deployment', async () => {
    vi.mocked(listAIWorkloads).mockResolvedValue({ items: [{
      metadata: { name: 'my-blueprint', namespace: 'my-blueprint-system' },
      spec:     { targetClusters: ['c-1'], componentValues: [] },
    }] } as any);
    const wrapper = await setup('manage', { instanceName: 'my-blueprint', instanceNamespace: 'my-blueprint-system' });
    expect(namespaceInput(wrapper)).toBe('my-blueprint-system');
    expect(fetchUserNamespaces).toHaveBeenCalledWith(expect.anything(), 'my-blueprint-system');
  });

  it('does not invent a namespace when the managed deployment is missing', async () => {
    const wrapper = await setup('manage', { instanceName: 'gone', instanceNamespace: 'gone-ns' });
    expect(wrapper.text()).toContain('Deployment "gone" not found in namespace "gone-ns".');
    expect(namespaceInput(wrapper)).toBe('');
    expect(fetchUserNamespaces).toHaveBeenCalledWith(expect.anything(), '');
    expect(button(wrapper, 'Next').attributes('disabled')).toBeDefined();
  });
});

describe('blueprint install wizard credential preflight', () => {
  const components = [
    { chartName: 'ac-app', chartRepo: 'application-collection' },
    { chartName: 'nim', chartRepo: 'nvidia' },
  ];

  afterEach(() => vi.mocked(getRegistryStatus).mockReset().mockResolvedValue(null as any));

  async function reviewStep() {
    vi.mocked(getBlueprint).mockResolvedValue({ spec: { displayName: 'My Blueprint', components } } as any);
    const wrapper = await setup();
    // A selected cluster isolates the credential gate on Install.
    (wrapper.vm as any).clusters = ['c-1'];
    (wrapper.vm as any).currentStep = 3;
    await flushPromises();
    return wrapper;
  }

  it('flags credentials the registry status reports as unconfigured', async () => {
    vi.mocked(getRegistryStatus).mockResolvedValue({ applicationCollection: true, suseRegistry: false, nvidia: false });
    const wrapper = await reviewStep();
    expect(getRegistryStatus).toHaveBeenCalled();
    expect(wrapper.text()).toContain("This blueprint needs credentials that aren't configured: NVIDIA.");
    expect(button(wrapper, 'Install').attributes('disabled')).toBeDefined();
  });

  it('enables Install for an anonymous NVIDIA mirror without credentials', async () => {
    vi.mocked(getRegistryStatus).mockResolvedValue({
      applicationCollection: true, suseRegistry: false, nvidia: false,
      usable:                { applicationCollection: true, suseRegistry: false, nvidia: true },
    });
    const wrapper = await reviewStep();
    expect(wrapper.text()).not.toContain('needs credentials');
    expect(button(wrapper, 'Install').attributes('disabled')).toBeUndefined();
  });

  it('shows no banner when the registry status is unavailable', async () => {
    vi.mocked(getRegistryStatus).mockRejectedValue(new Error('operator unreachable'));
    const wrapper = await reviewStep();
    expect(wrapper.text()).not.toContain('needs credentials');
  });
});

describe('blueprint install wizard rollback', () => {
  it.each(['install', 'manage'] as const)('passes the Blueprint rollback policy to the steps in %s mode', async (mode) => {
    vi.mocked(getBlueprint).mockResolvedValue({ spec: { displayName: 'My Blueprint', rollback: 'Disabled', components: [] } } as any);
    vi.mocked(listAIWorkloads).mockResolvedValue({ items: [{
      metadata: { name: 'my-blueprint', namespace: 'my-blueprint-system' },
      spec:     { targetClusters: ['c-1'], componentValues: [] },
    }] } as any);
    const wrapper = await setup(mode, { instanceName: 'my-blueprint', instanceNamespace: 'my-blueprint-system' });

    expect(wrapper.findComponent(BlueprintInstallBasicInfoStep).props('rollback')).toBe('Disabled');
  });
});

describe('blueprint install wizard existing releases', () => {
  const certManager = (version: string) => ({
    metadata: { name: 'cert-manager', namespace: 'cert-manager' },
    spec:     { chart: { metadata: { name: 'cert-manager', version } } },
  });
  const components = [{ chartRepo: 'application-collection', chartName: 'cert-manager', chartVersion: '1.0.0' }];

  async function toCustomize(wrapper: ReturnType<typeof mount>, clusterIds: string[]) {
    await button(wrapper, 'Next').trigger('click');
    wrapper.findComponent(TargetStep).vm.$emit('update:clusters', clusterIds);
    await flushPromises();
    await button(wrapper, 'Next').trigger('click');
    return wrapper.findComponent(BlueprintCustomizeStep);
  }

  const certManagerRelease = { clusterId: 'c-1', release: 'cert-manager', chart: 'cert-manager', version: 'v1.15.0', namespace: 'cert-manager' };

  async function selectClusters(wrapper: ReturnType<typeof mount>, clusterIds: string[]) {
    wrapper.findComponent(TargetStep).vm.$emit('update:clusters', clusterIds);
    await flushPromises();
  }

  async function releasesOnCustomize(wrapper: ReturnType<typeof mount>) {
    await button(wrapper, 'Next').trigger('click');
    const releases = wrapper.findComponent(BlueprintCustomizeStep).props('existingReleases');
    await button(wrapper, 'Previous').trigger('click');
    return releases;
  }

  beforeEach(() => {
    vi.mocked(getBlueprint).mockResolvedValue({ spec: { displayName: 'My Blueprint', components } } as any);
  });

  it('lists the target clusters\' releases and passes matches to the customize step', async () => {
    vi.mocked(listCatalogApps).mockImplementation(async (_store, id) => (id === 'c-1' ? [certManager('v1.15.0')] : []) as any);
    const step = await toCustomize(await setup(), ['c-1', 'c-2']);

    expect(listCatalogApps).toHaveBeenCalledTimes(2);
    expect(step.props('existingReleases')).toEqual({ 'cert-manager': [certManagerRelease] });
  });

  it('drops a deselected cluster at once and does not refetch it when reselected', async () => {
    vi.mocked(listCatalogApps).mockResolvedValue([certManager('v1.15.0')] as any);
    const wrapper = await setup();
    await button(wrapper, 'Next').trigger('click');

    await selectClusters(wrapper, ['c-1']);
    expect(await releasesOnCustomize(wrapper)).toEqual({ 'cert-manager': [certManagerRelease] });
    await selectClusters(wrapper, []);
    expect(await releasesOnCustomize(wrapper)).toEqual({});
    await selectClusters(wrapper, ['c-1']);
    expect(await releasesOnCustomize(wrapper)).toEqual({ 'cert-manager': [certManagerRelease] });
    expect(listCatalogApps).toHaveBeenCalledTimes(1);
  });

  it('ignores a superseded in-flight response', async () => {
    let resolveStale = (): void => undefined;
    vi.mocked(listCatalogApps)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveStale = () => resolve([certManager('v1.15.0')] as any); }))
      .mockResolvedValue([]);
    const wrapper = await setup();
    await button(wrapper, 'Next').trigger('click');
    await selectClusters(wrapper, ['c-1']);
    await selectClusters(wrapper, ['c-1', 'c-2']);
    resolveStale();
    await flushPromises();

    expect(await releasesOnCustomize(wrapper)).toEqual({});
  });

  it('shows no releases when listing a cluster fails', async () => {
    vi.mocked(listCatalogApps).mockRejectedValue(new Error('forbidden'));
    const step = await toCustomize(await setup(), ['c-1']);

    expect(step.props('existingReleases')).toEqual({});
  });

  it('does not look for existing releases in manage mode', async () => {
    vi.mocked(getBlueprint).mockResolvedValue({ spec: { displayName: 'My Blueprint', components } } as any);
    vi.mocked(listAIWorkloads).mockResolvedValue({ items: [{
      metadata: { name: 'my-blueprint', namespace: 'my-blueprint-system' },
      spec:     { targetClusters: ['c-1'], componentValues: [] },
    }] } as any);
    await setup('manage', { instanceName: 'my-blueprint', instanceNamespace: 'my-blueprint-system' });

    expect(listCatalogApps).not.toHaveBeenCalled();
  });
});

describe('blueprint install wizard advanced customization capability', () => {
  const workload = {
    metadata: { name: 'my-blueprint', namespace: 'my-blueprint-system' },
    spec:     { targetClusters: ['c-1'], componentValues: [] },
  };

  async function customizeStepIn(mode: 'install' | 'manage') {
    vi.mocked(listAIWorkloads).mockResolvedValue({ items: [workload] } as any);
    const wrapper = await setup(mode, { instanceName: 'my-blueprint', instanceNamespace: 'my-blueprint-system' });
    (wrapper.vm as any).currentStep = 2;
    await flushPromises();
    return { wrapper, step: wrapper.findComponent(BlueprintCustomizeStep) };
  }

  describe.each(['install', 'manage'] as const)('in %s mode', (mode) => {
    it('lets the customize step edit Advanced settings when allowed', async () => {
      const { step } = await customizeStepIn(mode);
      expect(step.props('advancedEditable')).toBe(true);
    });

    it.each([
      ['not allowed', () => vi.mocked(getCapabilities).mockResolvedValue({ advancedCustomization: false })],
      ['a malformed body', () => vi.mocked(getCapabilities).mockResolvedValue({} as any)],
      ['forbidden', () => vi.mocked(getCapabilities).mockRejectedValue(Object.assign(new Error('Forbidden'), { status: 403 }))],
      ['an older operator (404)', () => vi.mocked(getCapabilities).mockRejectedValue(Object.assign(new Error('Not Found'), { status: 404 }))],
      ['the operator unreachable', () => vi.mocked(getCapabilities).mockRejectedValue(new Error('network'))],
    ])('makes Advanced settings read-only when %s', async (_name, arrange) => {
      arrange();
      const { step } = await customizeStepIn(mode);
      expect(step.props('advancedEditable')).toBe(false);
    });

    it('fetches capabilities once, not per step change', async () => {
      const { wrapper } = await customizeStepIn(mode);
      await button(wrapper, 'Previous').trigger('click');
      await button(wrapper, 'Next').trigger('click');
      expect(getCapabilities).toHaveBeenCalledTimes(1);
    });
  });

  it('keeps Advanced settings read-only while capabilities are loading', async () => {
    vi.mocked(getCapabilities).mockReturnValue(new Promise(() => undefined));
    const { step } = await customizeStepIn('install');
    expect(step.props('advancedEditable')).toBe(false);
  });
});
