// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import Settings from '../Settings.vue';

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
    props: ['target', 'configuration', 'repoName'],
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

  const t = (key: string, args: Record<string, string> = {}) => {
    const value = key.split('.').reduce<any>((current, part) => current?.[part], translations);
    if (typeof value !== 'string') return key;
    return value.replace(/\{(\w+)\}/g, (_match, name) => args[name] ?? '');
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
