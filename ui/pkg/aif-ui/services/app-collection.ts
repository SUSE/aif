import { getClusterContext } from '../utils/cluster-operations';
import { log as logger } from '../utils/logger';
import { getSettings } from '../utils/operator-api';
import { TIMEOUT_VALUES } from '../utils/constants';
import { fetchStaticCatalog } from './static-catalog';

// Canonical OCI registry URLs for the two SUSE chart repositories.
// These are the single source of truth for all hardcoded registry URLs in the codebase.
// Air-gapped environments override these via Settings → registryEndpoints.
export const APP_COLLECTION_REPO_URL = 'oci://dp.apps.rancher.io/charts';
export const SUSE_REGISTRY_REPO_URL  = 'oci://registry.suse.com/ai/charts';

// NVIDIA NGC Helm repositories (HTTPS, public charts). Images are gated behind nvcr.io.
export const NVIDIA_REPO_URL           = 'https://helm.ngc.nvidia.com/nvidia';
export const NVIDIA_BLUEPRINT_REPO_URL = 'https://helm.ngc.nvidia.com/nvidia/blueprint';

export type PackagingFormat = 'HELM_CHART' | 'CONTAINER';

export interface AppLabel {
  code: string;
  name: string;
}

export interface AppCollectionItem {
  name: string;
  slug_name: string;
  description?: string;
  project_url?: string;
  documentation_url?: string;
  reference_guide_url?: string;
  source_code_url?: string;
  logo_url?: string;
  changelog_url?: string;
  last_updated_at?: string;
  packaging_format?: PackagingFormat;
  repository_url?: string;
  // 'suse-ai' and 'nvidia' are the built-in libraries; a remote catalog may define
  // its own library values, which the UI groups dynamically. `string & Record<never, never>`
  // keeps autocomplete for the built-ins while allowing arbitrary strings (the plain
  // `string & {}` form trips @typescript-eslint/ban-types).
  library?: 'suse-ai' | 'nvidia' | (string & Record<never, never>);
  // NVIDIA program/support designations (from the static catalog). Absent for
  // non-NVIDIA apps and in dynamic repo-discovery mode.
  labels?: AppLabel[];
}

export interface FailedRepo {
  url: string;
  reason: 'not-ready' | 'fetch-failed';
  message?: string;
}

export interface NvidiaClusterRepo {
  name: string;
  url: string;
  ready: boolean;
  message?: string;
}

export interface NvidiaAppsResult {
  apps: AppCollectionItem[];
  failedRepos: FailedRepo[];
}

function normalizeLogoUrl(logo?: string): string | undefined {
  if (!logo) return undefined;
  try { new URL(logo); return logo; } catch { /* not absolute */ }
  // These are relative (e.g. "/logos/xxx.png"); load directly from upstream
  return logo.startsWith('/logos/') ? `https://api.apps.rancher.io${logo}` : logo;
}


/** Find repository name by URL */
export async function findRepositoryByUrl($store: any, targetUrl: string): Promise<string | null> {
  try {
    const repositories = await fetchClusterRepositories($store);
    const repo = repositories.find(r => r.url === targetUrl);
    return repo?.name || null;
  } catch (err) {
    console.warn('Failed to find repository by URL:', err);
    return null;
  }
}

/** Determine library type from repository URL.
 *
 * NVIDIA classification is HOST-based: any helm.ngc.nvidia.com repo (org OR
 * team, e.g. .../nvidia, .../nvidia/omniverse, .../nim/nvidia) is 'nvidia'.
 * This is what tags the workload vendor=nvidia so the operator injects
 * ngc-secret/ngc-api (nvcr.io image pulls) — required by every NGC chart.
 *
 * Air-gap safety (by construction): this MUST be called with the LIVE
 * ClusterRepo URL (spec.url / spec.ociRepo), never the catalog repository_url.
 * In air-gap the live URL is the private mirror (oci://…), whose host is never
 * helm.ngc.nvidia.com, so it correctly stays 'suse' and uses the combined
 * injector that already carries nvcr.io auth. Every AppWizard.vue call site
 * (583, 906, 994, 1092, 1316, 1448, 1518) passes the live URL — keep it that way.
 */
