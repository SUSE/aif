import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { getSettings, validateCredentials } from '../../utils/operator-api';
import { CLUSTERREPOS_URL, MANAGED_REPO_LABEL, NVIDIA_TEAM_REPO_LABEL } from '../app-collection';
import { APP_COLLECTION_REPO_URL, SUSE_REGISTRY_REPO_URL, NVIDIA_REPO_URL, NVIDIA_BLUEPRINT_REPO_URL } from '../registry-endpoints';
import {
  checkRegistryConnection, refreshChartRepository, registryConfigurationFingerprint,
} from '../registry-connection';
import type { RegistryConfiguration, RegistryTarget } from '../registry-connection';

vi.mock('../../utils/operator-api', () => ({
  getSettings: vi.fn(), validateCredentials: vi.fn(),
}));

const TARGET = 'suseRegistry';
const NAME = 'suse-ai-registry';
const URL = `${CLUSTERREPOS_URL.split('?')[0]}/${NAME}`;
const CONFIG: RegistryConfiguration = {
  url: SUSE_REGISTRY_REPO_URL,
  userSecretRef: { name: 'registry-auth', key: 'username' },
  tokenSecretRef: { name: 'registry-auth', key: 'password' },
  caBundleSecretRef: null,
};
const AUTHENTICATED = {
  target: TARGET, status: 'ok' as const, host: 'registry.suse.com', latencyMs: 1121, message: '',
};

function repo(name = NAME, url = SUSE_REGISTRY_REPO_URL) {
  const labels: Record<string, string> = { [MANAGED_REPO_LABEL]: 'true' };
  return {
    metadata: { name, labels, generation: 2, resourceVersion: '42' },
    spec: { url, enabled: true, forceUpdate: '' },
    status: {
      indexConfigMapName: 'repository-index', observedGeneration: 2,
      conditions: [{ type: 'OCIDownloaded', status: 'True', message: '' }],
    },
  };
}

function storeWith(items = [repo()]) {
  return { dispatch: vi.fn().mockResolvedValue({ data: { items } }) };
}

beforeEach(() => {
  vi.mocked(getSettings).mockReset().mockResolvedValue({ spec: { [TARGET]: CONFIG } });
  vi.mocked(validateCredentials).mockReset().mockResolvedValue({ results: [AUTHENTICATED] });
});
afterEach(() => vi.useRealTimers());

