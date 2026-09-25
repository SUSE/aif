// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import type { Router, RouteLocationNormalizedLoaded } from 'vue-router';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import Apps from '../Apps.vue';
import { fetchStaticCatalog } from '../../services/static-catalog';
import { fetchManagedRepos } from '../../services/app-collection';
import type { AppCollectionItem, ManagedRepo } from '../../services/app-collection';

vi.mock('../../formatters/AppLabels.vue', () => ({ default: { template: '<span />' } }));
vi.mock('../../pages/components/RepositoryHealthBanner.vue', () => ({ default: { template: '<div />' } }));
vi.mock('../../services/static-catalog', () => ({ fetchStaticCatalog: vi.fn() }));
vi.mock('../../services/app-collection', async (original) => ({
  ...await original<typeof import('../../services/app-collection')>(),
  fetchManagedRepos: vi.fn(),
}));
vi.mock('../../utils/operator-config', () => ({
  getUseStaticCatalog: () => true,
  loadOperatorConfig: vi.fn()
}));

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../l10n/en-us.yaml'), 'utf8'));
const t = (key: string | { k: string }): string => {
  const name = typeof key === 'string' ? key : key.k;
  return name.split('.').reduce<any>((value, part) => value?.[part], translations) || name;
};
const mounted: ReturnType<typeof mount>[] = [];

async function setup(options: { apps?: AppCollectionItem[], repos?: ManagedRepo[] } = {}) {
  const push = vi.fn().mockResolvedValue(undefined);
  const router = { push, replace: vi.fn().mockResolvedValue(undefined) } as unknown as Router;
  const route = { params: { cluster: 'local' }, query: {} } as unknown as RouteLocationNormalizedLoaded;
  const store = { getters: { 'i18n/t': t }, dispatch: vi.fn(), commit: vi.fn() };

  if (options.apps !== undefined) {
    vi.mocked(fetchStaticCatalog).mockResolvedValue(options.apps);
  }
  if (options.repos !== undefined) {
    vi.mocked(fetchManagedRepos).mockResolvedValue(options.repos);
  }

  const wrapper = mount(Apps, {
    global: {
      config: {
        globalProperties: {
          $router: router,
          $route: route,
          $store: store,
          t,
          $t: t
        }
      },
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a :href="typeof to === \'string\' ? to : to.name"><slot /></a>'
        }
      },
    }
  });
  mounted.push(wrapper);
  await flushPromises();
  return { wrapper, push };
}

beforeEach(() => {
  vi.mocked(fetchStaticCatalog).mockReset().mockResolvedValue([]);
  vi.mocked(fetchManagedRepos).mockReset().mockResolvedValue([]);
});

afterEach(() => mounted.splice(0).forEach(wrapper => wrapper.unmount()));

describe('Apps page - custom repository filter labels', () => {
  it('shows one dropdown entry per custom repo, labeled by displayName', async () => {
    const { wrapper } = await setup({
      apps: [
        { name: 'App A', slug_name: 'a', library: 'suse-ai', packaging_format: 'HELM_CHART', repository_url: 'oci://r1' },
        { name: 'App B', slug_name: 'b', library: 'custom-prom', packaging_format: 'HELM_CHART', repository_url: 'oci://r2' },
        { name: 'App C', slug_name: 'c', library: 'custom-internal', packaging_format: 'HELM_CHART', repository_url: 'oci://r3' },
      ],
      repos: [
        { name: 'custom-prom', url: 'oci://r2', library: 'custom', ready: true, displayName: 'Prometheus Community' },
        { name: 'custom-internal', url: 'oci://r3', library: 'custom', ready: true }, // no displayName -> humanized
      ],
    });

    const select = wrapper.get('#repository-filter');
    const options = select.findAll('option');
    const labels = options.map(o => o.text());

    expect(labels).toContain('SUSE AI Library');
    expect(labels).toContain('Prometheus Community');   // displayName
    expect(labels).toContain('Custom Internal');        // humanized fallback of 'custom-internal'
  });

  it('filters apps to the selected custom repo', async () => {
    const { wrapper } = await setup({
      apps: [
        { name: 'App B', slug_name: 'b', library: 'custom-prom', packaging_format: 'HELM_CHART', repository_url: 'oci://r2' },
        { name: 'App C', slug_name: 'c', library: 'custom-internal', packaging_format: 'HELM_CHART', repository_url: 'oci://r3' },
      ],
      repos: [
        { name: 'custom-prom', url: 'oci://r2', library: 'custom', ready: true, displayName: 'Prometheus Community' },
      ],
    });

    const select = wrapper.get('#repository-filter');
    await select.setValue('custom-prom');
    await flushPromises();

    const shown = (wrapper.vm as any).filteredApps.map((a: any) => a.slug_name);
    expect(shown).toEqual(['b']);
  });
});
