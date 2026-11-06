// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import type { ComponentCustomProperties } from 'vue';
import type { Router, RouteLocationNormalizedLoaded } from 'vue-router';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import AppWizard from '../AppWizard.vue';
import { fetchUserNamespaces, findChartInRepo, listChartVersions } from '../../../services/rancher-apps';
import { isManagedRepoName } from '../../../services/app-collection';

vi.mock('../../../services/rancher-apps', async (original) => ({
  ...await original<typeof import('../../../services/rancher-apps')>(),
  fetchUserNamespaces:       vi.fn(),
  getAllClusters:            vi.fn().mockResolvedValue([]),
  inferClusterRepoForChart:  vi.fn().mockResolvedValue(null),
  getInstalledHelmDetails:   vi.fn().mockResolvedValue({}),
  findChartInRepo:           vi.fn().mockResolvedValue(null),
  listChartVersions:         vi.fn().mockResolvedValue([]),
}));
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
  getRegistryCredentials: vi.fn().mockResolvedValue(null),
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
