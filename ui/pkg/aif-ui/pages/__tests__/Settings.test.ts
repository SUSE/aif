// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import Settings from '../Settings.vue';
import { nextTick } from 'vue';
import { LabeledInput } from '@components/Form/LabeledInput';
import { getSettings, putSettings } from '../../utils/operator-api';

vi.mock('../../utils/operator-api', () => ({
  getSettings: vi.fn().mockResolvedValue({ spec: {} }),
  putSettings: vi.fn().mockResolvedValue({ spec: {} }),
  validateCredentials: vi.fn().mockResolvedValue({ results: [] }),
}));

vi.mock('../../utils/operator-config', () => ({
  loadOperatorConfig: vi.fn().mockResolvedValue({}),
  getOperatorNamespace: vi.fn().mockReturnValue('aif-operator'),
}));

vi.mock('../../services/registry-endpoints', () => ({
  resolveRegistryEndpoints: vi.fn().mockReturnValue({}),
  registryEndpointOverrides: vi.fn().mockReturnValue({}),
}));

vi.mock('../../services/rancher-token', () => ({
  mintOperatorToken: vi.fn(),
  ensureTokenSecret: vi.fn(),
  deleteToken: vi.fn(),
  requestErrorMessage: vi.fn((e) => String(e)),
  TOKEN_EXPIRES_ANNOTATION: 'suseai.io/token-expires-at',
  TOKEN_NAME_ANNOTATION: 'suseai.io/token-name',
  DEFAULT_TOKEN_SECRET_NAME: 'aif-rancher-token',
  DEFAULT_TOKEN_SECRET_KEY: 'token',
}));

vi.mock('../../utils/custom-repos', () => ({
  emptyCustomRepo: vi.fn(() => ({})),
  buildCustomReposCrd: vi.fn(() => []),
  buildCustomReposForm: vi.fn(() => []),
  validateCustomRepoForm: vi.fn().mockReturnValue(null),
}));

vi.mock('../components/RegistryConnectionStatus.vue', () => ({
  default: {
    name: 'RegistryConnectionStatus',
    props: ['target', 'configuration', 'repoName', 'testChart'],
    emits: ['update:testChart'],
    template: '<div class="registry-connection-status" />',
  },
}));

vi.mock('@shell/components/AsyncButton', () => ({
  default: { template: '<button><slot /></button>' },
}));

vi.mock('@shell/components/AppModal', () => ({
  default: { template: '<div class="app-modal"><slot /></div>' },
}));

vi.mock('@shell/components/Loading', () => ({
  default: { template: '<div>Loading...</div>' },
}));

vi.mock('@shell/components/form/LabeledSelect', () => ({
  default: { template: '<select><slot /></select>' },
}));

vi.mock('@shell/components/form/SecretSelector', () => ({
  default: { template: '<div class="secret-selector" />' },
}));

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../l10n/en-us.yaml'), 'utf8'));

async function mountSettings(options: { route?: { query?: Record<string, string> } } = {}) {
  const query = options.route?.query || {};
  const store = {
    dispatch: vi.fn().mockResolvedValue({}),
    getters: { 'i18n/t': (key: string) => key },
  };

  // Like Rancher's i18n t(): output is HTML-escaped unless raw is truthy, so a
  // template that renders it with {{ }} must pass raw to avoid literal entities.
  const t = (key: string, args: Record<string, string> = {}, raw?: unknown) => {
    const value = key.split('.').reduce<any>((current, part) => current?.[part], translations);
    if (typeof value !== 'string') return key;
    const out = value.replace(/\{(\w+)\}/g, (_match, name) => args[name] ?? '');
    return raw ? out : out.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  };

  const wrapper = mount(Settings, {
    global: {
      mocks: {
        $store: store,
        $route: { query },
        t,
      },
    },
  });

  // Manually call the fetch() lifecycle hook
  await (wrapper.vm as any).$options.fetch.call(wrapper.vm);
  await flushPromises();

  return wrapper;
}

describe('Settings page - Repositories grouping', () => {
  it('renders a Repositories parent that groups the built-in repo sections and the Add button', async () => {
    const wrapper = await mountSettings();
    const parent = wrapper.find('[data-testid="section-repositories"]');
    expect(parent.exists()).toBe(true);
    // The built-in repo sections and the Add Repository button live inside the group.
    const group = wrapper.find('.repositories-group');
    expect(group.find('[data-testid="section-appCollection"]').exists()).toBe(true);
    expect(group.find('[data-testid="section-suseRegistry"]').exists()).toBe(true);
    expect(group.find('[data-testid="section-nvidia"]').exists()).toBe(true);
    expect(group.find('[data-testid="add-custom-repo"]').exists()).toBe(true);
    // The old grouped "Custom Repositories" section is gone.
    expect(group.find('[data-testid="section-customRepos"]').exists()).toBe(false);
    // Fleet and Rancher remain OUTSIDE the group.
    expect(group.find('[data-testid="section-fleet"]').exists()).toBe(false);
  });

  it('expanding a child section also expands the Repositories parent via deep-link', async () => {
    const wrapper = await mountSettings({ route: { query: { section: 'nvidia' } } });
    expect((wrapper.vm as any).expanded.repositories).toBe(true);
    expect((wrapper.vm as any).expanded.nvidia).toBe(true);
  });
});

