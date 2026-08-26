import { describe, it, expect, vi } from 'vitest';

// fetchSuseAiApps / fetchNvidiaApps (added in later tasks) call getClusterContext
// via fetchAppsFromRepositoryResult. Mock it here so the whole file shares one setup.
vi.mock('../../utils/cluster-operations', () => ({
  getClusterContext: vi.fn(async (_store: any, { repoName }: any) => ({ baseApi: 'https://base', repoName })),
}));

vi.mock('../../utils/operator-api', () => ({
  getSettings: vi.fn(async () => null),
  getRegistryCredentials: vi.fn(async () => ({})),
}));

import { getRegistryCredentials } from '../../utils/operator-api';

import {
  fetchManagedRepos,
  fetchSuseAiApps,
  fetchNvidiaApps,
  resolveInstallRepoName,
  isManagedRepoName,
  CLUSTERREPOS_URL,
  NVIDIA_TEAM_REPO_LABEL,
  MANAGED_REPO_LABEL,
} from '../app-collection';

type RawRepo = {
  metadata: { name: string; labels?: Record<string, string> };
  spec: { url?: string; gitRepo?: string; enabled?: boolean };
  status?: { conditions?: Array<{ type: string; status: string; message?: string }>; indexConfigMapName?: string };
};

// A truly-ready repo: a download condition True AND the index ConfigMap written.
// Readiness is gated on indexConfigMapName because apps load via ?link=index,
// which resolves through it.
function ready(): RawRepo['status'] {
  return { conditions: [{ type: 'Downloaded', status: 'True' }], indexConfigMapName: 'idx' };
}
// The transient creation-window race: download condition already True, but the
// index ConfigMap not written yet. Must be treated as not-ready so the doomed
// ?link=index fetch (which would fail `configmaps "" not found`) is never made.
function readyNoIndex(): RawRepo['status'] {
  return { conditions: [{ type: 'Downloaded', status: 'True' }] };
}
function notReady(message: string): RawRepo['status'] {
  return { conditions: [{ type: 'Downloaded', status: 'False', message }] };
}
// A stale index: the ConfigMap from a PREVIOUS successful download is still
// present, but the most recent download flipped to False (e.g. spec.url was
// changed to a broken endpoint). Serving that index would list apps from a
// source the cluster is no longer configured to use, so it must be not-ready.
function staleIndex(message: string): RawRepo['status'] {
  return {
    conditions: [
      { type: 'Downloaded', status: 'True' },
      { type: 'OCIDownloaded', status: 'False', message },
    ],
    indexConfigMapName: 'idx-old',
  };
}

// A $store whose dispatch returns `repos` for the clusterrepos list, and per-repo
// index entries for `?link=index` requests. Index is keyed by repo name.
function makeStore(repos: RawRepo[], indexByRepo: Record<string, any> = {}) {
  return {
    dispatch: vi.fn(async (_action: string, { url }: { url: string }) => {
      if (url === CLUSTERREPOS_URL) return { data: { items: repos } };
      const m = url.match(/clusterrepos\/([^?]+)\?link=index/);
      if (m) {
        const name = decodeURIComponent(m[1]);
        return { data: { entries: indexByRepo[name] || {} } };
      }
      throw new Error(`unexpected url ${url}`);
    }),
  };
}

const MANAGED = MANAGED_REPO_LABEL;