describe('independent registry checks', () => {
  it.each(['OCIDownloaded', 'Downloaded', 'FollowerDownloaded'])('preserves authentication success alongside a %s failure', async (type) => {
    const failed = repo();
    failed.status.conditions = [{ type, status: 'False', message: '401 Unauthorized: chart access denied' }];
    const store = storeWith([failed]);
    const result = await checkRegistryConnection(store, TARGET, CONFIG);

    expect(result.authentication).toEqual(AUTHENTICATED);
    expect(result.chartRepositories.repositories).toEqual([expect.objectContaining({
      name: NAME, state: 'failed', message: '401 Unauthorized: chart access denied', canRefresh: true,
      link: `/c/local/apps/catalog.cattle.io.clusterrepo/${NAME}`,
    })]);
    expect(validateCredentials).toHaveBeenCalledWith({ targets: [TARGET], overrides: { [TARGET]: CONFIG } });
    // Test has no mutation: one uncached management-cluster list, no index or Secret reads.
    expect(store.dispatch).toHaveBeenCalledTimes(1);
    expect(store.dispatch).toHaveBeenCalledWith('rancher/request', {
      url: CLUSTERREPOS_URL, timeout: 8000,
    });
  });

  it('keeps a healthy chart result when the authentication endpoint fails', async () => {
    vi.mocked(validateCredentials).mockRejectedValue(new Error('Operator unavailable'));
    const result = await checkRegistryConnection(storeWith(), TARGET, CONFIG);
    expect(result.authentication).toMatchObject({ status: 'error', message: 'Operator unavailable' });
    expect(result.chartRepositories.repositories[0].state).toBe('ready');
  });

  it('does not describe a forbidden list as missing repositories', async () => {
    const store = { dispatch: vi.fn().mockRejectedValue({ _status: 403, message: 'Forbidden: clusterrepos' }) };
    const result = await checkRegistryConnection(store, TARGET, CONFIG);
    expect(result.authentication.status).toBe('ok');
    expect(result.chartRepositories).toMatchObject({ error: 'Forbidden: clusterrepos', repositories: [] });
  });

  it('does not describe an invalid response as missing repositories', async () => {
    const store = { dispatch: vi.fn().mockResolvedValue({ data: { message: 'unexpected' } }) };
    expect((await checkRegistryConnection(store, TARGET, CONFIG)).chartRepositories.error).toBe('Invalid ClusterRepo list response.');
  });

  it('reports a missing managed repository without adopting an unrelated repo at the same URL', async () => {
    const result = await checkRegistryConnection(storeWith([repo('some-other-repository')]), TARGET, CONFIG);
    expect(result.chartRepositories.repositories).toEqual([expect.objectContaining({
      name: NAME, state: 'missing', canRefresh: false, link: '/c/local/apps/catalog.cattle.io.clusterrepo',
    })]);
  });

  it.each(['unmanaged', 'disabled'])('reports a %s repository without offering Refresh', async (kind) => {
    const item = repo();
    if (kind === 'unmanaged') item.metadata.labels = {};
    else item.spec.enabled = false;
    const result = await checkRegistryConnection(storeWith([item]), TARGET, CONFIG);
    expect(result.chartRepositories.repositories[0]).toMatchObject({ state: 'failed', reason: kind, canRefresh: false });
  });

  it.each(['missing index', 'unknown condition', 'new generation', 'changed endpoint'])('reports pending for a %s, not a stale green index', async (kind) => {
    const item = repo();
    if (kind === 'missing index') item.status.indexConfigMapName = '';
    if (kind === 'unknown condition') item.status.conditions[0].status = 'Unknown';
    if (kind === 'new generation') item.metadata.generation++;
    if (kind === 'changed endpoint') item.spec.url = 'oci://old.example.com/charts';
    expect((await checkRegistryConnection(storeWith([item]), TARGET, CONFIG)).chartRepositories.repositories[0].state).toBe('pending');
  });

  it('does not claim saved credentials were applied before the Settings controller catches up', async () => {
    vi.mocked(getSettings).mockResolvedValue({
      metadata: { generation: 3 }, status: { observedGeneration: 2 }, spec: { [TARGET]: CONFIG },
    });
    const result = await checkRegistryConnection(storeWith(), TARGET, CONFIG);
    expect(result.chartRepositories.settingsPending).toBe(true);
    expect(result.chartRepositories.repositories[0]).toMatchObject({ state: 'pending', reason: 'reconciling' });
  });

  it.each(['wrapped list', 'direct list', 'wrapped array', 'direct array'])('accepts a %s response', async (kind) => {
    const items = [repo()];
    const body = kind.endsWith('list') ? { items } : items;
    const store = { dispatch: vi.fn().mockResolvedValue(kind.startsWith('wrapped') ? { data: body } : body) };
    expect((await checkRegistryConnection(store, TARGET, CONFIG)).chartRepositories.repositories[0].state).toBe('ready');
  });

  it('retains repository results when saved settings cannot be read', async () => {
    vi.mocked(getSettings).mockRejectedValue(new Error('Settings forbidden'));
    const result = await checkRegistryConnection(storeWith(), TARGET, CONFIG);
    expect(result.chartRepositories.appliedConfiguration).toBeUndefined();
    expect(result.chartRepositories.settingsError).toBe('Settings forbidden');
    expect(result.chartRepositories.repositories[0].state).toBe('ready');
  });

  it('can still probe explicit credentials and CA when saved settings cannot be read', async () => {
    vi.mocked(getSettings).mockRejectedValue(new Error('Settings forbidden'));
    const result = await checkRegistryConnection(storeWith(), TARGET, {
      ...CONFIG, caBundleSecretRef: { name: 'explicit-ca', key: 'ca.crt' },
    });
    expect(result.authentication.status).toBe('ok');
    expect(result.chartRepositories.settingsError).toBe('Settings forbidden');
  });

  it('distinguishes absent settings from an unreadable configuration', async () => {
    vi.mocked(getSettings).mockRejectedValue({ status: 404 });
    const result = await checkRegistryConnection(storeWith([]), TARGET, CONFIG);
    expect(result.chartRepositories.appliedConfiguration).toBeNull();
    expect(result.chartRepositories.settingsError).toBeUndefined();
  });

  it('bounds the saved-settings read so a stalled operator cannot leave Test busy forever', async () => {
    vi.useFakeTimers();
    vi.mocked(getSettings).mockImplementation(signal => new Promise((_resolve, reject) => {
      signal?.addEventListener('abort', () => reject(new Error('Settings request timed out')));
    }));
    const checking = checkRegistryConnection(storeWith(), TARGET, CONFIG);
    await vi.advanceTimersByTimeAsync(8000);
    const result = await checking;
    expect(result.chartRepositories.settingsError).toBe('Settings request timed out');
    expect(result.chartRepositories.repositories[0].state).toBe('ready');
    expect(result.authentication.status).toBe('skipped');
    expect(validateCredentials).not.toHaveBeenCalled();
  });

  it('re-reads saved settings on each Test, keeping an unsaved probe independent', async () => {
    const unsaved = { ...CONFIG, url: 'oci://mirror.example.com/charts' };
    const first = await checkRegistryConnection(storeWith(), TARGET, unsaved);
    expect(first.chartRepositories.appliedConfiguration?.url).toBe(SUSE_REGISTRY_REPO_URL);
    expect(first.chartRepositories.repositories[0].state).toBe('ready');
    vi.mocked(getSettings).mockResolvedValue({ spec: { [TARGET]: CONFIG, registryEndpoints: { [TARGET]: unsaved.url } } });
    const second = await checkRegistryConnection(storeWith(), TARGET, unsaved);
    expect(second.chartRepositories.appliedConfiguration?.url).toBe(unsaved.url);
    expect(second.chartRepositories.repositories[0].state).toBe('pending');
  });

  it('selects Application Collection by identity at a private mirror', async () => {
    const target = 'applicationCollection';
    const mirror = 'oci://mirror.example.com/ac';
    vi.mocked(getSettings).mockResolvedValue({ spec: { registryEndpoints: { [target]: mirror } } });
    const result = await checkRegistryConnection(storeWith([repo('application-collection', mirror), repo()]), target, { url: mirror });
    expect(result.chartRepositories.repositories).toEqual([expect.objectContaining({ name: 'application-collection', state: 'ready' })]);
  });

  it('checks both NVIDIA aliases and managed teams but not unrelated NGC repositories', async () => {
    const team = repo('nvidia-team-omniverse', 'https://helm.ngc.nvidia.com/nvidia/omniverse');
    team.metadata.labels[NVIDIA_TEAM_REPO_LABEL] = 'true';
    team.status.conditions = [{ type: 'Downloaded', status: 'False', message: 'no API version specified' }];
    const unrelated = repo('unmanaged-ngc', NVIDIA_REPO_URL);
    unrelated.metadata.labels = { [NVIDIA_TEAM_REPO_LABEL]: 'true' };
    const result = await checkRegistryConnection(storeWith([
      repo('nvidia', NVIDIA_REPO_URL), repo('nvidia-blueprints', NVIDIA_BLUEPRINT_REPO_URL), team, unrelated, repo(),
    ]), 'nvidia', {});
    expect(result.chartRepositories.repositories.map(r => [r.name, r.state])).toEqual([
      ['nvidia', 'ready'], ['nvidia-blueprints', 'ready'], ['nvidia-team-omniverse', 'failed'],
    ]);
  });

  it('checks both NVIDIA mirror aliases even without authentication references', async () => {
    const mirror = 'oci://mirror.example.com/nvidia/charts';
    vi.mocked(getSettings).mockResolvedValue({ spec: { registryEndpoints: { nvidia: mirror } } });
    vi.mocked(validateCredentials).mockResolvedValue({ results: [{ target: 'nvidia', status: 'skipped', message: 'No credentials' }] });
    const result = await checkRegistryConnection(storeWith([repo('nvidia', mirror)]), 'nvidia', { url: mirror });
    expect(result.authentication.status).toBe('skipped');
    expect(result.chartRepositories.repositories.map(r => [r.name, r.state])).toEqual([
      ['nvidia', 'ready'], ['nvidia-blueprints', 'missing'],
    ]);
  });
});

