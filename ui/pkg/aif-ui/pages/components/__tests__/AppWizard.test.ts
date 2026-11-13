// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import type { ComponentCustomProperties } from 'vue';
import type { Router, RouteLocationNormalizedLoaded } from 'vue-router';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import AppWizard from '../AppWizard.vue';
import {
  createOrUpgradeApp, ensurePullSecretOnAllSAs, ensureRegistrySecretSimple, ensureServiceAccountPullSecret, fetchUserNamespaces, findChartInRepo, listChartVersions, listClusterRepos,
} from '../../../services/rancher-apps';
import { isManagedRepoName } from '../../../services/app-collection';
import { getRegistryStatus } from '../../../utils/operator-api';
import { createFleetBundle } from '../../../services/fleet-bundle';
import { publishToFleetGit } from '../../../services/git-publish';

vi.mock('../../../services/rancher-apps', async (original) => ({
  ...await original<typeof import('../../../services/rancher-apps')>(),
  fetchUserNamespaces:       vi.fn(),
  getAllClusters:            vi.fn().mockResolvedValue([]),
  inferClusterRepoForChart:  vi.fn().mockResolvedValue(null),
  getInstalledHelmDetails:   vi.fn().mockResolvedValue({}),
  findChartInRepo:           vi.fn().mockResolvedValue(null),
  listChartVersions:         vi.fn().mockResolvedValue([]),
  listClusterRepos:          vi.fn().mockResolvedValue([]),
  ensureNamespace:           vi.fn().mockResolvedValue(undefined),
  ensureRegistrySecretSimple:     vi.fn().mockResolvedValue('suse-ai-pull-secret-dp-apps-rancher-io'),
  ensureServiceAccountPullSecret: vi.fn().mockResolvedValue(undefined),
  ensurePullSecretOnAllSAs:       vi.fn().mockResolvedValue(undefined),
  createOrUpgradeApp:        vi.fn().mockResolvedValue({ upgraded: false }),
  waitForAppInstall:         vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../../../services/fleet-bundle', async (original) => ({
  ...await original<typeof import('../../../services/fleet-bundle')>(),
  createFleetBundle:              vi.fn().mockResolvedValue('bundle'),
  ensureAppCollectionPullSecrets: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../../../services/git-publish', () => ({ publishToFleetGit: vi.fn().mockResolvedValue('commit') }));
vi.mock('../../../services/app-collection', async (original) => ({
  ...await original<typeof import('../../../services/app-collection')>(),
  fetchSuseAiApps:   vi.fn().mockResolvedValue({ apps: [] }),
  isManagedRepoName: vi.fn().mockResolvedValue(false),
}));
vi.mock('../../../services/ui-persist', () => ({
  persistLoad: (_key: string, fallback: unknown) => fallback, persistSave: vi.fn(), persistClear: vi.fn(),
}));
vi.mock('../../../utils/operator-api', () => ({
  listAIWorkloads:        vi.fn().mockResolvedValue({ items: [] }),
  getRegistryStatus:      vi.fn().mockResolvedValue({ applicationCollection: false, suseRegistry: false, nvidia: false }),
  getSettings:            vi.fn().mockResolvedValue({}),
  createAIWorkload:       vi.fn(),
  updateAIWorkload:       vi.fn(),
}));

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../../l10n/en-us.yaml'), 'utf8'));
const t = (key: string) => key.split('.').reduce<any>((value, part) => value?.[part], translations);
const RANDOM_NAMESPACE = /^coredns-[a-z0-9]{7}$/;
const mounted: ReturnType<typeof mount>[] = [];

async function setup(mode: 'install' | 'manage' = 'install', query: Record<string, string> = {}) {
  const store = { getters: { 'i18n/t': t }, dispatch: vi.fn(), commit: vi.fn() };
  const router = { push: vi.fn() } as unknown as Router;
  const route = { params: { cluster: 'local' }, query } as unknown as RouteLocationNormalizedLoaded;
  const wrapper = mount(AppWizard, {
    props:  { slug: 'coredns', mode },
    global: {
      // Cast like the route/router: the shell augments these with t, $t and $fetchState, which the wizards don't use.
      config: { globalProperties: { $store: store, $route: route, $router: router } as unknown as ComponentCustomProperties },
      stubs:  {
        Loading:       true,
        Banner:        { template: '<div><slot /></div>' },
        LabeledSelect: true,
      },
    },
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

const namespaceInput = (wrapper: ReturnType<typeof mount>) => (wrapper.get('.ns-input').element as HTMLInputElement).value;

beforeEach(() => {
  vi.mocked(fetchUserNamespaces).mockReset().mockImplementation(async (_store, suggested) => (
    [suggested, 'team-a'].filter(Boolean).map((ns) => ({ label: ns, value: ns }))
  ));
});
afterEach(() => mounted.splice(0).forEach((wrapper) => wrapper.unmount()));

describe('app install wizard namespace', () => {
  it('suggests one random namespace and keeps it while the form re-renders', async () => {
    const wrapper = await setup();
    const suggested = namespaceInput(wrapper);
    expect(suggested).toMatch(RANDOM_NAMESPACE);
    expect(fetchUserNamespaces).toHaveBeenCalledTimes(1);
    expect(fetchUserNamespaces).toHaveBeenCalledWith(expect.anything(), suggested);

    await wrapper.get('.labeled-input input').setValue('coredns-2');
    await flushPromises();

    expect((wrapper.get('.labeled-input input').element as HTMLInputElement).value).toBe('coredns-2');
    expect(namespaceInput(wrapper)).toBe(suggested);
  });

  it('suggests a different namespace for each install', async () => {
    const first = namespaceInput(await setup());
    const second = namespaceInput(await setup());
    expect(second).toMatch(RANDOM_NAMESPACE);
    expect(second).not.toBe(first);
  });

  it('keeps the namespace of the managed deployment', async () => {
    const wrapper = await setup('manage', { instanceName: 'coredns', instanceNamespace: 'coredns-system', instanceCluster: 'local' });
    expect(namespaceInput(wrapper)).toBe('coredns-system');
    expect(fetchUserNamespaces).toHaveBeenCalledWith(expect.anything(), 'coredns-system');
  });
});

describe('app install wizard version from the detail panel', () => {
  const selectedVersion = (wrapper: ReturnType<typeof mount>) => wrapper.findComponent({ name: 'BasicInfoStep' }).props('form').chartVersion;

  beforeEach(() => {
    vi.mocked(isManagedRepoName).mockResolvedValue(true);
    vi.mocked(findChartInRepo).mockResolvedValue({ chartName: 'coredns', version: '2.0.0' });
    vi.mocked(listChartVersions).mockResolvedValue(['2.0.0', '1.5.0', '1.0.0']);
  });
  afterEach(() => vi.mocked(isManagedRepoName).mockResolvedValue(false));

  it('preselects the version passed in the URL', async () => {
    const wrapper = await setup('install', { repo: 'repo-a', version: '1.5.0' });

    expect(selectedVersion(wrapper)).toBe('1.5.0');
  });

  it('keeps the latest version when the URL version is not offered by the repository', async () => {
    const wrapper = await setup('install', { repo: 'repo-a', version: '9.9.9' });

    expect(selectedVersion(wrapper)).toBe('2.0.0');
  });

  it('keeps the latest version when no version is passed', async () => {
    const wrapper = await setup('install', { repo: 'repo-a' });

    expect(selectedVersion(wrapper)).toBe('2.0.0');
  });
});

describe('app wizard image pull secrets', () => {
  const ALL_CONFIGURED = { applicationCollection: true, suseRegistry: true, nvidia: true };
  const COMBINED = 'suse-ai-pull-combined';
  const LEGACY = 'suse-ai-pull-secret-dp-apps-rancher-io';

  beforeEach(() => {
    vi.mocked(listClusterRepos).mockResolvedValue([
      { metadata: { name: 'application-collection' }, spec: { url: 'oci://dp.apps.rancher.io/charts' } },
    ] as any);
    vi.mocked(getRegistryStatus).mockReset().mockResolvedValue(ALL_CONFIGURED);
    vi.mocked(ensureRegistrySecretSimple).mockClear();
    vi.mocked(createFleetBundle).mockClear();
    vi.mocked(publishToFleetGit).mockClear();
    vi.mocked(createOrUpgradeApp).mockClear();
    vi.mocked(ensureServiceAccountPullSecret).mockClear();
    vi.mocked(ensurePullSecretOnAllSAs).mockClear();
  });
  afterEach(() => vi.mocked(listClusterRepos).mockResolvedValue([]));

  async function wizardWith(deployType: 'Helm' | 'FleetBundle' | 'GitOps', mode: 'install' | 'manage' = 'install', values: Record<string, any> = {}) {
    const query: Record<string, string> = mode === 'manage' ? { instanceName: 'coredns', instanceNamespace: 'coredns-system', instanceCluster: 'local' } : {};
    const wrapper = await setup(mode, query);
    const vm = wrapper.vm as any;
    Object.assign(vm.form, {
      chartRepo: 'application-collection', chartName: 'coredns', chartVersion: '1.0.0', clusters: ['local'], values,
    });
    await flushPromises();
    vm.form.deployType = deployType;
    return vm;
  }

  it('references the operator secret for a FleetBundle install without creating secrets', async () => {
    const vm = await wizardWith('FleetBundle');
    await vm.submit();

    expect(ensureRegistrySecretSimple).not.toHaveBeenCalled();
    expect(createFleetBundle).toHaveBeenCalledWith(expect.anything(), expect.objectContaining({
      additionalPullSecretNames: [COMBINED], library: 'suse-ai',
    }));
  });

  it('puts the operator secret in the values of a local Helm install', async () => {
    const vm = await wizardWith('Helm');
    await vm.submit();
    await flushPromises();

    expect(ensureRegistrySecretSimple).not.toHaveBeenCalled();
    expect(createOrUpgradeApp).toHaveBeenCalled();
    const values = vi.mocked(createOrUpgradeApp).mock.calls[0][5] as Record<string, any>;
    expect(values.imagePullSecrets).toEqual([{ name: COMBINED }]);
    expect(values.global.imagePullSecrets).toEqual([{ name: COMBINED }]);
  });

  it('attaches ngc-secret to service accounts for an NVIDIA chart on the Helm path, without the generic pull-secret list', async () => {
    vi.mocked(listClusterRepos).mockResolvedValue([
      { metadata: { name: 'nvidia' }, spec: { url: 'https://helm.ngc.nvidia.com/nim' } },
    ] as any);
    vi.mocked(getRegistryStatus).mockResolvedValue({ applicationCollection: false, suseRegistry: false, nvidia: true });
    const vm = await wizardWith('Helm');
    vm.form.chartRepo = 'nvidia';
    await vm.submit();
    await flushPromises();

    expect(ensureRegistrySecretSimple).not.toHaveBeenCalled();
    const values = vi.mocked(createOrUpgradeApp).mock.calls[0][5] as Record<string, any>;
    // Only the NVIDIA-specific refs (injectNvidiaPullSecretRefs) reach the values;
    // the generic suse-ai list (incl. global.imagePullSecrets) is not applied.
    expect(values.imagePullSecrets).toEqual([{ name: 'ngc-secret' }]);
    expect(values.global.imagePullSecrets).toBeUndefined();
    expect(values.global.ngcImagePullSecretName).toBe('ngc-secret');
    expect(JSON.stringify(values)).not.toContain(COMBINED);
    const attached = vi.mocked(ensureServiceAccountPullSecret).mock.calls.map(c => c[4]);
    expect(attached.length).toBeGreaterThan(0);
    expect(new Set(attached)).toEqual(new Set(['ngc-secret']));
    expect(ensurePullSecretOnAllSAs).toHaveBeenCalledWith(expect.anything(), 'local', expect.any(String), 'ngc-secret');
    expect(ensurePullSecretOnAllSAs).not.toHaveBeenCalledWith(expect.anything(), expect.anything(), expect.anything(), COMBINED);
  });

  it('passes the operator secret to a GitOps install without creating secrets', async () => {
    const vm = await wizardWith('GitOps');
    await vm.submit();

    expect(ensureRegistrySecretSimple).not.toHaveBeenCalled();
    expect(publishToFleetGit).toHaveBeenCalledWith(expect.objectContaining({ pullSecretNames: [COMBINED] }));
  });

  it('replaces a legacy per-registry secret with the operator secret on a FleetBundle upgrade', async () => {
    const legacy = [{ name: LEGACY }];
    const vm = await wizardWith('FleetBundle', 'manage', { imagePullSecrets: legacy, global: { imagePullSecrets: legacy } });
    await vm.submit();

    expect(ensureRegistrySecretSimple).not.toHaveBeenCalled();
    const params = vi.mocked(createFleetBundle).mock.calls[0][1];
    expect(params.additionalPullSecretNames).toEqual([COMBINED]);
    // The real createFleetBundle merges these names into imagePullSecrets and drops the legacy one.
    expect(params.additionalPullSecretNames?.some(n => n.startsWith('suse-ai-pull-secret-'))).toBe(false);
  });

  it('passes only the operator secret on a GitOps upgrade', async () => {
    const vm = await wizardWith('GitOps', 'manage', { imagePullSecrets: [{ name: LEGACY }] });
    await vm.submit();

    expect(ensureRegistrySecretSimple).not.toHaveBeenCalled();
    expect(publishToFleetGit).toHaveBeenCalledWith(expect.objectContaining({ pullSecretNames: [COMBINED] }));
  });

  it('replaces a legacy per-registry secret with the operator secret on a local Helm upgrade', async () => {
    const legacy = [{ name: LEGACY }];
    const vm = await wizardWith('Helm', 'manage', { imagePullSecrets: legacy, global: { imagePullSecrets: legacy } });
    await vm.submit();
    await flushPromises();

    expect(ensureRegistrySecretSimple).not.toHaveBeenCalled();
    expect(createOrUpgradeApp).toHaveBeenCalled();
    const call = vi.mocked(createOrUpgradeApp).mock.calls[0];
    expect(call[6]).toBe('upgrade');
    const values = call[5] as Record<string, any>;
    expect(values.imagePullSecrets).toEqual([{ name: COMBINED }]);
    expect(values.global.imagePullSecrets).toEqual([{ name: COMBINED }]);
  });

  it('keeps a user pull secret and drops the legacy one on a local Helm upgrade', async () => {
    const vm = await wizardWith('Helm', 'manage', { imagePullSecrets: [{ name: 'my-mirror' }, { name: 'suse-ai-pull-secret-x' }] });
    await vm.submit();
    await flushPromises();

    const call = vi.mocked(createOrUpgradeApp).mock.calls[0];
    expect(call[6]).toBe('upgrade');
    const values = call[5] as Record<string, any>;
    expect(values.imagePullSecrets).toEqual([{ name: 'my-mirror' }, { name: COMBINED }]);
    expect(values.global.imagePullSecrets).toEqual([{ name: COMBINED }]);
  });

  it('keeps a user pull secret on a local Helm install', async () => {
    const vm = await wizardWith('Helm', 'install', { global: { imagePullSecrets: ['my-mirror'] } });
    await vm.submit();
    await flushPromises();

    const values = vi.mocked(createOrUpgradeApp).mock.calls[0][5] as Record<string, any>;
    expect(values.global.imagePullSecrets).toEqual(['my-mirror', { name: COMBINED }]);
    expect(values.imagePullSecrets).toEqual([{ name: COMBINED }]);
  });

  it('keeps a local Helm upgrade going with the values unchanged when the registry status is unavailable', async () => {
    vi.mocked(getRegistryStatus).mockRejectedValue(new Error('operator down'));
    const legacy = [{ name: LEGACY }];
    const vm = await wizardWith('Helm', 'manage', { imagePullSecrets: legacy, global: { imagePullSecrets: legacy } });
    await vm.submit();
    await flushPromises();

    const call = vi.mocked(createOrUpgradeApp).mock.calls[0];
    expect(call[6]).toBe('upgrade');
    const values = call[5] as Record<string, any>;
    expect(values.imagePullSecrets).toEqual(legacy);
    expect(values.global.imagePullSecrets).toEqual(legacy);
  });

  it('shows the operator secret in the Configuration values when a registry is configured', async () => {
    const vm = await wizardWith('FleetBundle');
    await vm.resolvePullSecretNames();

    expect(vm.form.values.imagePullSecrets).toEqual([{ name: COMBINED }]);
    expect(vm.form.values.global.imagePullSecrets).toEqual([{ name: COMBINED }]);
  });

  it('keeps a user pull secret in the Configuration values', async () => {
    const vm = await wizardWith('FleetBundle', 'manage', { imagePullSecrets: [{ name: 'my-mirror' }, { name: LEGACY }] });
    await vm.resolvePullSecretNames();

    expect(vm.form.values.imagePullSecrets).toEqual([{ name: 'my-mirror' }, { name: COMBINED }]);
    expect(vm.form.values.global.imagePullSecrets).toEqual([{ name: COMBINED }]);
  });

  it('leaves imagePullSecrets unset when no registry is configured', async () => {
    vi.mocked(getRegistryStatus).mockResolvedValue({ applicationCollection: false, suseRegistry: false, nvidia: false });
    const vm = await wizardWith('FleetBundle');
    await vm.resolvePullSecretNames();

    expect(vm.form.values.imagePullSecrets).toBeUndefined();
    expect(vm.form.values.global).toBeUndefined();
  });

  it('keeps installing with no pull secret names when the registry status is unavailable', async () => {
    vi.mocked(getRegistryStatus).mockRejectedValue(new Error('operator down'));
    const vm = await wizardWith('FleetBundle');
    await vm.submit();

    expect(createFleetBundle).toHaveBeenCalledWith(expect.anything(), expect.objectContaining({ additionalPullSecretNames: [] }));
    expect(vm.error).toBeNull();
  });

  it('keeps a GitOps install going with no pull secret names when the registry status is unavailable', async () => {
    vi.mocked(getRegistryStatus).mockRejectedValue(new Error('operator down'));
    const vm = await wizardWith('GitOps');
    await vm.submit();

    expect(publishToFleetGit).toHaveBeenCalledWith(expect.objectContaining({ pullSecretNames: [] }));
  });
});