export function getLibraryFromRepoUrl(repoUrl: string): 'suse-ai' | 'nvidia' | undefined {
  // Normalize URL by removing trailing slashes and lowercasing for comparison.
  const normalize = (url: string) => url.trim().toLowerCase().replace(/\/+$/, '');
  const normalized = normalize(repoUrl);

  // NVIDIA: any helm.ngc.nvidia.com host (org or team repo).
  try {
    if (new URL(normalized).host === 'helm.ngc.nvidia.com') {
      return 'nvidia';
    }
  } catch { /* unparseable as a URL — fall through to the SUSE checks. (An oci:// mirror parses fine but its host != helm.ngc.nvidia.com, so it also falls through here.) */ }

  // SUSE AI repositories (exact match).
  if (normalized === normalize(APP_COLLECTION_REPO_URL) ||
      normalized === normalize(SUSE_REGISTRY_REPO_URL)) {
    return 'suse-ai';
  }

  return undefined;
}

/** Get cluster repository name from repository URL */
export async function getClusterRepoNameFromUrl($store: any, repoUrl: string): Promise<string | null> {
  return await findRepositoryByUrl($store, repoUrl);
}

/**
 * Fetch the operator Settings, returning null only when none exist yet (404).
 * Real failures (operator unreachable, 5xx) are rethrown so callers don't silently
 * fall back to default/public registry URLs — which in air-gap is exactly wrong.
 */
export async function fetchSettingsOrNull(): Promise<any | null> {
  try {
    return await getSettings();
  } catch (e: any) {
    if (e?.status === 404) return null;
    throw e;
  }
}

/**
 * Fetch apps from SUSE Application Collection and SUSE Registry, merged and sorted alphabetically.
 * Pass `settings` to reuse an already-fetched Settings object (avoids a duplicate round trip when
 * the caller also calls fetchNvidiaApps). Omit it to fetch on demand.
 */
export async function fetchSuseAiApps($store: any, settings?: any | null): Promise<AppCollectionItem[]> {
  const s = settings !== undefined ? settings : await fetchSettingsOrNull();
  const re = s?.spec?.registryEndpoints || {};
  const acUrl = re.applicationCollection || APP_COLLECTION_REPO_URL;
  const srUrl = re.suseRegistry         || SUSE_REGISTRY_REPO_URL;

  const repos = await fetchClusterRepositories($store);
  const appCollectionRepo = repos.find(r => r.url === acUrl);
  const suseRegistryRepo  = repos.find(r => r.url === srUrl);

  const [appCollectionApps, suseRegistryApps] = await Promise.all([
    appCollectionRepo
      ? fetchAppsFromRepository($store, appCollectionRepo.name).then(apps => apps.map(a => ({ ...a, repository_url: acUrl, library: 'suse-ai' as const })))
      : Promise.resolve([] as AppCollectionItem[]),
    suseRegistryRepo
      ? fetchAppsFromRepository($store, suseRegistryRepo.name).then(apps => apps.map(a => ({ ...a, repository_url: srUrl, library: 'suse-ai' as const })))
      : Promise.resolve([] as AppCollectionItem[]),
  ]);

  // App Collection takes precedence on dedup
  const appMap = new Map<string, AppCollectionItem>();
  for (const app of appCollectionApps) appMap.set(app.slug_name, app);
  for (const app of suseRegistryApps) {
    if (!appMap.has(app.slug_name)) appMap.set(app.slug_name, app);
  }

  return Array.from(appMap.values()).sort((a, b) => a.name.localeCompare(b.name));
}

/** List all enabled NGC-host (helm.ngc.nvidia.com) ClusterRepos, INCLUDING
 *  not-ready ones (unlike fetchClusterRepositories, which drops them). Used to
 *  discover NVIDIA apps and to warn about repos present but not contributing. */
