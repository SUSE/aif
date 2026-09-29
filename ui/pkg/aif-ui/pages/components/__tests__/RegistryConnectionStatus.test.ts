// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import RegistryConnectionStatus from '../RegistryConnectionStatus.vue';
import { getSettings, validateCredentials, validateChartAccess } from '../../../utils/operator-api';
import { CLUSTERREPOS_URL, MANAGED_REPO_LABEL } from '../../../services/app-collection';
import { SUSE_REGISTRY_REPO_URL } from '../../../services/registry-endpoints';

vi.mock('../../../utils/operator-api', () => ({ getSettings: vi.fn(), validateCredentials: vi.fn(), validateChartAccess: vi.fn() }));

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../../l10n/en-us.yaml'), 'utf8'));
const configuration = {
  url: SUSE_REGISTRY_REPO_URL,
  userSecretRef: { name: 'suse-auth', key: 'user' },
  tokenSecretRef: { name: 'suse-auth', key: 'token' },
  caBundleSecretRef: null,
};
const authenticated = { target: 'suseRegistry', status: 'ok' as const, host: 'registry.suse.com', latencyMs: 1121, message: '' };
const mounted: ReturnType<typeof mount>[] = [];

function setup() {
  const repo = {
    metadata: { name: 'suse-ai-registry', resourceVersion: '42', generation: 4, labels: { [MANAGED_REPO_LABEL]: 'true' } },
    spec: { url: SUSE_REGISTRY_REPO_URL },
    // SUSEAI-1089: FollowerDownloaded=True does not mean the OCI download worked.
    status: {
      observedGeneration: 4,
      conditions: [
        { type: 'FollowerDownloaded', status: 'True', message: '' },
        { type: 'OCIDownloaded', status: 'False', message: 'error 401: Unauthorized' },
      ],
    },
  };
  const store = {
    dispatch: vi.fn(async (_action, request) => request.url === CLUSTERREPOS_URL ? { items: [repo] } : repo),
  };
  const wrapper = mount(RegistryConnectionStatus, {
    props: { target: 'suseRegistry', configuration },
    global: {
      mocks: {
        $store: store,
        t: (key: string, args: Record<string, string> = {}) => {
          const value = key.split('.').reduce<any>((current, part) => current?.[part], translations);
          if (typeof value !== 'string') throw new Error(`Missing translation: ${key}`);
          return value.replace(/\{(\w+)\}/g, (_match, name) => args[name] ?? '');
        },
      },
      // Rancher registers these globally. This component supplies plain labels
      // and escaped banner slots, so the HTML directives are not exercised here.
      directives: { 'clean-html': {}, 'stripped-aria-label': {} },
      stubs: { t: true, RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  });
  mounted.push(wrapper);
  return { wrapper, store, repo };
}

async function runTest(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('button').trigger('click');
  await flushPromises();
}

beforeEach(() => {
  vi.mocked(validateChartAccess).mockReset().mockResolvedValue({ results: [{ repositoryUrl: SUSE_REGISTRY_REPO_URL, chartName: 'qdrant', status: 'failed', reason: 'accessDenied', httpStatus: 401, latencyMs: 10 }] });
  vi.mocked(getSettings).mockReset().mockResolvedValue({ spec: { suseRegistry: configuration } });
  vi.mocked(validateCredentials).mockReset().mockResolvedValue({ results: [authenticated] });
});
afterEach(() => mounted.splice(0).forEach(wrapper => wrapper.unmount()));

describe('Settings registry diagnostics', () => {
  it('shows chart access denial with actionable entitlement guidance even when login succeeds', async () => {
    const { wrapper } = setup();
    await runTest(wrapper);
    expect(wrapper.text()).toContain('Some checks failed.');
    expect(wrapper.text()).toContain('Registry responded');
    expect(wrapper.text()).toContain('Chart access failed (HTTP 401)');
    expect(wrapper.text()).toContain('SUSE AI subscription entitlement');
    expect(wrapper.text()).not.toContain('Sample chart access verified');
    expect(validateChartAccess).toHaveBeenCalledWith({ target: 'suseRegistry', configuration, chartName: 'qdrant' });
  });

  it('uses mirror guidance without asserting a SUSE subscription problem', async () => {
    vi.mocked(validateChartAccess).mockResolvedValue({ results: [{ repositoryUrl: 'oci://mirror.internal/team', chartName: 'qdrant', status: 'failed', reason: 'accessDenied', httpStatus: 403, latencyMs: 1 }] });
    const { wrapper } = setup();
    await wrapper.setProps({ configuration: { ...configuration, url: 'oci://mirror.internal/team' } });
    await runTest(wrapper);
    expect(wrapper.text()).toContain('permission to read this chart repository');
    expect(wrapper.text()).not.toContain('SUSE AI subscription entitlement');
  });

  it('invalidates direct access results when the sample chart changes', async () => {
    const { wrapper } = setup();
    await runTest(wrapper);
    await wrapper.get('input').setValue('kubeflow');
    expect(wrapper.text()).not.toContain('Chart access failed (HTTP 401)');
    expect(wrapper.text()).toContain('Settings changed since the last test.');
    await runTest(wrapper);
    expect(validateChartAccess).toHaveBeenLastCalledWith(expect.objectContaining({ chartName: 'kubeflow' }));
  });

  it('does not mark an older operator as a successful chart check', async () => {
    vi.mocked(validateChartAccess).mockRejectedValue({ status: 404 });
    const { wrapper } = setup();
    await runTest(wrapper);
    expect(wrapper.text()).toContain('Chart access checks require an updated AI Factory operator.');
    expect(wrapper.text()).not.toContain('Sample chart access verified');
  });

  it('reports success only for accessible sample metadata and a current saved index', async () => {
    vi.mocked(validateChartAccess).mockResolvedValue({ results: [{ repositoryUrl: SUSE_REGISTRY_REPO_URL, chartName: 'qdrant', version: '1.2.3', check: 'manifest', status: 'ok', latencyMs: 1 }] });
    const { wrapper, repo } = setup();
    Object.assign(repo.status, { indexConfigMapName: 'suse-index' });
    repo.status.conditions[1].status = 'True';
    repo.status.conditions[1].message = '';
    await runTest(wrapper);
    expect(wrapper.text()).toContain('Sample chart access verified and the Rancher repository index is ready.');
    expect(wrapper.text()).toContain('Chart metadata readable');
    expect(wrapper.text()).toContain('qdrant — 1.2.3');
    expect(wrapper.text()).toContain('This does not verify all charts, full downloads, or application container images.');

    repo.metadata.generation++;
    await runTest(wrapper);
    expect(wrapper.text()).not.toContain('Sample chart access verified');
    expect(wrapper.text()).toContain('Waiting for the Rancher repository index to become ready.');
  });

  it.each(['empty', 'unavailable'])('keeps verification incomplete when chart access is %s despite a healthy saved index', async (scenario) => {
    if (scenario === 'empty') vi.mocked(validateChartAccess).mockResolvedValue({ results: [] });
    else vi.mocked(validateChartAccess).mockRejectedValue({ status: 404 });
    const { wrapper, repo } = setup();
    Object.assign(repo.status, { indexConfigMapName: 'suse-index' });
    repo.status.conditions[1].status = 'True';
    repo.status.conditions[1].message = '';
    await runTest(wrapper);
    expect(wrapper.text()).toContain('Verification is incomplete.');
    expect(wrapper.text()).not.toContain('Sample chart access verified');
  });

  it('renders successful authentication and the failing Rancher repository independently', async () => {
    const { wrapper, store } = setup();
    expect(store.dispatch).not.toHaveBeenCalled();
    await runTest(wrapper);
    expect(wrapper.text()).toContain('Registry connection (current form)');
    expect(wrapper.text()).toContain('Registry responded (see chart access below)');
    expect(wrapper.text()).toContain('registry.suse.com (1121 ms)');
    expect(wrapper.text()).toContain('Chart repositories (saved settings)');
    expect(wrapper.text()).toContain('suse-ai-registry — Failed');
    expect(wrapper.text()).toContain('error 401: Unauthorized');
    expect(wrapper.get('a').attributes('href')).toBe('/c/local/apps/catalog.cattle.io.clusterrepo/suse-ai-registry');
    expect(store.dispatch).toHaveBeenCalledTimes(1);
    expect(wrapper.get('[role="status"]').attributes('aria-busy')).toBe('false');
  });

  it.each(['url', 'userSecretRef', 'tokenSecretRef', 'caBundleSecretRef'])('identifies unsaved %s inputs without replacing the saved repository status', async (field) => {
    const { wrapper } = setup();
    await wrapper.setProps({ configuration: {
      ...configuration, [field]: field === 'url' ? 'oci://mirror.example.com/charts' : { name: 'new-auth', key: 'new-key' },
    } });
    await runTest(wrapper);
    expect(wrapper.text()).toContain('The form differs from saved settings.');
    expect(wrapper.text()).toContain('Repository status and Refresh use the saved configuration');
    expect(wrapper.text()).toContain(SUSE_REGISTRY_REPO_URL);
    expect(wrapper.text()).toContain('error 401: Unauthorized');
  });

  it('invalidates the authentication result when an input is edited after Test', async () => {
    const { wrapper } = setup();
    await runTest(wrapper);
    await wrapper.setProps({ configuration: { ...configuration, tokenSecretRef: { name: 'different', key: 'token' } } });
    expect(wrapper.text()).toContain('The form changed since this test.');
    expect(wrapper.text()).not.toContain('Registry responded');
    expect(wrapper.text()).toContain('error 401: Unauthorized');
    await runTest(wrapper);
    expect(wrapper.text()).not.toContain('The form changed since this test.');
    expect(wrapper.text()).toContain('Registry responded');
  });

  it('does not attach an in-flight authentication success to edited inputs', async () => {
    let complete!: (value: { results: typeof authenticated[] }) => void;
    vi.mocked(validateCredentials).mockReturnValue(new Promise(resolve => { complete = resolve; }));
    const { wrapper } = setup();
    await wrapper.get('button').trigger('click');
    expect(wrapper.get('button').attributes('disabled')).toBeDefined();
    await wrapper.setProps({ configuration: { ...configuration, url: 'oci://different.example.com/charts' } });
    complete({ results: [authenticated] });
    await flushPromises();
    expect(wrapper.text()).toContain('The form changed since this test.');
    expect(wrapper.text()).not.toContain('Registry responded');
  });

  it('shows read permission errors instead of a false Missing status', async () => {
    const { wrapper, store } = setup();
    store.dispatch.mockRejectedValue({ _status: 403, message: 'Forbidden' });
    await runTest(wrapper);
    expect(wrapper.text()).toContain('Could not read Rancher chart repositories: Forbidden');
    expect(wrapper.text()).toContain('Registry responded');
    expect(wrapper.text()).not.toContain('Missing');
    expect(wrapper.findAll('button')).toHaveLength(1);
  });

  it('shows a missing repository with a link to Rancher Repositories, without Refresh', async () => {
    const { wrapper, store } = setup();
    store.dispatch.mockResolvedValue({ items: [] });
    await runTest(wrapper);
    expect(wrapper.text()).toContain('suse-ai-registry — Missing');
    expect(wrapper.get('a').attributes('href')).toBe('/c/local/apps/catalog.cattle.io.clusterrepo');
    expect(wrapper.findAll('button')).toHaveLength(1);
  });

  it('discards in-flight results when the component is replaced after Apply or navigation', async () => {
    let complete!: (value: { results: typeof authenticated[] }) => void;
    vi.mocked(validateCredentials).mockReturnValue(new Promise(resolve => { complete = resolve; }));
    const { wrapper } = setup();
    await wrapper.get('button').trigger('click');
    const state = wrapper.vm.$data as { result: unknown };
    wrapper.unmount();
    mounted.splice(mounted.indexOf(wrapper), 1);
    complete({ results: [authenticated] });
    await flushPromises();
    expect(state.result).toBeNull();
  });

  it('can inspect repositories with no credential references configured', async () => {
    const { wrapper, store } = setup();
    await wrapper.setProps({ configuration: {} });
    vi.mocked(validateCredentials).mockResolvedValue({ results: [{ target: 'suseRegistry', status: 'skipped', message: 'No credentials' }] });
    await runTest(wrapper);
    expect(wrapper.text()).toContain('Authentication not tested');
    expect(wrapper.text()).toContain('error 401: Unauthorized');
    expect(store.dispatch).toHaveBeenCalledTimes(1);
  });

  it('renders Rancher errors as text, never as HTML', async () => {
    const { wrapper, repo } = setup();
    repo.status.conditions[1].message = '<img src=x onerror="alert(1)">';
    await runTest(wrapper);
    expect(wrapper.text()).toContain(repo.status.conditions[1].message);
    expect(wrapper.find('img').exists()).toBe(false);
  });

  it('refreshes only after an explicit click and does not call acceptance Ready', async () => {
    const { wrapper, store } = setup();
    await runTest(wrapper);
    expect(store.dispatch.mock.calls.every(([, request]) => !request.method)).toBe(true);
    await wrapper.get('[aria-label="Refresh saved repository suse-ai-registry"]').trigger('click');
    await flushPromises();
    expect(store.dispatch.mock.calls.filter(([, request]) => request.method === 'PATCH')).toHaveLength(1);
    expect(wrapper.text()).toContain('suse-ai-registry — Pending');
    expect(wrapper.text()).toContain('Refresh requested for the saved repository.');
    expect(wrapper.text()).toContain('Test again to check its status.');
    expect(wrapper.text()).not.toContain('error 401: Unauthorized');
    expect(wrapper.text()).toContain('Registry responded');
    expect(validateCredentials).toHaveBeenCalledTimes(1);
  });

  it('preserves a refresh error and the repository result, and allows retry', async () => {
    const { wrapper, store } = setup();
    await runTest(wrapper);
    store.dispatch.mockRejectedValue({ message: 'Forbidden: cannot patch clusterrepos' });
    await wrapper.get('[aria-label="Refresh saved repository suse-ai-registry"]').trigger('click');
    await flushPromises();
    expect(wrapper.text()).toContain('Could not request repository refresh: suse-ai-registry: Forbidden');
    expect(wrapper.text()).toContain('error 401: Unauthorized');
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined();
  });
});
