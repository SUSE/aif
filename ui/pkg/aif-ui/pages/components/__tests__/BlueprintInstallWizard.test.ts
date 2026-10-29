// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import type { Router, RouteLocationNormalizedLoaded } from 'vue-router';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import BlueprintInstallWizard from '../BlueprintInstallWizard.vue';
import { getBlueprint } from '../../../utils/blueprint-api';
import { listAIWorkloads } from '../../../utils/operator-api';
import { fetchUserNamespaces } from '../../../services/rancher-apps';

vi.mock('../../../utils/blueprint-api', async (original) => ({
  ...await original<typeof import('../../../utils/blueprint-api')>(), getBlueprint: vi.fn(),
}));
vi.mock('../../../utils/operator-api', () => ({
  listAIWorkloads:        vi.fn(),
  getRegistryCredentials: vi.fn().mockResolvedValue(null),
  getSettings:            vi.fn().mockResolvedValue({}),
  createAIWorkload:       vi.fn(),
  updateAIWorkload:       vi.fn(),
}));
vi.mock('../../../services/rancher-apps', async (original) => ({
  ...await original<typeof import('../../../services/rancher-apps')>(),
  fetchUserNamespaces: vi.fn(),
  getAllClusters:      vi.fn().mockResolvedValue([]),
}));

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
      config: { globalProperties: { $store: store, $route: route, $router: router } },
      stubs:  { TargetStep: true, RouterLink: true, Loading: true, Banner: { template: '<div><slot /></div>' } },
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