describe('Settings page - Custom Repositories', () => {
  it('Add Repository appends a new custom-repo accordion, auto-expanded', async () => {
    const wrapper = await mountSettings();
    await flushPromises();
    expect(wrapper.find('[data-testid="section-customRepo-0"]').exists()).toBe(false);

    await wrapper.find('[data-testid="add-custom-repo"]').trigger('click');

    expect((wrapper.vm as any).spec.customRepos.length).toBe(1);
    expect((wrapper.vm as any).customRepoExpanded[0]).toBe(true);
    const box = wrapper.find('[data-testid="section-customRepo-0"]');
    expect(box.exists()).toBe(true);
    // The accordion header carries a blue "Custom" badge that identifies it as a custom repo.
    const badge = box.find('.badge-state.bg-info');
    expect(badge.exists()).toBe(true);
    expect(badge.text()).toBe('Custom');
    // Add button stays available (no per-form lockout in the live-bind model).
    expect(wrapper.find('[data-testid="add-custom-repo"]').attributes('disabled')).toBeUndefined();
  });

  it('renders RegistryConnectionStatus and a Delete button inside an expanded repo accordion', async () => {
    const wrapper = await mountSettings();
    await flushPromises();
    await wrapper.find('[data-testid="add-custom-repo"]').trigger('click');
    await flushPromises();

    expect(wrapper.find('[data-testid="section-customRepo-0"] .registry-connection-status').exists()).toBe(true);
    expect(wrapper.find('[data-testid="delete-custom-repo-0"]').exists()).toBe(true);
  });

  it('binds the custom repo test chart to its Test so Apply saves the chart', async () => {
    const wrapper = await mountSettings();
    await flushPromises();
    await wrapper.find('[data-testid="add-custom-repo"]').trigger('click');
    await flushPromises();

    const status = wrapper.findAllComponents({ name: 'RegistryConnectionStatus' })
      .find(c => c.props('target') === 'customRepo')!;
    status.vm.$emit('update:testChart', 'demo');
    await flushPromises();

    expect((wrapper.vm as any).spec.customRepos[0].testChart).toBe('demo');
    expect(status.props('testChart')).toBe('demo');
  });

  it('Delete opens a confirmation modal and only removes the repo on confirm', async () => {
    const wrapper = await mountSettings();
    await flushPromises();
    await wrapper.find('[data-testid="add-custom-repo"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-testid="section-customRepo-0"]').exists()).toBe(true);

    // Clicking Delete opens the modal but does NOT remove the repo yet.
    await wrapper.find('[data-testid="delete-custom-repo-0"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-testid="confirm-delete-custom-repo"]').exists()).toBe(true);
    expect((wrapper.vm as any).spec.customRepos.length).toBe(1);
    expect(wrapper.find('[data-testid="section-customRepo-0"]').exists()).toBe(true);

    // Confirming removes the repo and closes the modal.
    await wrapper.find('[data-testid="confirm-delete-custom-repo"]').trigger('click');
    await flushPromises();
    expect((wrapper.vm as any).spec.customRepos.length).toBe(0);
    expect(wrapper.find('[data-testid="section-customRepo-0"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="confirm-delete-custom-repo"]').exists()).toBe(false);
  });

  it('Cancel in the delete modal keeps the repo', async () => {
    const wrapper = await mountSettings();
    await flushPromises();
    await wrapper.find('[data-testid="add-custom-repo"]').trigger('click');
    await flushPromises();

    await wrapper.find('[data-testid="delete-custom-repo-0"]').trigger('click');
    await flushPromises();
    await wrapper.find('[data-testid="cancel-delete-custom-repo"]').trigger('click');
    await flushPromises();

    expect((wrapper.vm as any).spec.customRepos.length).toBe(1);
    expect(wrapper.find('[data-testid="section-customRepo-0"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="confirm-delete-custom-repo"]').exists()).toBe(false);
  });
});

const PARTNER = {
  name: 'partner-blueprints', repoURL: 'https://github.com/suse/partner-blueprints.git', branch: 'main', paths: ['partners'],
};
const TEAM_A = { name: 'team-a', repoURL: 'https://git.example.com/a.git', branch: 'main' };

function settingsWithHelmCatalog(catalogs: any[] = [PARTNER, TEAM_A], managed: string[] = ['partner-blueprints']) {
  return {
    metadata: { annotations: { 'ai-factory.suse.com/helm-managed': JSON.stringify({ blueprintCatalogs: managed }) } },
    spec:     { blueprintCatalogs: catalogs },
  };
}