describe('cleared form inputs do not silently authenticate saved configuration', () => {
  it.each(['userSecretRef', 'tokenSecretRef'] as const)('skips authentication with a cleared %s but still checks repositories', async (field) => {
    const result = await checkRegistryConnection(storeWith(), TARGET, { ...CONFIG, [field]: null });
    expect(result.authentication.status).toBe('skipped');
    expect(validateCredentials).not.toHaveBeenCalled();
    expect(result.chartRepositories.repositories[0].state).toBe('ready');
  });

  it('asks to apply a removed CA, which the probe API cannot override with null', async () => {
    vi.mocked(getSettings).mockResolvedValue({ spec: { [TARGET]: { ...CONFIG, caBundleSecretRef: { name: 'saved-ca', key: 'ca.crt' } } } });
    const result = await checkRegistryConnection(storeWith(), TARGET, CONFIG);
    expect(result.authentication).toMatchObject({ status: 'skipped', message: 'Apply the removal of the saved CA bundle before testing authentication.' });
    expect(validateCredentials).not.toHaveBeenCalled();
  });

  it.each([
    ['applicationCollection', APP_COLLECTION_REPO_URL], ['suseRegistry', SUSE_REGISTRY_REPO_URL], ['nvidia', 'https://nvcr.io'],
  ] as [RegistryTarget, string][])('tests the connected default when the %s mirror is cleared', async (target, url) => {
    vi.mocked(getSettings).mockResolvedValue({ spec: { registryEndpoints: { [target]: 'oci://saved.example.com/charts' } } });
    await checkRegistryConnection(storeWith(), target, { ...CONFIG, url: '' });
    expect(validateCredentials).toHaveBeenCalledWith({ targets: [target], overrides: { [target]: { ...CONFIG, url } } });
  });
});