export async function fetchNvidiaClusterRepos($store: any): Promise<NvidiaClusterRepo[]> {
  try {
    const res = await $store.dispatch('rancher/request', { url: CLUSTERREPOS_URL, timeout: TIMEOUT_VALUES.READ });
    const repos = res?.data?.items || res?.data || res?.items || [];
    return repos
      .filter((repo: any) => repo?.spec?.enabled !== false)
      .map((repo: any) => {
        const ready = isRepoReady(repo);
        return {
          name:    repo?.metadata?.name || '',
          url:     repo?.spec?.url || repo?.spec?.gitRepo || '',
          ready,
          message: ready ? undefined : repoNotReadyMessage(repo),
        } as NvidiaClusterRepo;
      })
      .filter((r: NvidiaClusterRepo) => r.name && getLibraryFromRepoUrl(r.url) === 'nvidia');
  } catch (e) {
    logger.error('Failed to fetch NVIDIA cluster repositories', e, { component: 'AppCollection' });
    return [];
  }
}

/**
 * Fetch NVIDIA catalog apps, tagged with library 'nvidia'.
 *  - Air-gapped (registryEndpoints.nvidia set): the single mirrored OCI repo at
 *    that URL. UNCHANGED — no host discovery.
 *  - Connected (registryEndpoints.nvidia empty): every enabled NGC-host
 *    ClusterRepo (org + team, provisioned by the operator). Repos present but
 *    not-ready, or whose index fails to load, are reported in failedRepos.
 */
export async function fetchNvidiaApps($store: any, settings?: any | null): Promise<NvidiaAppsResult> {
  const s = settings !== undefined ? settings : await fetchSettingsOrNull();
  const nvUrl = s?.spec?.registryEndpoints?.nvidia;

  // Air-gap: single mirrored repo — unchanged behavior.
  if (nvUrl) {
    const repos = await fetchClusterRepositories($store);
    const repo = repos.find(r => r.url === nvUrl);
    if (!repo) return { apps: [], failedRepos: [] };
    const apps = (await fetchAppsFromRepository($store, repo.name))
      .map(a => ({ ...a, repository_url: nvUrl, library: 'nvidia' as const }))
      .sort((a, b) => a.name.localeCompare(b.name));
    return { apps, failedRepos: [] };
  }

  // Connected: discover all NGC-host ClusterRepos. Sort by URL for deterministic
  // first-wins dedup order.
  const ngcRepos = (await fetchNvidiaClusterRepos($store)).sort((a, b) => a.url.localeCompare(b.url));
  const failedRepos: FailedRepo[] = [];

  const readyRepos = ngcRepos.filter((r) => {
    if (!r.ready) {
      failedRepos.push({ url: r.url, reason: 'not-ready', message: r.message });
      return false;
    }
    return true;
  });

  const perRepo = await Promise.all(readyRepos.map(async (r) => {
    const { apps, error } = await fetchAppsFromRepositoryResult($store, r.name);
    return { repo: r, apps, error };
  }));

  const appMap = new Map<string, AppCollectionItem>();
  for (const { repo, apps, error } of perRepo) {
    if (error) {
      // rancher/request often rejects with a plain response object, not an
      // Error — prefer its message so the banner never shows "[object Object]".
      const e = error as any;
      const message = e?.message || e?.data?.message || String(error);
      failedRepos.push({ url: repo.url, reason: 'fetch-failed', message });
      continue;
    }
    for (const a of apps) {
      if (!appMap.has(a.slug_name)) {
        appMap.set(a.slug_name, { ...a, repository_url: repo.url, library: 'nvidia' as const });
      }
    }
  }

  const apps = Array.from(appMap.values()).sort((a, b) => a.name.localeCompare(b.name));
  return { apps, failedRepos };
}

/** Single source of truth for the clusterrepos list endpoint. */
export const CLUSTERREPOS_URL =
  '/k8s/clusters/local/apis/catalog.cattle.io/v1/clusterrepos?limit=1000';

const READY_CONDITION_TYPES = ['FollowerDownloaded', 'OCIDownloaded', 'Downloaded'];

