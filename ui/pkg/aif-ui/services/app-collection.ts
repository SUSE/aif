import { getClusterContext } from '../utils/cluster-operations';
import { log as logger } from '../utils/logger';
import { getSettings, getRegistryCredentials } from '../utils/operator-api';
import { TIMEOUT_VALUES } from '../utils/constants';
import { fetchStaticCatalog } from './static-catalog';

// Canonical OCI registry URLs for the two SUSE chart repositories.
// These are the single source of truth for all hardcoded registry URLs in the codebase.
// Air-gapped environments override these via Settings → registryEndpoints.
export const APP_COLLECTION_REPO_URL = 'oci://dp.apps.rancher.io/charts';
export const SUSE_REGISTRY_REPO_URL  = 'oci://registry.suse.com/ai/charts';

// The single NGC Helm registry host. A repo is classified 'nvidia' iff its URL
// host equals this (see getLibraryFromRepoUrl); mirror of the operator's NGCHost.
export const NGC_HOST = 'helm.ngc.nvidia.com';

// NVIDIA NGC Helm repositories (HTTPS, public charts). Images are gated behind nvcr.io.
export const NVIDIA_REPO_URL           = `https://${NGC_HOST}/nvidia`;
export const NVIDIA_BLUEPRINT_REPO_URL = `https://${NGC_HOST}/nvidia/blueprint`;

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
  // The authoritative operator-managed ClusterRepo name this app installs from.
  // Attached by the dynamic fetchers so install never re-resolves by URL. Absent
  // for static-catalog items (resolved via findManagedRepoNameByUrl at install).
  repository_name?: string;
  // 'suse-ai' and 'nvidia' are the built-in libraries; a remote catalog may define
  // its own library values, which the UI groups dynamically. `string & Record<never, never>`
  // keeps autocomplete for the built-ins while allowing arbitrary strings (the plain
  // `string & {}` form trips @typescript-eslint/ban-types).
  library?: 'suse-ai' | 'nvidia' | (string & Record<never, never>);
  // Program/support designations (from the static catalog). Absent in dynamic
  // repo-discovery mode.
  labels?: AppLabel[];
}

// Single source of truth for the "supported" rule, shared by the Apps page sort
// order (Apps.vue) and the badge color/ordering (AppLabels.vue) so the two can't
// drift. The catalog uses the bare `supported` code; the legacy
// `<program>_supported` convention is also honored.
export function isSupportedCode(code: string): boolean {
  return code === 'supported' || code.endsWith('_supported');
}

// True when any of an app's labels marks it as supported.
export function isAppSupported(app: Pick<AppCollectionItem, 'labels'>): boolean {
  return (app.labels ?? []).some(l => isSupportedCode(l.code));
}

export interface FailedRepo {
  url: string;
  reason: 'not-ready' | 'fetch-failed';
  message?: string;
}

export interface RepoAppsResult {
  apps: AppCollectionItem[];
  failedRepos: FailedRepo[];
}

// Retained alias: fetchNvidiaApps historically returned this shape.
export type NvidiaAppsResult = RepoAppsResult;

function normalizeLogoUrl(logo?: string): string | undefined {
  if (!logo) return undefined;
  try { new URL(logo); return logo; } catch { /* not absolute */ }
  // These are relative (e.g. "/logos/xxx.png"); load directly from upstream
  return logo.startsWith('/logos/') ? `https://api.apps.rancher.io${logo}` : logo;
}


/** First managed (provenance-labeled) ClusterRepo whose live URL matches. Scoped to
 *  the managed set so an out-of-band repo at the same URL is never chosen — this is
 *  the install-path counterpart to fetchManagedRepos' discovery scoping. */
