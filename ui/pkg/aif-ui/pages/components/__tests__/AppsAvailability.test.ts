// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import type { Router, RouteLocationNormalizedLoaded } from 'vue-router';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import Apps from '../../Apps.vue';
import { fetchStaticCatalog } from '../../../services/static-catalog';
import { fetchManagedRepos } from '../../../services/app-collection';

vi.mock('../../../formatters/AppLabels.vue', () => ({ default: { template: '<span />' } }));
vi.mock('../../../services/static-catalog', () => ({ fetchStaticCatalog: vi.fn() }));
vi.mock('../../../services/app-collection', async (original) => ({
  ...await original<typeof import('../../../services/app-collection')>(), fetchManagedRepos: vi.fn(),
}));
vi.mock('../../../utils/operator-config', () => ({ getUseStaticCatalog: () => true, loadOperatorConfig: vi.fn() }));

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../../l10n/en-us.yaml'), 'utf8'));
const t = (key: string | { k: string }): string => {
  const name = typeof key === 'string' ? key : key.k;
  return name.split('.').reduce<any>((value, part) => value?.[part], translations) || name;
};
const mounted: ReturnType<typeof mount>[] = [];

async function setup() {
  const push = vi.fn().mockResolvedValue(undefined);
  const router = { push, replace: vi.fn().mockResolvedValue(undefined) } as unknown as Router;
  const route = { params: { cluster: 'local' }, query: {} } as unknown as RouteLocationNormalizedLoaded;
  const store = { getters: { 'i18n/t': t }, dispatch: vi.fn(), commit: vi.fn() };
  // $fetchState is required by @rancher/shell's `fetch.client` mixin typings; the
  // page under test doesn't read it, so a stub satisfying the shape suffices.
  const $fetchState = { pending: false, error: null, timestamp: 0 };
  const wrapper = mount(Apps, { global: {
    config: { globalProperties: { $router: router, $route: route, $store: store, t, $t: t, $fetchState } },
    stubs: { RouterLink: { props: ['to'], template: '<a :href="typeof to === \'string\' ? to : to.name"><slot /></a>' } },
  } });
  mounted.push(wrapper);
  await flushPromises();
  return { wrapper, push };
}

const repository = { name: 'suse-ai-registry', url: 'oci://mirror.internal/charts', library: 'suse-ai' as const, ready: false, message: '401 Unauthorized' };

beforeEach(() => {
  vi.mocked(fetchStaticCatalog).mockReset().mockResolvedValue([{ name: 'Qdrant', slug_name: 'qdrant', library: 'suse-ai', packaging_format: 'HELM_CHART', repository_url: 'oci://registry.suse.com/ai/charts' }]);
  vi.mocked(fetchManagedRepos).mockReset().mockResolvedValue([repository]);
});
afterEach(() => mounted.splice(0).forEach(wrapper => wrapper.unmount()));

describe('unavailable application interaction', () => {
  it('keeps a failed app visible, shows the repository status where Install was, and never installs', async () => {
    const { wrapper, push } = await setup();
    const tile = wrapper.get('.app-tile');
    const footer = tile.get('.tile-footer');
    expect(tile.find('[data-testid="app-install"]').exists()).toBe(false);
    expect(footer.get('.availability-message').text()).toContain('Repository unavailable: 401 Unauthorized');
    expect(footer.get('a').attributes('href')).toBe('c-cluster-suseai-settings');
    expect(tile.get('.tile-content').find('.availability-message').exists()).toBe(false);
    // Tile activation opens the detail panel rather than installing.
    await tile.trigger('click');
    await tile.trigger('keydown.enter');
    await tile.trigger('keydown.space');
    await flushPromises();
    expect(push).not.toHaveBeenCalled();
  });

  it('also shows the repository status in place of Install in list view', async () => {
    const { wrapper, push } = await setup();
    await wrapper.get('[aria-label="List View"]').trigger('click');
    const row = wrapper.get('.main-row');
    expect(row.find('[data-testid="app-install"]').exists()).toBe(false);
    expect(row.get('.col-actions .availability-message').text()).toContain('401 Unauthorized');
    expect(row.get('.col-actions a').attributes('href')).toBe('c-cluster-suseai-settings');
    expect(row.get('.col-description').find('.availability-message').exists()).toBe(false);
    await row.trigger('click');
    await row.trigger('keydown.enter');
    await flushPromises();
    expect(push).not.toHaveBeenCalled();
  });

  it('enables the app after recovery and routes to the actual mirror identity', async () => {
    const { wrapper, push } = await setup();
    vi.mocked(fetchManagedRepos).mockResolvedValue([{ ...repository, ready: true, message: undefined }]);
    await wrapper.get('[aria-label="Refresh applications"]').trigger('click');
    await flushPromises();
    const install = wrapper.get('.app-tile [data-testid="app-install"]');
    expect(install.attributes('disabled')).toBeUndefined();
    expect(wrapper.find('.app-tile .availability-message').exists()).toBe(false);
    await install.trigger('click');
    await flushPromises();
    expect(push).toHaveBeenCalledWith(expect.objectContaining({ query: expect.objectContaining({ repo: 'suse-ai-registry' }) }));
  });

  it('keeps an unreadable repository list distinct from an unconfigured registry', async () => {
    vi.mocked(fetchManagedRepos).mockRejectedValue(new Error('Forbidden'));
    const { wrapper, push } = await setup();
    expect(wrapper.text()).toContain('Chart repository status could not be checked');
    expect(wrapper.get('.app-tile').text()).toContain('Repository status unknown');
    await wrapper.get('.app-tile').trigger('click');
    expect(push).not.toHaveBeenCalled();
  });
});