/** A ClusterRepo is ready when its index has been downloaded/indexed. Shared by
 *  fetchClusterRepositories and fetchNvidiaClusterRepos so the predicate cannot
 *  drift. (Confirm the condition-type set against a live cluster — see Task 0.) */
export function isRepoReady(repo: any): boolean {
  const conditions = repo?.status?.conditions || [];
  const hasDownloaded = conditions.some(
    (c: any) => READY_CONDITION_TYPES.includes(c?.type) && c?.status === 'True',
  );
  return hasDownloaded || !!repo?.status?.indexConfigMapName;
}

/** Human-readable reason a repo is not ready, from its failing download condition. */
export function repoNotReadyMessage(repo: any): string | undefined {
  const conditions = repo?.status?.conditions || [];
  const failing = conditions.find(
    (c: any) => READY_CONDITION_TYPES.includes(c?.type) && c?.status !== 'True' && c?.message,
  );
  return failing?.message || undefined;
}

/** Repository information */
export interface AppRepository {
  name: string;
  displayName: string;
  type: string;
  url?: string;
  enabled?: boolean;
}

/** Get list of all cluster repositories */
export async function fetchClusterRepositories($store: any): Promise<AppRepository[]> {
  logger.debug('Starting cluster repositories fetch', {
    component: 'AppCollection'
  });
  try {
    const url = CLUSTERREPOS_URL;
    logger.debug('Requesting cluster repositories', {
      component: 'AppCollection',
      data: { url }
    });
    const res = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.READ });

    logger.debug('Cluster repositories response received', {
      component: 'AppCollection',
      data: {
        hasData: !!res?.data,
        hasItems: !!res?.data?.items,
        dataType: typeof res?.data,
        itemsLength: res?.data?.items ? res.data.items.length : 'N/A'
      }
    });
    
    const repos = res?.data?.items || res?.data || res?.items || [];
    logger.debug('Raw repositories count', {
      component: 'AppCollection',
      data: { count: repos.length }
    });
    
    if (repos.length > 0) {
      logger.debug('First repository sample', {
        component: 'AppCollection',
        data: {
          name: repos[0]?.metadata?.name,
          enabled: repos[0]?.spec?.enabled,
          state: repos[0]?.metadata?.state?.name,
          url: repos[0]?.spec?.url || repos[0]?.spec?.gitRepo
        }
      });
    }
    
    const filtered = repos.filter((repo: any) => {
      const enabled = repo?.spec?.enabled !== false;
      const isReady = isRepoReady(repo);

      logger.debug('Repository filtering', {
        component: 'AppCollection',
        data: {
          repo: repo?.metadata?.name,
          enabled,
          isReady,
          conditionsCount: (repo?.status?.conditions || []).length
        }
      });
      return enabled && isReady;
    });
    
    logger.debug('Filtered repositories count', {
      component: 'AppCollection',
      data: { count: filtered.length }
    });
    
    const mapped = filtered.map((repo: any) => ({
      name: repo.metadata?.name || '',
      displayName: getRepoDisplayName(repo.metadata?.name || ''),
      type: getRepoType(repo),
      url: repo.spec?.url || repo.spec?.gitRepo || '',
      enabled: repo.spec?.enabled !== false
    }));
    
    const final = mapped.filter((repo: AppRepository) => repo.name);
    logger.info('Cluster repositories fetched successfully', {
      component: 'AppCollection',
      data: {
        count: final.length,
        repos: final.map((r: AppRepository) => ({ name: r.name, type: r.type, enabled: r.enabled }))
      }
    });
    
    return final;
  } catch (e: any) {
    logger.error('Failed to fetch cluster repositories', e, {
      component: 'AppCollection'
    });
    return [];
  }
}

function getRepoDisplayName(name: string): string {
  const displayNames: Record<string, string> = {
    'rancher-charts': 'Rancher Charts',
    'rancher-partner-charts': 'Rancher Partner Charts',
    'rancher-rke2-charts': 'RKE2 Charts',
    'jetstack': 'Jetstack',
    'suse-edge': 'SUSE Edge'
  };
  return displayNames[name] || name.replace(/-/g, ' ').replace(/\b\w/g, l => l.toUpperCase());
}

