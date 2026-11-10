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
import { listChartVersionEntries } from '../../services/rancher-apps';
import type { AppCollectionItem, ManagedRepo } from '../../services/app-collection';

vi.mock('../../formatters/AppLabels.vue', () => ({ default: { template: '<span />' } }));
vi.mock('../../pages/components/RepositoryHealthBanner.vue', () => ({ default: { template: '<div />' } }));
vi.mock('../../components/AppDetailPanel.vue', () => ({
  default: {
    name:     'AppDetailPanel',
    props:    ['app', 'unavailable', 'availabilityMessage', 'settingsLink', 'versions', 'versionsLoading', 'versionsError'],
    emits:    ['close', 'select-version'],
    template: `<div data-testid="detail-panel" :data-slug="app.slug_name">
      <button data-testid="panel-version" @click="$emit('select-version', '1.0.0')" />
      <button data-testid="panel-close" @click="$emit('close')" />
    </div>`,
  },
}));
vi.mock('../../services/static-catalog', () => ({ fetchStaticCatalog: vi.fn() }));
vi.mock('../../services/rancher-apps', async (original) => ({
  ...await original<typeof import('../../services/rancher-apps')>(),
  listChartVersionEntries: vi.fn(),
}));
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

async function setup(options: { apps?: AppCollectionItem[], repos?: ManagedRepo[], attachTo?: HTMLElement } = {}) {
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
    attachTo: options.attachTo,
    global: {
      config: {
        globalProperties: {
          $router: router,
          $route: route,
          $store: store,
          t,
          $t: t,
          // Required by @rancher/shell's `fetch.client` mixin typings; the page
          // under test doesn't read it, so a stub satisfying the shape suffices.
          $fetchState: { pending: false, error: null, timestamp: 0 }
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
  vi.mocked(listChartVersionEntries).mockReset().mockResolvedValue([]);
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

describe('Apps page - detail panel and Install buttons', () => {
  const readyApp: AppCollectionItem = {
    name: 'Ready App', slug_name: 'ready-app', library: 'suse-ai', packaging_format: 'HELM_CHART', repository_name: 'repo-a',
  };
  const brokenApp: AppCollectionItem = {
    name: 'Broken App', slug_name: 'broken-app', library: 'suse-ai', packaging_format: 'HELM_CHART', repository_name: 'repo-b',
  };
  const repos: ManagedRepo[] = [
    { name: 'repo-a', url: 'oci://a', library: 'suse-ai', ready: true },
    { name: 'repo-b', url: 'oci://b', library: 'suse-ai', ready: false, message: 'down' },
  ];
  const tileFor = (wrapper: ReturnType<typeof mount>, name: string) => {
    const tile = wrapper.findAll('.app-tile').find(candidate => candidate.find('.tile-title').exists() && candidate.find('.tile-title').text() === name);
    if (!tile) throw new Error(`tile not found: ${ name }`);
    return tile;
  };
  const installRoute = expect.objectContaining({
    name:   'c-cluster-suseai-install',
    params: expect.objectContaining({ slug: 'ready-app' }),
    query:  expect.objectContaining({ repo: 'repo-a' }),
  });

  it('opens the detail panel on tile click without navigating', async () => {
    const { wrapper, push } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').trigger('click');

    const panel = wrapper.find('[data-testid="detail-panel"]');
    expect(panel.exists()).toBe(true);
    expect(panel.attributes('data-slug')).toBe('ready-app');
    expect(push).not.toHaveBeenCalled();
  });

  it('opens the detail panel with Enter and Space on a focused tile', async () => {
    const { wrapper, push } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').trigger('keydown', { key: 'Enter' });
    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(true);

    await wrapper.find('[data-testid="panel-close"]').trigger('click');
    await tileFor(wrapper, 'Ready App').trigger('keydown', { key: ' ' });
    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(true);
    expect(push).not.toHaveBeenCalled();
  });

  it('installs from the tile Install button without opening the panel', async () => {
    const { wrapper, push } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').find('[data-testid="app-install"]').trigger('click');
    await flushPromises();

    expect(push).toHaveBeenCalledWith(installRoute);
    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(false);
  });

  it('installs with Enter on the tile Install button without opening the panel', async () => {
    const { wrapper, push } = await setup({ apps: [readyApp], repos });
    const install = tileFor(wrapper, 'Ready App').find('[data-testid="app-install"]');

    // A browser turns Enter on a focused button into keydown followed by click;
    // jsdom does not, so both are dispatched here.
    await install.trigger('keydown', { key: 'Enter' });
    await install.trigger('click');
    await flushPromises();

    expect(push).toHaveBeenCalledWith(installRoute);
    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(false);
  });

  it('closes the panel on Escape pressed from a focused tile Install button', async () => {
    // Attached so the keydown can bubble from the button up to the document listener.
    const { wrapper } = await setup({ apps: [readyApp], repos, attachTo: document.body });

    await tileFor(wrapper, 'Ready App').trigger('click');
    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(true);

    const install = tileFor(wrapper, 'Ready App').find('[data-testid="app-install"]').element as HTMLElement;
    install.focus();
    install.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await flushPromises();

    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(false);
  });

  it('opens the panel for an unavailable app, whose tile shows the status instead of Install', async () => {
    const { wrapper } = await setup({ apps: [brokenApp], repos });

    const tile = tileFor(wrapper, 'Broken App');
    expect(tile.find('[data-testid="app-install"]').exists()).toBe(false);
    expect(tile.get('.tile-footer .availability-message').text()).toContain('down');

    await tile.trigger('click');

    const panel = wrapper.findComponent({ name: 'AppDetailPanel' });
    expect(panel.exists()).toBe(true);
    expect(panel.props('unavailable')).toBe(true);
    expect(panel.props('availabilityMessage')).toContain('down');
  });

  it('closes the panel on Escape and on backdrop click', async () => {
    const { wrapper } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').trigger('click');
    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(true);
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    await flushPromises();
    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(false);

    await tileFor(wrapper, 'Ready App').trigger('click');
    await wrapper.find('.app-detail-backdrop').trigger('click');
    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(false);
  });

  it('removes the Escape listener on unmount', async () => {
    const removeSpy = vi.spyOn(document, 'removeEventListener');
    const { wrapper } = await setup({ apps: [readyApp], repos });

    wrapper.unmount();
    mounted.splice(mounted.indexOf(wrapper), 1);

    expect(removeSpy).toHaveBeenCalledWith('keydown', expect.any(Function));
    removeSpy.mockRestore();
  });

  it('opens the panel from a list row and installs from the row button', async () => {
    const { wrapper, push } = await setup({ apps: [readyApp], repos });
    await wrapper.find('[aria-label="List View"]').trigger('click');

    const row = wrapper.find('tr.main-row');
    expect(row.find('.icon-chevron-right').exists()).toBe(false);

    await row.trigger('click');
    expect(wrapper.find('[data-testid="detail-panel"]').attributes('data-slug')).toBe('ready-app');
    expect(push).not.toHaveBeenCalled();

    await wrapper.find('[data-testid="panel-close"]').trigger('click');
    await row.find('[data-testid="app-install"]').trigger('click');
    await flushPromises();
    expect(push).toHaveBeenCalledWith(installRoute);
    expect(wrapper.find('[data-testid="detail-panel"]').exists()).toBe(false);
  });

  it('loads the app versions from its repository when the panel opens', async () => {
    vi.mocked(listChartVersionEntries).mockResolvedValue([{ version: '2.0.0', created: '2025-07-30T12:00:00Z' }]);
    const { wrapper } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').trigger('click');
    const panel = () => wrapper.findComponent({ name: 'AppDetailPanel' });
    expect(panel().props('versionsLoading')).toBe(true);

    await flushPromises();
    expect(listChartVersionEntries).toHaveBeenCalledWith(expect.anything(), 'repo-a', 'ready-app');
    expect(panel().props('versionsLoading')).toBe(false);
    expect(panel().props('versions')).toEqual([{ version: '2.0.0', created: '2025-07-30T12:00:00Z' }]);
  });

  it('reuses loaded versions when the same app is opened again', async () => {
    const { wrapper } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').trigger('click');
    await flushPromises();
    await wrapper.find('[data-testid="panel-close"]').trigger('click');
    await tileFor(wrapper, 'Ready App').trigger('click');
    await flushPromises();

    expect(listChartVersionEntries).toHaveBeenCalledTimes(1);
  });

  it('loads the new app versions when another app is opened while the panel is open', async () => {
    const otherApp: AppCollectionItem = { ...readyApp, name: 'Other App', slug_name: 'other-app' };
    let resolveFirst: (v: { version: string }[]) => void = () => {};
    vi.mocked(listChartVersionEntries)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }))
      .mockResolvedValueOnce([{ version: '9.0.0' }]);
    const { wrapper } = await setup({ apps: [readyApp, otherApp], repos });
    const panel = () => wrapper.findComponent({ name: 'AppDetailPanel' });

    // Keyboard users can reach other tiles while the drawer is open.
    await tileFor(wrapper, 'Ready App').trigger('keydown', { key: 'Enter' });
    await tileFor(wrapper, 'Other App').trigger('keydown', { key: 'Enter' });
    resolveFirst([{ version: '1.0.0' }]);
    await flushPromises();

    expect(panel().props('app').slug_name).toBe('other-app');
    expect(listChartVersionEntries).toHaveBeenLastCalledWith(expect.anything(), 'repo-a', 'other-app');
    expect(panel().props('versionsLoading')).toBe(false);
    expect(panel().props('versions')).toEqual([{ version: '9.0.0' }]);
  });

  it('reloads versions after the page is refreshed', async () => {
    const { wrapper } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').trigger('click');
    await flushPromises();
    await wrapper.find('[data-testid="panel-close"]').trigger('click');
    await wrapper.find('[aria-label="Refresh applications"]').trigger('click');
    await flushPromises();
    await tileFor(wrapper, 'Ready App').trigger('click');
    await flushPromises();

    expect(listChartVersionEntries).toHaveBeenCalledTimes(2);
  });

  it('marks versions as failed when the index cannot be read', async () => {
    vi.mocked(listChartVersionEntries).mockRejectedValue(new Error('index gone'));
    const { wrapper } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').trigger('click');
    await flushPromises();

    expect(wrapper.findComponent({ name: 'AppDetailPanel' }).props('versionsError')).toBe(true);
  });

  it('does not load versions for an unavailable app', async () => {
    const { wrapper } = await setup({ apps: [brokenApp], repos });

    await tileFor(wrapper, 'Broken App').trigger('click');
    await flushPromises();

    expect(listChartVersionEntries).not.toHaveBeenCalled();
    expect(wrapper.findComponent({ name: 'AppDetailPanel' }).props('versions')).toBeNull();
  });

  it('opens the install wizard with the version selected in the panel', async () => {
    const { wrapper, push } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').trigger('click');
    await wrapper.find('[data-testid="panel-version"]').trigger('click');
    await flushPromises();

    expect(push).toHaveBeenCalledWith(expect.objectContaining({
      name:  'c-cluster-suseai-install',
      query: expect.objectContaining({ repo: 'repo-a', version: '1.0.0' }),
    }));
  });

  it('does not add a version to the install route from the tile Install button', async () => {
    const { wrapper, push } = await setup({ apps: [readyApp], repos });

    await tileFor(wrapper, 'Ready App').find('[data-testid="app-install"]').trigger('click');
    await flushPromises();

    expect(push.mock.calls[0][0].query).not.toHaveProperty('version');
  });

  it('does not show version or last-updated on tiles', async () => {
    const lastUpdated = '2026-09-15T10:00:00Z';
    const { wrapper } = await setup({
      apps: [{ ...readyApp, latest_version: '3.1.3', last_updated_at: lastUpdated }],
      repos,
    });

    const text = tileFor(wrapper, 'Ready App').text();
    expect(text).not.toContain('3.1.3');
    expect(text).not.toContain(new Date(lastUpdated).toLocaleDateString());
  });
});