describe('fetchManagedRepos', () => {
  it('includes only provenance-labeled repos, classified by library', async () => {
    const store = makeStore([
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac' }, status: ready() },
      { metadata: { name: 'suse-ai-registry', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://sr' }, status: ready() },
      { metadata: { name: 'nvidia', labels: { [MANAGED]: 'true' } }, spec: { url: 'https://helm.ngc.nvidia.com/nvidia' }, status: ready() },
      { metadata: { name: 'nvidia-blueprints', labels: { [MANAGED]: 'true' } }, spec: { url: 'https://helm.ngc.nvidia.com/nvidia/blueprint' }, status: ready() },
      { metadata: { name: 'nvidia-omniverse', labels: { [MANAGED]: 'true', [NVIDIA_TEAM_REPO_LABEL]: 'true' } }, spec: { url: 'https://helm.ngc.nvidia.com/nvidia/omniverse' }, status: ready() },
    ]);
    const managed = await fetchManagedRepos(store);
    expect(managed.map(r => [r.name, r.library])).toEqual([
      ['application-collection', 'suse-ai'],
      ['suse-ai-registry', 'suse-ai'],
      ['nvidia', 'nvidia'],
      ['nvidia-blueprints', 'nvidia'],
      ['nvidia-omniverse', 'nvidia'],
    ]);
  });

  it('excludes a canonical-named repo that lacks the provenance label', async () => {
    const store = makeStore([
      { metadata: { name: 'application-collection' }, spec: { url: 'oci://ac' }, status: ready() }, // no label
      { metadata: { name: 'suse-ai-registry', labels: { [MANAGED]: 'false' } }, spec: { url: 'oci://sr' }, status: ready() }, // exact-match gate: 'false' is excluded
      { metadata: { name: 'admin-ngc', labels: { [MANAGED]: 'true' } }, spec: { url: 'https://helm.ngc.nvidia.com/other' }, status: ready() }, // labeled but unclassifiable
    ]);
    const managed = await fetchManagedRepos(store);
    expect(managed).toEqual([]);
  });

  it('is not fooled by a prototype-named repo', async () => {
    const store = makeStore([
      { metadata: { name: 'constructor', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://x' }, status: ready() },
    ]);
    const managed = await fetchManagedRepos(store);
    expect(managed).toEqual([]);
  });

  it('excludes disabled repos and reports readiness/message', async () => {
    const store = makeStore([
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac', enabled: false }, status: ready() },
      { metadata: { name: 'nvidia', labels: { [MANAGED]: 'true' } }, spec: { url: 'https://helm.ngc.nvidia.com/nvidia' }, status: notReady('index download failed') },
    ]);
    const managed = await fetchManagedRepos(store);
    expect(managed).toEqual([
      { name: 'nvidia', url: 'https://helm.ngc.nvidia.com/nvidia', library: 'nvidia', ready: false, message: 'index download failed' },
    ]);
  });

  it('treats a stale index as not-ready when the latest download failed', async () => {
    // indexConfigMapName present (previous success) but OCIDownloaded=False now.
    const store = makeStore([
      { metadata: { name: 'nvidia', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://mirror' }, status: staleIndex('tls: failed to verify certificate') },
    ]);
    const managed = await fetchManagedRepos(store);
    expect(managed).toEqual([
      { name: 'nvidia', url: 'oci://mirror', library: 'nvidia', ready: false, message: 'tls: failed to verify certificate' },
    ]);
  });

  it('rethrows when the clusterrepos list request fails (not silently empty)', async () => {
    // A failed list (operator/Rancher unreachable, RBAC) must not look like
    // "no managed repos" — callers surface it as an error instead.
    const store = {
      dispatch: vi.fn(async () => { throw new Error('boom'); }),
    };
    await expect(fetchManagedRepos(store)).rejects.toThrow('boom');
  });
});

describe('fetchSuseAiApps', () => {
  const acEntries = { grafana: [{ name: 'grafana', created: '2026-01-02T00:00:00Z' }] };
  const srEntries = {
    grafana: [{ name: 'grafana', created: '2026-01-01T00:00:00Z' }], // dup: AC must win
    milvus:  [{ name: 'milvus',  created: '2026-01-01T00:00:00Z' }],
  };

  // The operator API serializes an absent registry section as {} (value struct,
  // ineffective omitempty), so section presence is not a usable "active" signal.
  // The existence of a managed repo (which the operator creates only when creds
  // are configured, and prunes otherwise) is the ground truth — same as NVIDIA.
  it('returns [] when no managed repo exists (empty {} section)', async () => {
    const store = makeStore([]);
    const { apps, failedRepos } = await fetchSuseAiApps(store, { spec: { applicationCollection: {}, suseRegistry: {} } });
    expect(apps).toEqual([]);
    expect(failedRepos).toEqual([]);
  });

  it('loads apps from an existing managed repo regardless of the (always-{}) settings section', async () => {
    const store = makeStore([
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac' }, status: ready() },
    ], { 'application-collection': acEntries });
    const { apps } = await fetchSuseAiApps(store, { spec: { applicationCollection: {} } });
    expect(apps.map(a => a.slug_name)).toEqual(['grafana']);
  });

  it('loads managed repos by fixed name and ignores a same-URL unmanaged repo', async () => {
    const store = makeStore([
      // Listed suse-ai-registry FIRST so the AC-wins dedup can only come from the
      // name-precedence sort, not from list order.
      { metadata: { name: 'suse-ai-registry', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://sr' }, status: ready() },
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac' }, status: ready() },
      // Admin-created repo at the same AC URL but a different name — must be ignored.
      { metadata: { name: 'admin-copy' }, spec: { url: 'oci://ac' }, status: ready() },
    ], { 'application-collection': acEntries, 'suse-ai-registry': srEntries });

    const { apps } = await fetchSuseAiApps(store, {
      spec: { applicationCollection: { tokenSecretRef: {} }, suseRegistry: { tokenSecretRef: {} } },
    });

    // grafana (AC precedence, tagged with AC url) + milvus (from SR), sorted by name.
    expect(apps.map(a => a.slug_name)).toEqual(['grafana', 'milvus']);
    const grafana = apps.find(a => a.slug_name === 'grafana')!;
    expect(grafana.repository_url).toBe('oci://ac');
    expect(grafana.library).toBe('suse-ai');
    // The admin repo was never fetched (only managed repo names are requested).
    const requested = store.dispatch.mock.calls.map(c => c[1].url).join('\n');
    expect(requested).not.toContain('admin-copy');
  });

  it('reports a not-ready managed SUSE repo via failedRepos', async () => {
    const store = makeStore([
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac' }, status: notReady('boom') },
    ]);
    const { apps, failedRepos } = await fetchSuseAiApps(store, { spec: {} });
    expect(apps).toEqual([]);
    expect(failedRepos).toEqual([
      { url: 'oci://ac', reason: 'not-ready', message: 'boom' },
    ]);
  });

  it('treats a downloaded-but-not-yet-indexed repo as not-ready (no configmaps "" fetch)', async () => {
    // Creation-window race: Downloaded=True but indexConfigMapName not written yet.
    // Must be reported not-ready rather than attempting ?link=index (which would
    // fail `configmaps "" not found` and surface as a scary fetch-failed banner).
    const store = makeStore([
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac' }, status: readyNoIndex() },
    ]);
    const { apps, failedRepos } = await fetchSuseAiApps(store, { spec: {} });
    expect(apps).toEqual([]);
    expect(failedRepos).toEqual([
      { url: 'oci://ac', reason: 'not-ready', message: undefined },
    ]);
    // The index endpoint must never be hit for a not-yet-indexed repo.
    const requested = store.dispatch.mock.calls.map(c => c[1].url).join('\n');
    expect(requested).not.toContain('link=index');
  });
});

describe('fetchNvidiaApps', () => {
  const nvEntries = { nim: [{ name: 'nim', created: '2026-01-01T00:00:00Z' }] };

  // The operator API serializes an absent registry section as {} (value struct,
  // ineffective omitempty), so section presence is not a usable "active" signal.
  // The existence of a managed repo (which the operator creates only when creds
  // are configured, and prunes otherwise) is the ground truth.
  it('stays silent when nvidia is unconfigured and no managed repo exists', async () => {
    const store = makeStore([]);
    const { apps, failedRepos } = await fetchNvidiaApps(store, { spec: {} });
    expect(apps).toEqual([]);
    expect(failedRepos).toEqual([]);
  });

  it('loads apps from an existing managed repo regardless of the (always-{}) settings section', async () => {
    const store = makeStore([
      { metadata: { name: 'nvidia', labels: { [MANAGED]: 'true' } }, spec: { url: 'https://helm.ngc.nvidia.com/nvidia' }, status: ready() },
    ], { nvidia: nvEntries });
    const { apps, failedRepos } = await fetchNvidiaApps(store, { spec: { nvidia: {} } });
    expect(apps.map(a => a.slug_name)).toEqual(['nim']);
    expect(failedRepos).toEqual([]);
  });

  it('excludes an unmanaged NGC-host repo (regression)', async () => {
    const store = makeStore([
      { metadata: { name: 'nvidia', labels: { [MANAGED]: 'true' } }, spec: { url: 'https://helm.ngc.nvidia.com/nvidia' }, status: ready() },
      { metadata: { name: 'admin-ngc' }, spec: { url: 'https://helm.ngc.nvidia.com/other' }, status: ready() },
    ], { nvidia: nvEntries, 'admin-ngc': { evil: [{ name: 'evil', created: '2026-01-01T00:00:00Z' }] } });

    const { apps, failedRepos } = await fetchNvidiaApps(store, { spec: {} });
    expect(apps.map(a => a.slug_name)).toEqual(['nim']);
    expect(failedRepos).toEqual([]);
  });

  it('reports a not-ready managed repo via failedRepos', async () => {
    const store = makeStore([
      { metadata: { name: 'nvidia', labels: { [MANAGED]: 'true' } }, spec: { url: 'https://helm.ngc.nvidia.com/nvidia' }, status: notReady('boom') },
    ]);
    const { apps, failedRepos } = await fetchNvidiaApps(store, { spec: {} });
    expect(apps).toEqual([]);
    expect(failedRepos).toEqual([
      { url: 'https://helm.ngc.nvidia.com/nvidia', reason: 'not-ready', message: 'boom' },
    ]);
  });

  it('reports "not created yet" when nvidia creds are effectively configured but no managed repo exists', async () => {
    (getRegistryCredentials as any).mockResolvedValueOnce({ nvidia: { username: '$oauthtoken' } });
    const store = makeStore([]);
    const { apps, failedRepos } = await fetchNvidiaApps(store, { spec: {} });
    expect(apps).toEqual([]);
    expect(failedRepos).toEqual([
      { url: 'https://helm.ngc.nvidia.com/nvidia', reason: 'not-ready', message: 'repository not created yet' },
    ]);
  });

  it('reports "not created yet" for an air-gap endpoint with no credentials', async () => {
    // The operator creates the NVIDIA mirror WITHOUT credentials when
    // registryEndpoints.nvidia is set, so a creds-only gate would stay silent
    // while the mirror is pending. Banner must fire on the endpoint alone, and at
    // the endpoint URL.
    (getRegistryCredentials as any).mockResolvedValueOnce({});
    const store = makeStore([]);
    const { apps, failedRepos } = await fetchNvidiaApps(store, {
      spec: { registryEndpoints: { nvidia: 'oci://mirror.local/nvidia' } },
    });
    expect(apps).toEqual([]);
    expect(failedRepos).toEqual([
      { url: 'oci://mirror.local/nvidia', reason: 'not-ready', message: 'repository not created yet' },
    ]);
  });
});

describe('install-path repo resolution', () => {
  const acEntries = { grafana: [{ name: 'grafana', created: '2026-01-02T00:00:00Z' }] };

  it('attaches the authoritative managed repo name to discovered apps', async () => {
    const store = makeStore([
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac' }, status: ready() },
    ], { 'application-collection': acEntries });
    const { apps } = await fetchSuseAiApps(store, { spec: {} });
    const grafana = apps.find(a => a.slug_name === 'grafana')!;
    expect(grafana.repository_name).toBe('application-collection');
  });

  it('resolveInstallRepoName prefers the attached name', async () => {
    const store = makeStore([]); // no list request should be needed
    const name = await resolveInstallRepoName(store, { repository_name: 'nvidia', repository_url: 'oci://whatever' });
    expect(name).toBe('nvidia');
  });

  it('resolveInstallRepoName resolves URL only within the managed set (ignores squatter)', async () => {
    const store = makeStore([
      { metadata: { name: 'admin-copy' }, spec: { url: 'oci://ac' }, status: ready() },                               // unmanaged, sorts first
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac' }, status: ready() },
    ]);
    const name = await resolveInstallRepoName(store, { repository_url: 'oci://ac' });
    expect(name).toBe('application-collection');
  });
});

describe('managed-repo threading (list once)', () => {
  const acEntries = { grafana: [{ name: 'grafana', created: '2026-01-02T00:00:00Z' }] };
  const nvEntries = { nim: [{ name: 'nim', created: '2026-01-01T00:00:00Z' }] };

  // The caller lists managed repos once and threads the same set into both fetchers.
  // Given that set, neither fetcher may re-request the clusterrepos list endpoint.
  it('uses a supplied managed set without re-listing clusterrepos', async () => {
    const store = makeStore([], { 'application-collection': acEntries, nvidia: nvEntries });
    const managed = [
      { name: 'application-collection', url: 'oci://ac', library: 'suse-ai' as const, ready: true },
      { name: 'nvidia', url: 'https://helm.ngc.nvidia.com/nvidia', library: 'nvidia' as const, ready: true },
    ];

    const suse = await fetchSuseAiApps(store, { spec: {} }, managed);
    const nv = await fetchNvidiaApps(store, { spec: {} }, managed);

    expect(suse.apps.map(a => a.slug_name)).toEqual(['grafana']);
    expect(nv.apps.map(a => a.slug_name)).toEqual(['nim']);
    // Neither fetcher hit the clusterrepos list endpoint — the set was threaded in.
    const listCalls = store.dispatch.mock.calls.filter((c: any) => c[1].url === CLUSTERREPOS_URL);
    expect(listCalls).toHaveLength(0);
  });
});

describe('isManagedRepoName (untrusted ?repo= guard)', () => {
  it('returns true for a repo carrying the provenance label (value exactly "true")', async () => {
    const store = makeStore([
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac' }, status: ready() },
    ]);
    expect(await isManagedRepoName(store, 'application-collection')).toBe(true);
  });

  it('returns false for an unlabeled repo — even at a canonical name', async () => {
    const store = makeStore([
      { metadata: { name: 'application-collection' }, spec: { url: 'oci://ac' }, status: ready() },
    ]);
    expect(await isManagedRepoName(store, 'application-collection')).toBe(false);
  });

  it('returns false when the label value is not exactly "true"', async () => {
    const store = makeStore([
      { metadata: { name: 'rogue', labels: { [MANAGED]: 'TRUE' } }, spec: { url: 'oci://x' }, status: ready() },
    ]);
    expect(await isManagedRepoName(store, 'rogue')).toBe(false);
  });

  it('returns false for a name absent from the cluster', async () => {
    const store = makeStore([
      { metadata: { name: 'application-collection', labels: { [MANAGED]: 'true' } }, spec: { url: 'oci://ac' }, status: ready() },
    ]);
    expect(await isManagedRepoName(store, 'does-not-exist')).toBe(false);
  });

  it('returns false for an empty name without listing', async () => {
    const store = makeStore([]);
    expect(await isManagedRepoName(store, '')).toBe(false);
    expect(store.dispatch).not.toHaveBeenCalled();
  });

  it('fails CLOSED — returns false when the clusterrepos list throws', async () => {
    const store = { dispatch: vi.fn(async () => { throw new Error('boom'); }) };
    expect(await isManagedRepoName(store, 'application-collection')).toBe(false);
  });
});

describe('cross-language label constants (drift pins)', () => {
  // These literals are ALSO defined in Go — operator/internal/credentials/credentials.go
  // (ManagedRepoLabel / TeamRepoLabel). They are tied only by convention, so pin the
  // exact strings on this side: a rename here shows up as a red diff, and the Go side
  // has a matching pin (settings_label_internal_test.go). Change BOTH together.
  it('MANAGED_REPO_LABEL matches the Go ManagedRepoLabel literal', () => {
    expect(MANAGED_REPO_LABEL).toBe('ai-factory.suse.com/managed-repo');
  });
  it('NVIDIA_TEAM_REPO_LABEL matches the Go TeamRepoLabel literal', () => {
    expect(NVIDIA_TEAM_REPO_LABEL).toBe('ai-factory.suse.com/nvidia-team-repo');
  });
});