async function mountWithCatalogs(settings: any) {
  vi.mocked(getSettings).mockResolvedValueOnce(settings);
  const wrapper = await mountSettings();

  (wrapper.vm as any).expanded.blueprintCatalogs = true;
  await nextTick();

  return wrapper;
}

describe('Settings page - Helm-managed blueprint catalogs', () => {
  it('renders a Helm catalog read-only with a badge and no remove button', async () => {
    const wrapper = await mountWithCatalogs(settingsWithHelmCatalog());
    const helmRow = wrapper.find('[data-testid="catalog-row-0"]');
    const uiRow = wrapper.find('[data-testid="catalog-row-1"]');

    expect(helmRow.find('[data-testid="catalog-helm-badge-0"]').exists()).toBe(true);
    expect(helmRow.find('[data-testid="catalog-remove-0"]').exists()).toBe(false);
    expect(helmRow.findAllComponents(LabeledInput).every((c: any) => c.props('mode') === 'view')).toBe(true);

    expect(uiRow.find('[data-testid="catalog-helm-badge-1"]').exists()).toBe(false);
    expect(uiRow.find('[data-testid="catalog-remove-1"]').exists()).toBe(true);
    expect(uiRow.findAllComponents(LabeledInput).every((c: any) => c.props('mode') === 'edit')).toBe(true);
  });

  it('never sends Helm catalogs on Apply', async () => {
    const wrapper = await mountWithCatalogs(settingsWithHelmCatalog());

    vi.mocked(putSettings).mockClear();
    vi.mocked(putSettings).mockResolvedValueOnce(settingsWithHelmCatalog());
    await (wrapper.vm as any).save(() => {});

    const sent = vi.mocked(putSettings).mock.calls[0][0];

    expect(sent.blueprintCatalogs.map((c: any) => c.name)).toEqual(['team-a']);
  });

  it('keeps Helm rows read-only after saving, from the PUT response', async () => {
    const wrapper = await mountWithCatalogs(settingsWithHelmCatalog());

    vi.mocked(putSettings).mockResolvedValueOnce(settingsWithHelmCatalog());
    await (wrapper.vm as any).save(() => {});

    expect((wrapper.vm as any).spec.blueprintCatalogs.map((c: any) => c.helmManaged)).toEqual([true, false]);
  });

  // Review Focus 2: Helm names are not held to the page's own name rules.
  it('does not block Apply when a Helm catalog name breaks the page name rules', async () => {
    const longName = 'a-very-long-helm-declared-catalog-name-over-forty-chars';
    const wrapper = await mountWithCatalogs(settingsWithHelmCatalog([{ ...PARTNER, name: longName }], [longName]));

    expect((wrapper.vm as any).validateBlueprintCatalogs()).toEqual([]);
  });

  // Review Focus 5: a new row reusing a Helm-declared name gets a specific error.
  it('rejects a new catalog that reuses a Helm-declared name', async () => {
    const wrapper = await mountWithCatalogs(settingsWithHelmCatalog([PARTNER]));
    const vm = wrapper.vm as any;

    vm.addCatalog();
    vm.spec.blueprintCatalogs[1].name = 'partner-blueprints';
    vm.spec.blueprintCatalogs[1].touched = true;

    expect(vm.catalogNameError(vm.spec.blueprintCatalogs[1], 1)).toContain('declared in Helm values');
    expect(vm.validateBlueprintCatalogs()).toHaveLength(1);
  });

  it('renders the Helm help text without HTML entities', async () => {
    const wrapper = await mountWithCatalogs(settingsWithHelmCatalog());
    const text = wrapper.find('[data-testid="catalog-row-0"]').text();

    expect(text).toContain("--set-json 'blueprintCatalogs=[]'");
    expect(text).not.toContain('&#39;');
  });

  it('shows the Helm-name collision message without HTML entities', async () => {
    const wrapper = await mountWithCatalogs(settingsWithHelmCatalog([PARTNER]));
    const vm = wrapper.vm as any;

    vm.addCatalog();
    vm.spec.blueprintCatalogs[1].name = 'partner-blueprints';
    vm.spec.blueprintCatalogs[1].touched = true;

    expect(vm.catalogNameError(vm.spec.blueprintCatalogs[1], 1)).toBe('"partner-blueprints" is declared in Helm values; choose another name');
  });

  // Review Focus 3: no annotation means everything is editable, as before.
  it('treats every catalog as editable when the annotation is absent', async () => {
    const wrapper = await mountWithCatalogs({ spec: { blueprintCatalogs: [PARTNER] } });

    expect((wrapper.vm as any).spec.blueprintCatalogs[0].helmManaged).toBe(false);
    expect(wrapper.find('[data-testid="catalog-remove-0"]').exists()).toBe(true);
  });
});