function getRepoType(repo: any): string {
  if (repo.spec?.gitRepo) return 'git';
  if (repo.spec?.url?.startsWith('oci:')) return 'oci';
  return 'helm';
}

/** Fetch apps from a specific cluster repository, surfacing fetch errors instead
 *  of swallowing them. `error` is set when the repo is unreachable or its index
 *  fails to load; apps is [] in that case. */
export async function fetchAppsFromRepositoryResult(
  $store: any,
  repoName: string,
): Promise<{ apps: AppCollectionItem[]; error?: unknown }> {
  const found = await getClusterContext($store, { repoName });
  if (!found) {
    logger.warn(`ClusterRepo "${repoName}" not found in any cluster`);
    return { apps: [], error: new Error(`ClusterRepo "${repoName}" not found`) };
  }
  const { baseApi } = found;

  try {
    const indexUrl = `${baseApi}/catalog.cattle.io.clusterrepos/${encodeURIComponent(repoName)}?link=index`;
    const res = await $store.dispatch('rancher/request', { url: indexUrl, timeout: TIMEOUT_VALUES.READ });
    const indexData = res?.data || res;
    const entries = indexData?.entries || {};

    const apps: AppCollectionItem[] = [];
    for (const [chartName, versions] of Object.entries(entries)) {
      if (!Array.isArray(versions) || versions.length === 0) continue;
      const latestVersion = versions[0] as any;
      apps.push({
        name:            latestVersion.name || chartName,
        slug_name:       chartName,
        description:     latestVersion.description || '',
        project_url:     latestVersion.home || '',
        source_code_url: Array.isArray(latestVersion.sources) ? latestVersion.sources[0] : latestVersion.sources,
        logo_url:        latestVersion.icon ? normalizeLogoUrl(latestVersion.icon) : undefined,
        last_updated_at: latestVersion.created || new Date().toISOString(),
        packaging_format: 'HELM_CHART',
      });
    }
    apps.sort((a, b) => new Date(b.last_updated_at || 0).getTime() - new Date(a.last_updated_at || 0).getTime());
    logger.info('Repository apps fetched successfully', { component: 'AppCollection', data: { repoName, count: apps.length } });
    return { apps };
  } catch (e) {
    logger.error('Failed to fetch apps from repository', e, { component: 'AppCollection', data: { repoName } });
    return { apps: [], error: e };
  }
}

/** Backward-compatible wrapper: returns just the apps ([] on error), preserving
 *  the contract existing callers (e.g. fetchSuseAiApps) rely on. */
export async function fetchAppsFromRepository($store: any, repoName: string): Promise<AppCollectionItem[]> {
  const { apps } = await fetchAppsFromRepositoryResult($store, repoName);
  return apps;
}

/** Fetch apps from all cluster repositories */
export async function fetchAllRepositoryApps($store: any): Promise<{ [repoName: string]: AppCollectionItem[] }> {
  logger.debug('Starting fetch all repository apps', {
    component: 'AppCollection'
  });
  const repositories = await fetchClusterRepositories($store);
  logger.debug('Found repositories', {
    component: 'AppCollection',
    data: {
      count: repositories.length,
      repos: repositories.map(r => ({ name: r.name, enabled: r.enabled }))
    }
  });
  
  const repoApps: { [repoName: string]: AppCollectionItem[] } = {};
  
  await Promise.all(repositories.map(async (repo) => {
    logger.debug('Processing repository', {
      component: 'AppCollection',
      data: { repoName: repo.name }
    });
    try {
      const apps = await fetchAppsFromRepository($store, repo.name);
      if (apps.length > 0) {
        repoApps[repo.name] = apps;
        logger.debug('Repository apps loaded', {
          component: 'AppCollection',
          data: { repoName: repo.name, count: apps.length }
        });
      }
    } catch (e) {
      logger.error('Failed to fetch apps from repository', e, {
        component: 'AppCollection',
        data: { repoName: repo.name }
      });
    }
  }));
  
  logger.info('All repository apps fetched successfully', {
    component: 'AppCollection',
    data: {
      totalRepos: Object.keys(repoApps).length,
      repos: Object.keys(repoApps).map(key => ({ repo: key, count: repoApps[key].length }))
    }
  });
  return repoApps;
}

