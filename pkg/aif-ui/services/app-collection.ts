import { getClusterContext } from '../utils/cluster-operations';
import { log as logger } from '../utils/logger';
import { getSettings } from '../utils/operator-api';
import { TIMEOUT_VALUES } from '../utils/constants';

// Canonical OCI registry URLs for the two SUSE chart repositories.
// These are the single source of truth for all hardcoded registry URLs in the codebase.
// Air-gapped environments override these via Settings → registryEndpoints.
export const APP_COLLECTION_REPO_URL = 'oci://dp.apps.rancher.io/charts';
export const SUSE_REGISTRY_REPO_URL  = 'oci://registry.suse.com/ai/charts';

// NVIDIA NGC Helm repositories (HTTPS, public charts). Images are gated behind nvcr.io.
export const NVIDIA_REPO_URL           = 'https://helm.ngc.nvidia.com/nvidia';
export const NVIDIA_BLUEPRINT_REPO_URL = 'https://helm.ngc.nvidia.com/nvidia/blueprint';

export type PackagingFormat = 'HELM_CHART' | 'CONTAINER';

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
  library?: 'suse-ai' | 'nvidia';
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

/** Determine library type from repository URL */
export function getLibraryFromRepoUrl(repoUrl: string): 'suse-ai' | 'nvidia' | undefined {
  // Normalize URL by removing trailing slashes and converting to lowercase for comparison
  const normalize = (url: string) => url.trim().toLowerCase().replace(/\/+$/, '');
  const normalized = normalize(repoUrl);

  // Check NVIDIA repositories
  if (normalized === normalize(NVIDIA_REPO_URL) ||
      normalized === normalize(NVIDIA_BLUEPRINT_REPO_URL)) {
    return 'nvidia';
  }

  // Check SUSE AI repositories
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

/**
 * Fetch NVIDIA catalog apps, tagged with library 'nvidia'.
 *  - Connected (registryEndpoints.nvidia empty): the two public NGC HTTPS repos.
 *  - Air-gapped (registryEndpoints.nvidia set): the single mirrored OCI repo at that URL.
 */
export async function fetchNvidiaApps($store: any, settings?: any | null): Promise<AppCollectionItem[]> {
  const s = settings !== undefined ? settings : await fetchSettingsOrNull();
  const nvUrl = s?.spec?.registryEndpoints?.nvidia;
  const urls = nvUrl ? [nvUrl] : [NVIDIA_REPO_URL, NVIDIA_BLUEPRINT_REPO_URL];

  const repos = await fetchClusterRepositories($store);

  const perRepo = await Promise.all(urls.map(async (url) => {
    const repo = repos.find(r => r.url === url);
    if (!repo) return [] as AppCollectionItem[];
    const apps = await fetchAppsFromRepository($store, repo.name);
    return apps.map(a => ({ ...a, repository_url: url, library: 'nvidia' as const }));
  }));

  // Single mirrored repo (air-gap): nothing to dedup.
  if (urls.length === 1) {
    return perRepo[0].sort((a, b) => a.name.localeCompare(b.name));
  }

  // Connected: dedup by slug across the two public NGC repos; first occurrence wins.
  const appMap = new Map<string, AppCollectionItem>();
  for (const apps of perRepo) {
    for (const app of apps) {
      if (!appMap.has(app.slug_name)) appMap.set(app.slug_name, app);
    }
  }
  return Array.from(appMap.values()).sort((a, b) => a.name.localeCompare(b.name));
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
    const url = '/k8s/clusters/local/apis/catalog.cattle.io/v1/clusterrepos?limit=1000';
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
      
      // Check if repository is ready based on status conditions
      const conditions = repo?.status?.conditions || [];
      const hasDownloadedCondition = conditions.some((c: any) => 
        (c.type === 'FollowerDownloaded' || c.type === 'OCIDownloaded' || c.type === 'Downloaded') && 
        c.status === 'True'
      );
      const hasIndexConfigMap = !!repo?.status?.indexConfigMapName;
      const isReady = hasDownloadedCondition || hasIndexConfigMap;
      
      logger.debug('Repository filtering', {
        component: 'AppCollection',
        data: {
          repo: repo?.metadata?.name,
          enabled,
          isReady,
          conditionsCount: conditions.length
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

/** Fetch apps from a specific cluster repository */
export async function fetchAppsFromRepository($store: any, repoName: string): Promise<AppCollectionItem[]> {
  logger.debug('Starting repository apps fetch', {
    component: 'AppCollection',
    data: { repoName }
  });

  const found = await getClusterContext($store, { repoName: repoName});
  if (!found) {
    logger.warn(`ClusterRepo "${repoName}" not found in any cluster`);
    return [];
  }
  const { baseApi } = found
  
  try {
    const indexUrl = `${baseApi}/catalog.cattle.io.clusterrepos/${encodeURIComponent(repoName)}?link=index`;
    logger.debug('Requesting repository index', {
      component: 'AppCollection',
      data: { repoName, indexUrl }
    });
    const res = await $store.dispatch('rancher/request', { url: indexUrl, timeout: TIMEOUT_VALUES.READ });
    
    logger.debug('Repository index response', {
      component: 'AppCollection',
      data: {
        repoName,
        hasData: !!res?.data,
        dataType: typeof res?.data
      }
    });
    
    const indexData = res?.data || res;
    const entries = indexData?.entries || {};
    logger.debug('Repository index entries', {
      component: 'AppCollection',
      data: {
        repoName,
        entriesCount: Object.keys(entries).length
      }
    });
    
    const apps: AppCollectionItem[] = [];
    
    for (const [chartName, versions] of Object.entries(entries)) {
      if (!Array.isArray(versions) || versions.length === 0) continue;
      
      const latestVersion = versions[0] as any;
      const app: AppCollectionItem = {
        name: latestVersion.name || chartName,
        slug_name: chartName,
        description: latestVersion.description || '',
        project_url: latestVersion.home || '',
        source_code_url: Array.isArray(latestVersion.sources) ? latestVersion.sources[0] : latestVersion.sources,
        logo_url: latestVersion.icon ? normalizeLogoUrl(latestVersion.icon) : undefined,
        last_updated_at: latestVersion.created || new Date().toISOString(),
        packaging_format: 'HELM_CHART'
      };
      
      apps.push(app);
    }
    
    logger.info('Repository apps fetched successfully', {
      component: 'AppCollection',
      data: { repoName, count: apps.length }
    });
    return apps.sort((a, b) => new Date(b.last_updated_at || 0).getTime() - new Date(a.last_updated_at || 0).getTime());
  } catch (e) {
    logger.error('Failed to fetch apps from repository', e, {
      component: 'AppCollection',
      data: { repoName }
    });
    return [];
  }
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