export async function findManagedRepoNameByUrl($store: any, targetUrl: string): Promise<string | null> {
  const managed = await fetchManagedRepos($store);
  const repo = managed.find(r => r.url === targetUrl);
  return repo?.name ?? null;
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
 * injector that already carries nvcr.io auth. Contract: every caller MUST pass the
 * live ClusterRepo URL (spec.url / spec.ociRepo), never a catalog repository_url —
 * passing the catalog URL in air-gap would misclassify the repo as 'nvidia'.
 */
export function getLibraryFromRepoUrl(repoUrl: string): 'suse-ai' | 'nvidia' | undefined {
  // Normalize URL by removing trailing slashes and lowercasing for comparison.
  const normalize = (url: string) => url.trim().toLowerCase().replace(/\/+$/, '');
  const normalized = normalize(repoUrl);

  // NVIDIA: any helm.ngc.nvidia.com host (org or team repo).
  try {
    if (new URL(normalized).host === NGC_HOST) {
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

/** Resolve the ClusterRepo name to install an app from. Dynamic-mode items carry
 *  the authoritative repository_name; static-catalog items resolve their
 *  repository_url within the managed set. */
export async function resolveInstallRepoName(
  $store: any,
  app: Pick<AppCollectionItem, 'repository_name' | 'repository_url'>,
): Promise<string | null> {
  if (app.repository_name) return app.repository_name;
  if (app.repository_url) return await findManagedRepoNameByUrl($store, app.repository_url);
  return null;
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
 * Returns { apps, failedRepos } symmetric with fetchNvidiaApps. Loads apps only from operator-managed
 * ClusterRepos (fixed names, via fetchManagedRepos) — never by URL, so an out-of-band ClusterRepo at
 * a matching URL is ignored. The operator creates these repos only when the corresponding credentials
 * are configured and prunes them otherwise, so the EXISTENCE of a managed repo — not the settings
 * section — is the signal that the registry is in use (the operator API always serializes
 * spec.applicationCollection / spec.suseRegistry as {}, value structs with ineffective omitempty,
 * so section presence is not usable — the same reasoning that governs fetchNvidiaApps). Any managed
 * repo that exists is loaded; a not-ready repo or an index fetch failure is reported via failedRepos.
 * The `settings` parameter is accepted for call-site symmetry with fetchNvidiaApps but is not needed here.
 * `managedRepos`, when supplied, is used instead of re-listing ClusterRepos — the caller lists once
 * (fetchManagedRepos) and threads the same set into both fetchers.
 */
export async function fetchSuseAiApps($store: any, _settings?: any | null, managedRepos?: ManagedRepo[]): Promise<RepoAppsResult> {
  const all = managedRepos ?? await fetchManagedRepos($store);
  const managed = all.filter(r => r.library === 'suse-ai');
  const failedRepos: FailedRepo[] = [];

  // Order: application-collection first (dedup precedence), then suse-ai-registry.
  const order = ['application-collection', 'suse-ai-registry'];
  const ranked = managed.slice().sort((a, b) => order.indexOf(a.name) - order.indexOf(b.name));

  const readyRepos = ranked.filter((r) => {
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
      const e = error as any;
      const message = e?.message || e?.data?.message || String(error);
      failedRepos.push({ url: repo.url, reason: 'fetch-failed', message });
      continue;
    }
    for (const a of apps) {
      if (!appMap.has(a.slug_name)) {
        appMap.set(a.slug_name, { ...a, repository_url: repo.url, repository_name: repo.name, library: 'suse-ai' as const });
      }
    }
  }

  const appsOut = Array.from(appMap.values()).sort((a, b) => a.name.localeCompare(b.name));
  return { apps: appsOut, failedRepos };
}

/**
 * Fetch NVIDIA catalog apps, tagged with library 'nvidia'. Loads apps only from
 * operator-managed ClusterRepos (fixed names + team-repo label, via
 * fetchManagedRepos) — never by host, so an out-of-band NGC-host ClusterRepo is
 * ignored. The operator creates these repos only when NVIDIA credentials are
 * configured and prunes them otherwise, so the EXISTENCE of a managed repo — not
 * the settings section — is the signal that NVIDIA is in use (the operator API
 * always serializes spec.nvidia as {}, a value struct with ineffective omitempty,
 * so section presence is not usable). Any managed repo that exists is loaded; a
 * not-ready repo or an index fetch failure is reported via failedRepos. When no
 * managed repo exists, "not created yet" is surfaced only if NVIDIA credentials
 * effectively resolve (per the operator's /registry-credentials endpoint — which
 * catches well-known secret names a spec check misses) so an unconfigured registry
 * stays silent. Works the same air-gapped or connected: name-based matching is
 * endpoint-independent, and each app's repository_url is the live repo's spec.url.
 */
export async function fetchNvidiaApps($store: any, settings?: any | null, managedRepos?: ManagedRepo[]): Promise<NvidiaAppsResult> {
  // Managed nvidia repos (fixed names + team-labeled). Sorted for deterministic
  // first-wins dedup. No host-based discovery, so unmanaged NGC repos are excluded.
  // `managedRepos`, when supplied, avoids re-listing ClusterRepos (the caller lists once).
  const all = managedRepos ?? await fetchManagedRepos($store);
  const managed = all
    .filter(r => r.library === 'nvidia')
    .sort((a, b) => a.url.localeCompare(b.url) || a.name.localeCompare(b.name));

  const failedRepos: FailedRepo[] = [];

  if (managed.length === 0) {
    // No managed repo. Only nag when NVIDIA is effectively configured per the
    // operator's credentials endpoint (EffectiveRefs — catches well-known secret
    // names a spec.nvidia.tokenSecretRef check misses). Otherwise stay silent.
    let configured = false;
    try {
      const creds = await getRegistryCredentials();
      configured = Boolean(creds?.nvidia?.username);
    } catch { /* operator unreachable — fail silent, no false banner */ }
    if (configured) {
      // Only now is `settings` needed — for the banner's registry-URL fallback.
      const s = settings !== undefined ? settings : await fetchSettingsOrNull();
      const url = s?.spec?.registryEndpoints?.nvidia || NVIDIA_REPO_URL;
      failedRepos.push({ url, reason: 'not-ready', message: 'repository not created yet' });
    }
    return { apps: [], failedRepos };
  }

  const readyRepos = managed.filter((r) => {
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
        appMap.set(a.slug_name, { ...a, repository_url: repo.url, repository_name: repo.name, library: 'nvidia' as const });
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

/** A ClusterRepo is ready when its index is actually fetchable. Called by
 *  fetchClusterRepositories and fetchManagedRepos (which stamps the result onto
 *  ManagedRepo.ready, consumed by fetchSuseAiApps/fetchNvidiaApps) so the predicate
 *  cannot drift. Readiness is gated on `status.indexConfigMapName`: apps are read via
 *  `?link=index`, which Rancher resolves *through* that ConfigMap. A repo can
 *  briefly report a download condition True (OCIDownloaded / Downloaded /
 *  FollowerDownloaded) before Rancher writes the index ConfigMap; fetching in that
 *  window fails with `configmaps "" not found`. Requiring indexConfigMapName makes
 *  readiness mean "the index can be served", which is what every caller needs — and
 *  it also preserves the older-Rancher path that set the ConfigMap without a ready
 *  condition. */
export function isRepoReady(repo: any): boolean {
  return !!repo?.status?.indexConfigMapName;
}

/** Human-readable reason a repo is not ready, from its failing download condition. */
export function repoNotReadyMessage(repo: any): string | undefined {
  const conditions = repo?.status?.conditions || [];
  const failing = conditions.find(
    (c: any) => READY_CONDITION_TYPES.includes(c?.type) && c?.status !== 'True' && c?.message,
  );
  return failing?.message || undefined;
}

/** Fixed ClusterRepo names the operator creates for each SUSE/NVIDIA registry.
 *  Mirror of operator/internal/credentials/credentials.go. These names (plus the
 *  team-repo label below) are the provenance signal for operator-managed repos. */
export const MANAGED_REPO_NAMES: Record<string, 'suse-ai' | 'nvidia'> = {
  'application-collection': 'suse-ai',
  'suse-ai-registry':       'suse-ai',
  'nvidia':                 'nvidia',
  'nvidia-blueprints':      'nvidia',
};

/** Label the operator stamps on NVIDIA team ClusterRepos. Mirror of
 *  settings_controller.go teamRepoMarkerLabel. Presence marks a managed nvidia
 *  repo; the value is not significant. */
export const NVIDIA_TEAM_REPO_LABEL = 'ai-factory.suse.com/nvidia-team-repo';

/** Provenance label the operator stamps on EVERY ClusterRepo it creates. Mirror of
 *  settings_controller.go managedRepoMarkerLabel. Presence (value exactly 'true')
 *  is the sole signal that a ClusterRepo is operator-managed, so an out-of-band repo
 *  at a matching URL/host is never discovered. */
export const MANAGED_REPO_LABEL = 'ai-factory.suse.com/managed-repo';

export interface ManagedRepo {
  name: string;
  url: string;
  library: 'suse-ai' | 'nvidia';
  ready: boolean;
  message?: string;
}

/** List the operator-managed ClusterRepos (by provenance label), classified by
 *  canonical name or team-repo label, with readiness. This is the single source of
 *  truth for dynamic-mode discovery — it deliberately does NOT match by URL/host,
 *  so pre-existing/unmanaged repos are excluded. Includes not-ready repos so
 *  callers can surface them. */
export async function fetchManagedRepos($store: any): Promise<ManagedRepo[]> {
  try {
    const res = await $store.dispatch('rancher/request', { url: CLUSTERREPOS_URL, timeout: TIMEOUT_VALUES.READ });
    const repos = res?.data?.items || res?.data || res?.items || [];
    const out: ManagedRepo[] = [];
    for (const repo of repos) {
      if (repo?.spec?.enabled === false) continue;
      const name = repo?.metadata?.name || '';
      if (!name) continue;
      const labels = repo?.metadata?.labels || {};
      // Provenance gate: only operator-stamped repos, matched exactly.
      if (labels[MANAGED_REPO_LABEL] !== 'true') continue;
      // Classify by canonical name (prototype-safe) or team label.
      let library: 'suse-ai' | 'nvidia' | undefined =
        Object.prototype.hasOwnProperty.call(MANAGED_REPO_NAMES, name) ? MANAGED_REPO_NAMES[name] : undefined;
      if (!library && labels[NVIDIA_TEAM_REPO_LABEL] === 'true') library = 'nvidia';
      if (!library) continue;
      const isReady = isRepoReady(repo);
      out.push({
        name,
        url:     repo?.spec?.url || repo?.spec?.gitRepo || '',
        library,
        ready:   isReady,
        message: isReady ? undefined : repoNotReadyMessage(repo),
      });
    }
    return out;
  } catch (e) {
    logger.error('Failed to fetch managed cluster repositories', e, { component: 'AppCollection' });
    return [];
  }
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