/**
 * Merge curated catalog metadata onto dynamically-discovered apps.
 *
 * Keyed by `(library, slug_name)` — robust in connected AND air-gap (library is
 * stamped consistently by the fetchers; slug_name == chart name in both). The
 * full repository URL is deliberately NOT part of the key: in air-gap the live
 * URL is a private mirror that never matches the curated repository_url.
 *
 * Precedence:
 *  - curated wins (enrichment the Helm index lacks, + logo for air-gap):
 *    labels, documentation_url, reference_guide_url, changelog_url, logo_url.
 *  - live wins, curated fallback (chart-intrinsic, fresh in the live index):
 *    name, description, project_url, source_code_url, packaging_format.
 *  - live always wins (identity/volatile): slug_name, library, repository_url,
 *    last_updated_at.
 * Discovered apps with no curated match pass through unchanged; curated entries
 * with no discovered match are dropped (existence is discovery-driven).
 */
export function overlayCuratedMetadata(
  discovered: AppCollectionItem[],
  curated: AppCollectionItem[],
): AppCollectionItem[] {
  // Contract: both sides stamp `library` with the same slug — the operator
  // stamps it from its catalog's top-level key ('nvidia' / 'suse-ai'), and the
  // discovered side stamps the identical value (see getLibraryFromRepoUrl). If
  // those slugs ever diverge (value or casing) the overlay silently no-ops, so
  // keep them in sync.
  const keyOf = (a: { library?: string; slug_name: string }) => `${a.library ?? ''} ${a.slug_name}`;
  const curatedByKey = new Map<string, AppCollectionItem>();
  for (const c of curated) curatedByKey.set(keyOf(c), c);

  return discovered.map((app) => {
    const c = curatedByKey.get(keyOf(app));
    if (!c) return app;
    return {
      ...app,
      // curated wins
      labels:              c.labels ?? app.labels,
      documentation_url:   c.documentation_url  || app.documentation_url,
      reference_guide_url: c.reference_guide_url || app.reference_guide_url,
      changelog_url:       c.changelog_url       || app.changelog_url,
      logo_url:            c.logo_url            || app.logo_url,
      // live wins, curated fallback
      name:            app.name            || c.name,
      description:     app.description     || c.description,
      project_url:     app.project_url     || c.project_url,
      source_code_url: app.source_code_url || c.source_code_url,
      packaging_format: app.packaging_format || c.packaging_format,
      // live always wins: slug_name, library, repository_url, last_updated_at (from ...app)
    };
  });
}

/**
 * Fetch the curated catalog for use as a dynamic-mode overlay. Unlike
 * fetchStaticCatalog (whose contract is "throw so static mode shows an error"),
 * this swallows failures and returns [] so the overlay stays additive and never
 * blanks the page. When it returns [], overlayCuratedMetadata is a no-op.
 */
export async function fetchCuratedOverlayOrEmpty(): Promise<AppCollectionItem[]> {
  try {
    return await fetchStaticCatalog();
  } catch (e) {
    logger.warn('Curated overlay unavailable; rendering discovered apps only', {
      component: 'AppCollection',
      data: { error: String(e) },
    });
    return [];
  }
}

/** Format failed-repo entries into human-readable warning lines for the banner. */
export function buildWarnings(failedRepos: FailedRepo[]): string[] {
  return failedRepos.map((r) => {
    const reason = r.reason === 'not-ready'
      ? 'not ready (repository index has not been downloaded)'
      : 'could not be loaded';
    return r.message ? `${r.url}: ${reason} — ${r.message}` : `${r.url}: ${reason}`;
  });
}