describe('configuration comparison', () => {
  it.each(['url', 'userSecretRef', 'tokenSecretRef', 'caBundleSecretRef'] as const)('detects edits to %s', (field) => {
    const changed = { ...CONFIG, [field]: field === 'url' ? 'oci://new.example.com/charts' : { name: 'new', key: 'new-key' } };
    expect(registryConfigurationFingerprint(TARGET, CONFIG)).not.toBe(registryConfigurationFingerprint(TARGET, changed));
  });

  it('compares Secret keys, not just names', () => {
    const changed = { ...CONFIG, tokenSecretRef: { ...CONFIG.tokenSecretRef!, key: 'different' } };
    expect(registryConfigurationFingerprint(TARGET, CONFIG)).not.toBe(registryConfigurationFingerprint(TARGET, changed));
  });

  it.each([
    ['applicationCollection', APP_COLLECTION_REPO_URL], ['suseRegistry', SUSE_REGISTRY_REPO_URL], ['nvidia', ''],
  ] as [RegistryTarget, string][])('normalizes implicit defaults and empty refs for %s', (target, url) => {
    expect(registryConfigurationFingerprint(target, {})).toBe(registryConfigurationFingerprint(target, {
      url, userSecretRef: null, tokenSecretRef: null, caBundleSecretRef: null,
    }));
  });
});

describe('explicit repository Refresh', () => {
  it('patches only forceUpdate with a concurrency guard, and reports pending, not ready', async () => {
    vi.useFakeTimers().setSystemTime(new Date('2026-09-04T12:00:00Z'));
    const item = repo();
    item.spec.forceUpdate = '2026-09-04T12:00:00Z';
    const store = { dispatch: vi.fn().mockResolvedValueOnce({ data: item }).mockResolvedValueOnce(item) };
    expect(await refreshChartRepository(store, TARGET, NAME)).toMatchObject({ state: 'pending', reason: 'refreshRequested' });
    expect(store.dispatch.mock.calls).toEqual([
      ['rancher/request', { url: URL, timeout: 8000 }],
      ['rancher/request', {
        url: URL, method: 'PATCH', headers: { 'Content-Type': 'application/merge-patch+json' }, timeout: 20000,
        data: { metadata: { resourceVersion: '42' }, spec: { forceUpdate: '2026-09-04T12:00:01Z' } },
      }],
    ]);
    expect(validateCredentials).not.toHaveBeenCalled();
    expect(getSettings).not.toHaveBeenCalled();
  });

  it.each(['unmanaged', 'other registry', 'disabled', 'replaced', 'missing resourceVersion'])('refuses to patch a repository that is %s', async (kind) => {
    const item = repo();
    if (kind === 'unmanaged') item.metadata.labels = {};
    if (kind === 'disabled') item.spec.enabled = false;
    if (kind === 'replaced') item.metadata.name = 'different';
    if (kind === 'missing resourceVersion') item.metadata.resourceVersion = '';
    const store = { dispatch: vi.fn().mockResolvedValue(item) };
    await expect(refreshChartRepository(store, kind === 'other registry' ? 'nvidia' : TARGET, NAME)).rejects.toThrow('Only an enabled');
    expect(store.dispatch).toHaveBeenCalledTimes(1);
  });

  it('preserves permission/concurrency errors rather than reporting a refresh was requested', async () => {
    const error = { _status: 409, message: 'Conflict' };
    const store = { dispatch: vi.fn().mockResolvedValueOnce(repo()).mockRejectedValueOnce(error) };
    await expect(refreshChartRepository(store, TARGET, NAME)).rejects.toBe(error);
  });

  it('does not let an NVIDIA team label override a canonical SUSE repository identity', async () => {
    const item = repo();
    item.metadata.labels[NVIDIA_TEAM_REPO_LABEL] = 'true';
    const store = { dispatch: vi.fn().mockResolvedValue(item) };
    await expect(refreshChartRepository(store, 'nvidia', NAME)).rejects.toThrow('Only an enabled');
    expect(store.dispatch).toHaveBeenCalledTimes(1);
  });

  it('a later Test remains pending until Rancher observes the refresh generation', async () => {
    const item = repo();
    const store = {
      dispatch: vi.fn(async (_action, request) => {
        if (request.method === 'PATCH') item.metadata.generation++;
        return request.url === CLUSTERREPOS_URL ? { items: [item] } : item;
      }),
    };
    await refreshChartRepository(store, TARGET, NAME);
    expect((await checkRegistryConnection(store, TARGET, CONFIG)).chartRepositories.repositories[0].state).toBe('pending');
    item.status.observedGeneration = item.metadata.generation;
    expect((await checkRegistryConnection(store, TARGET, CONFIG)).chartRepositories.repositories[0].state).toBe('ready');
  });
});
