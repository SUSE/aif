import yaml from 'js-yaml';
import { APP_COLLECTION_REPO_URL, SUSE_REGISTRY_REPO_URL, NVIDIA_REPO_URL, NVIDIA_BLUEPRINT_REPO_URL } from './app-collection';

// Utility function to deep merge objects (for combining chart defaults with user values)
function deepMerge(target: Record<string, any>, source: Record<string, any>): Record<string, any> {
  const result = { ...target };

  for (const key in source) {
    if (Object.prototype.hasOwnProperty.call(source, key)) {
      if (source[key] && typeof source[key] === 'object' && !Array.isArray(source[key]) &&
          result[key] && typeof result[key] === 'object' && !Array.isArray(result[key])) {
        // Recursively merge objects
        result[key] = deepMerge(result[key], source[key]);
      } else {
        // Override with source value (including arrays and primitives)
        result[key] = source[key];
      }
    }
  }

  return result;
}
import { log as logger } from '../utils/logger';
import { createChartValuesService, extractFileFromTarGz } from './chart-values';
import { createErrorHandler, handleSimpleError } from '../utils/error-handler';
import type {
  Dispatchable,
  ClusterInfo,
  ClusterResource,
  NamespaceResource,
  HelmSecret,
  HelmReleaseInfo,
  HelmInstallationDetails,
  AppCRD,
  RegistrySecret,
  RepositoryIndex,
  FileEntry,
  RancherError,
  ListResponse,
  InstallationPayload,
  ProjectResource,
  ServiceAccount
} from '../types/rancher-types';
import { getClusterContext } from '../utils/cluster-operations';
import { filterAndSortVersions } from '../utils/chart-version';
import { TIMEOUT_VALUES } from '../utils/constants';

export interface ChartRef {
  repoName: string;   // ClusterRepo metadata.name
  chartName: string;  // Chart name within repo
  version: string;    // SemVer
}

/* ============================== logging helpers - CLEANED UP ============================== */
// Legacy logging functions - replaced with proper logger
const log = (l: string, ...a: unknown[]) => {
  logger.debug(l, { component: 'RancherApps', data: a.length > 0 ? a : undefined });
};
const dbg = (label: string, obj: unknown) => {
  logger.debug(label, {
    component: 'RancherApps',
    data: {
      type: typeof obj,
      isArray: Array.isArray(obj),
      keys: obj && typeof obj === 'object' && obj !== null ? Object.keys(obj).slice(0, 25) : []
    }
  });
};

/* ============================== name matching =============================== */

function normName(s?: string): string {
  return (s || '').toLowerCase().replace(/[^a-z0-9]+/g, '');
}
function sameName(a?: string, b?: string): boolean {
  return !!a && !!b && normName(a) === normName(b);
}
function matchesSlug(name: string, slug: string, guess?: string): boolean {
  return sameName(name, slug) || (guess ? sameName(name, guess) : false);
}

/* ===================== cluster + Rancher App basic helpers ==================== */

function isClusterReady(c: ClusterResource): boolean {
  if (c.id === 'local' || c.metadata?.name === 'local') return true;
  if (c.status?.ready === true) return true;
  const conditions = (c.status?.conditions ?? []) as Array<{ type: string; status: string }>;
  return conditions.some(cond => cond.type === 'Ready' && cond.status === 'True');
}

// getAllClusters returns every cluster Rancher knows about, including unhealthy ones.
// Each entry carries a `ready` flag so callers can disable/grey out unreachable clusters
// without hiding them from the user entirely.
export async function getAllClusters($store: Dispatchable): Promise<ClusterInfo[]> {
  try {
    let timer: ReturnType<typeof setTimeout>;
    const rows = await Promise.race([
      $store.dispatch('management/findAll', { type: 'cluster' }),
      new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error('timeout')), TIMEOUT_VALUES.READ); }),
    ]).finally(() => clearTimeout(timer)) as ClusterResource[];
    return (rows || []).map((c: ClusterResource) => ({
      id:    c.id,
      name:  c.spec?.displayName || c.metadata?.name || c.id,
      ready: isClusterReady(c)
    }));
  } catch {
    const res = await $store.dispatch('rancher/request', { url: '/v1/management.cattle.io.clusters?limit=2000', timeout: TIMEOUT_VALUES.CLUSTER });
    const items = res?.data?.data || res?.data || [];
    return (items || [])
      .map((c: ClusterResource) => ({
        id:    c?.metadata?.name || c?.id,
        name:  c?.spec?.displayName || c?.metadata?.name || c?.id,
        ready: isClusterReady(c)
      })).filter((x: ClusterInfo) => !!x.id);
  }
}

// getClusters returns only ready clusters. Used for polling loops and any code that
// makes downstream API calls — unhealthy clusters cause those calls to time out.
export async function getClusters($store: Dispatchable): Promise<ClusterInfo[]> {
  const all = await getAllClusters($store);
  return all.filter(c => c.ready !== false);
}

export async function ensureNamespace($store: Dispatchable, clusterId: string, namespace: string): Promise<void> {
  const getUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}`;
  try {
    await $store.dispatch('rancher/request', { url: getUrl, timeout: TIMEOUT_VALUES.CLUSTER });
  } catch {
    const createUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces`;
    await $store.dispatch('rancher/request', {
      url: createUrl,
      method: 'POST',
      data: { apiVersion: 'v1', kind: 'Namespace', metadata: { name: namespace } },
      timeout: TIMEOUT_VALUES.MUTATION
    });
  }
}



export async function createOrUpgradeApp(
  $store: Dispatchable,
  clusterId: string,
  namespace: string,
  releaseName: string,
  chart: { repoName: string; chartName: string; version: string },
  values: Record<string, unknown>,
  preferredAction: 'install' | 'upgrade' = 'install'
) {
  const errorHandler = createErrorHandler($store, 'RancherApps');
  const log = (l: string, ...a: unknown[]) => { try { console.log(`[SUSE-AI-INSTALL] ${l}`, ...a); } catch {} };

  log('=== Starting createOrUpgradeApp ===');
  log('Input parameters:', { 
    clusterId, 
    namespace, 
    releaseName, 
    chart: chart,
    preferredAction,
    valuesKeys: Object.keys(values || {}),
    valuesSize: JSON.stringify(values || {}).length 
  });

  const clusterReposUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/v1/catalog.cattle.io.clusterrepos/${chart.repoName}?action=${preferredAction}`;
  const appsUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/apis/catalog.cattle.io/v1/namespaces/${encodeURIComponent(namespace)}/apps`;
  const appUrl = `${appsUrl}/${encodeURIComponent(releaseName)}`;

  log('URLs constructed:', { clusterReposUrl, appsUrl, appUrl });

  log('Fetching projects for cluster...', clusterId);
  try {    
    const charts = [
      {
        chartName: chart.chartName,
        version: chart.version,
        releaseName,
        annotations: {
          'catalog.cattle.io/ui-source-repo-type': 'cluster',
          'catalog.cattle.io/ui-source-repo': chart.repoName
        },
        values
      }
    ];
    log('Charts array prepared:', charts);

    const appPayload = {
      apiVersion: 'catalog.cattle.io/v1',
      kind:       'App',
      metadata:   {
        namespace,
        name:   releaseName,
        labels: { 'catalog.cattle.io/cluster-repo-name': chart.repoName },
        resourceVersion: undefined as string | undefined
      },
      spec: {
        chart: {
          metadata: {
            name:    chart.chartName,
            version: chart.version,
          }
        },
        name:      releaseName,
        namespace: namespace,
        values,
      },
    };

    // For upgrade actions, use the clusterRepo action directly instead of trying PUT
    if (preferredAction === 'upgrade') {
      log('Performing upgrade via clusterRepo action');
      const upgradeData = {
        charts,
        namespace,
        clusterId,
        wait: true,
        timeout: '600s',
        noHooks: false,
        disableOpenAPIValidation: false,
        skipCRDs: false
      };
      
      try {
        log('Dispatching upgrade request...', { url: clusterReposUrl, data: upgradeData });
        const upgradeResult = await $store.dispatch('rancher/request', {
          method: 'post',
          url: clusterReposUrl,
          data: upgradeData,
          timeout: TIMEOUT_VALUES.MUTATION
        });
        log('App upgrade successful. Result:', upgradeResult);
        log('=== Completed createOrUpgradeApp (upgrade) ===');
        return { upgraded: true };
      } catch (upgradeError: unknown) {
        const standardError = errorHandler.handleApiError(upgradeError, 'upgrade', { releaseName, namespace });
        throw new Error(`Failed to upgrade app: ${standardError.message}`);
      }
    }

    // For install actions, check if app exists first and use upgrade if it does
    try {
      log('Checking for existing App...', { namespace, releaseName, checkUrl: appUrl });
      const existingAppResp = await $store.dispatch('rancher/request', { url: appUrl, timeout: TIMEOUT_VALUES.CLUSTER });
      const existingApp = existingAppResp?.data ?? existingAppResp;
      const existingState = (existingApp?.status?.summary?.state || existingApp?.metadata?.state?.name || '').toLowerCase();
      if (existingState === 'uninstalling') {
        throw new Error(
          `Cannot install "${releaseName}" — a previous installation is still being uninstalled. ` +
          `Wait for it to finish and then retry.`
        );
      }

      // App exists — use clusterRepo upgrade action to re-trigger Helm
      log('App exists, performing upgrade via clusterRepo action');
      const upgradeUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/v1/catalog.cattle.io.clusterrepos/${chart.repoName}?action=upgrade`;
      const upgradeData = {
        charts,
        namespace,
        clusterId,
        wait: true,
        timeout: '600s',
        noHooks: false,
        disableOpenAPIValidation: false,
        skipCRDs: false
      };

      try {
        await $store.dispatch('rancher/request', {
          method: 'post',
          url: upgradeUrl,
          data: upgradeData,
          timeout: TIMEOUT_VALUES.MUTATION
        });
        log('App upgrade successful');
        return { upgraded: true };
      } catch (upgradeError: unknown) {
        const standardError = errorHandler.handleApiError(upgradeError, 'upgrade', { releaseName, namespace });
        throw new Error(`Failed to upgrade app: ${standardError.message}`);
      }
    } catch (e: unknown) {
      const standardError = errorHandler.normalizeError(e);

      log('Exception during app check/upgrade:', {
        error: e,
        status: standardError.status,
        message: standardError.message,
        details: standardError.details
      });

      if (standardError.status === 404) {
        log('App does not exist (404), performing install (POST)');
        
        const installData = {
          charts,
          namespace,
          clusterId,
          wait: true,
          timeout: '600s',
          noHooks: false,
          disableOpenAPIValidation: false,
          skipCRDs: false
        };
        
        try {
          const installResult = await $store.dispatch('rancher/request', {
            method: 'post',
            url: clusterReposUrl,
            data: installData,
            timeout: TIMEOUT_VALUES.MUTATION
          });
          log('App install successful');
        } catch (installError: unknown) {
          const standardError = errorHandler.handleApiError(installError, 'install', { releaseName, namespace });
          throw new Error(`Failed to install app: ${standardError.message}`);
        }
      } else {
        // For non-404 errors during app check, handle and re-throw
        errorHandler.handleApiError(e, 'check-app', { releaseName, namespace, status: standardError.status });
        throw e; // Re-throw original error to be caught by outer handler
      }
    }
  } catch (projectError: unknown) {
    // Only handle if this is a new error, not a re-thrown error from inner catch
    if (projectError instanceof Error && projectError.message.includes('Failed to')) {
      // Already handled, just re-throw
      throw projectError;
    }
    const standardError = errorHandler.handleApiError(projectError, 'fetch-projects', { operation: 'fetch projects' });
    throw new Error(`Failed to fetch projects: ${standardError.message}`);
  }
  
  log('=== Completed createOrUpgradeApp ===');
  return { upgraded: false };
}

/* ====================== verify app appears and becomes ready ===================== */

export async function waitForAppInstall(
  $store: Dispatchable,
  clusterId: string,
  namespace: string,
  releaseName: string,
  timeoutMs = 90_000,
  isRetry = false
): Promise<AppCRD> {
  const errorHandler = createErrorHandler($store, 'RancherApps');
  const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/apis/catalog.cattle.io/v1/namespaces/${encodeURIComponent(namespace)}/apps/${encodeURIComponent(releaseName)}`;
  const start = Date.now();

  log('post-install: wait for App to appear', { clusterId, namespace, releaseName, timeoutMs });

  let initialObs    = -1;
  let everFound     = false; // true once the App CR has been observed at least once

  for (;;) {
    let app: any        = null;
    let is404           = false;

    try {
      const r = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
      app = (r?.data ?? r) || {};
    } catch (e: unknown) {
      const standardError = errorHandler.normalizeError(e);
      if (standardError.status === 404) {
        is404 = true;
      } else if (standardError.status) {
        log('post-install: early error (non-404)', standardError.status);
      }
    }

    if (is404 && everFound) {
      // The App CR existed but was deleted. This can happen after a successful Helm install
      // in some Rancher versions — verify by checking for the Helm release secret before
      // assuming failure.
      const helmSecretUrl =
        `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}/secrets` +
        `?labelSelector=owner%3Dhelm%2Cname%3D${encodeURIComponent(releaseName)}`;
      try {
        const secretsResp = await $store.dispatch('rancher/request', { url: helmSecretUrl, timeout: TIMEOUT_VALUES.CLUSTER });
        const secrets = secretsResp?.data?.items ?? secretsResp?.items ?? [];
        if (secrets.length > 0) {
          // Helm release exists — install succeeded, App CR was just cleaned up by Rancher.
          log('post-install: App CR gone but Helm release secret found — treating as success', { releaseName, namespace });
          return {} as AppCRD;
        }
      } catch {
        // ignore — fall through to the failure path below
      }
      throw new Error(
        `Helm install of "${releaseName}" failed and was rolled back. ` +
        `Check pod and job status in namespace "${namespace}" for details.`
      );
    }

    if (app) {
      everFound = true;
      const gen = app?.metadata?.generation ?? 0;
      const obs = app?.status?.observedGeneration ?? 0;
      const sum = app?.status?.summary || {};
      const state = sum?.state || app?.status?.conditions?.find((c: { type: string; status: string }) => c?.type === 'Ready')?.status || 'Unknown';

      if (initialObs < 0) initialObs = obs;

      console.log('[SUSE-AI] post-install: app peek', {
        gen, obs, initialObs, state, ns: namespace, name: releaseName,
        'metadata.state': app?.metadata?.state,
        'status.summary': sum,
        'status.conditions': app?.status?.conditions
      });

      if (obs >= gen) {
        const lowerState = (state || '').toLowerCase();
        // On retry/upgrade, wait for observedGeneration to increment to avoid reading stale
        // status from the previous operation — but skip the stale check when the App is
        // actively uninstalling, since that IS the current live state.
        const isStale = isRetry && obs <= initialObs && lowerState !== 'uninstalling';
        if (!isStale) {
          if (lowerState === 'failed' || lowerState === 'error') {
            const errMsg = app?.metadata?.state?.message
              || (typeof sum?.error === 'string' ? sum.error : null)
              || `Helm install failed (state: ${state})`;
            console.error('[SUSE-AI] post-install: app failed', { state, errMsg });
            throw new Error(errMsg);
          }
          // Only return for terminal success states; keep polling for transitional states.
          // "uninstalling" is included so the loop continues until the App CR disappears,
          // at which point the Helm release secret check below determines true outcome.
          const transitional = ['installing', 'pending', 'pending-install', 'pending-upgrade', 'pending-rollback', 'uninstalling'];
          if (!transitional.includes(lowerState)) {
            return app;
          }
        }
      }
    }

    if (Date.now() - start > timeoutMs) {
      const detail = everFound
        ? `App "${releaseName}" is still in a transitional state after ${Math.round(timeoutMs / 1000)}s — check pod status in namespace "${namespace}"`
        : `App "${releaseName}" did not appear in namespace "${namespace}" within ${Math.round(timeoutMs / 1000)}s — check ClusterRepo permissions`;
      throw new Error(detail);
    }
    await new Promise(r => setTimeout(r, 1500));
  }
}

export async function deleteApp($store: Dispatchable, clusterId: string, namespace: string, releaseName: string, _repoName?: string): Promise<void> {
  try {
    const url =
      `/k8s/clusters/${encodeURIComponent(clusterId)}` +
      `/v1/catalog.cattle.io.apps/${encodeURIComponent(namespace)}/${encodeURIComponent(releaseName)}?action=uninstall`;

    await $store.dispatch('rancher/request', {
      url,
      method: 'POST',
      data: { timeout: '600s' },
      timeout: TIMEOUT_VALUES.MUTATION
    });
    await new Promise(resolve => setTimeout(resolve, 5000));
    log('App CRD deleted');
  } catch (e: unknown) {
    const errorMsg = handleSimpleError(e, 'Failed to delete app');
    log('Failed to delete app:', errorMsg);
    throw e;
  }
}

/* ============================ discovery (manage) ============================ */

export async function listCatalogApps($store: Dispatchable, clusterId: string): Promise<AppCRD[]> {
  const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/apis/catalog.cattle.io/v1/apps?limit=1000`;
  const res = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
  return res?.data?.items || res?.data || res?.items || [];
}

export const SYSTEM_NAMESPACE_PREFIXES = [
  'c-', 'p-', 'kube-', 'cattle-', 'rancher', 'longhorn-',
  'fleet-', 'cluster-fleet-', 'system-', 'istio-',
  'neuvector', 'ingress-', 'cert-manager',
];

export async function listNamespaces($store: Dispatchable, clusterId: string): Promise<string[]> {
  const url = clusterId === 'local'
    ? '/api/v1/namespaces?limit=5000'
    : `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces?limit=5000`;
  const res = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
  const items = res?.data?.items || res?.data || res?.items || [];

  return (items || []).map((n: NamespaceResource) => n?.metadata?.name).filter((n: string) => !!n);
}

export async function fetchUserNamespaces(
  $store: Dispatchable,
  suggestedDefault: string
): Promise<Array<{ label: string; value: string }>> {
  try {
    const clusters = await getClusters($store);
    const allNs = new Set<string>();
    await Promise.all(clusters.map(async (cluster) => {
      try {
        const nsList = await listNamespaces($store, cluster.id);
        nsList.forEach(ns => allNs.add(ns));
      } catch {}
    }));
    const sorted = [...allNs]
      .filter(ns => !SYSTEM_NAMESPACE_PREFIXES.some(p => ns.startsWith(p)))
      .sort();
    if (!sorted.includes(suggestedDefault)) sorted.unshift(suggestedDefault);
    return sorted.map(ns => ({ label: ns, value: ns }));
  } catch {
    return [{ label: suggestedDefault, value: suggestedDefault }];
  }
}

async function listNsHelmSecrets($store: Dispatchable, clusterId: string, ns: string): Promise<HelmSecret[]> {
  const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(ns)}/secrets?labelSelector=owner%3Dhelm`;
  const res = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
  return res?.data?.items || res?.data || [];
}

// Removed listNsHelmConfigMaps - Helm v3+ uses Secrets exclusively (not ConfigMaps)
// ConfigMaps were only used by Helm v2 (deprecated)

function extractHelmRelease(obj: HelmSecret): HelmReleaseInfo {
  const meta = obj?.metadata || {};
  const labels = meta?.labels || {};
  const ann    = meta?.annotations || {};
  const release  =
        labels.name
     || (meta?.name && (meta.name.match(/^sh\.helm\.release\.v1\.(.+)\.v\d+$/)?.[1]))
     || labels['app.kubernetes.io/instance']
     || ann['meta.helm.sh/release-name']
     || '';
  const chartLabel = labels.chart || ann['helm.sh/chart'] || '';
  const chartBase  = chartLabel ? chartLabel.replace(/-\d+\.\d+\.\d+(?:[-+].*)?$/, '') : '';
  const verMatch   = chartLabel.match(/-(\d+\.\d+\.\d+(?:[-+].*)?)$/);
  const version    = verMatch ? verMatch[1] : '';
  return { release, chartBase, version };
}

type FoundInfo = { release: string; namespace: string; chartName?: string; version?: string; clusters: string[] };

export async function discoverExistingInstall(
  $store: Dispatchable,
  slug: string,
  chartNameGuess?: string,
  preferClusterId?: string
): Promise<FoundInfo | null> {
  const clusters = await getClusters($store);
  const order = [
    ...(preferClusterId ? clusters.filter(c => c.id === preferClusterId) : []),
    ...clusters.filter(c => !preferClusterId || c.id !== preferClusterId)
  ];

  type ClusterMatch = { clusterId: string; release: string; namespace: string; chartName?: string; version?: string };

  const searchCluster = async (c: ClusterInfo): Promise<ClusterMatch | null> => {
    // 1) Rancher Apps
    try {
      const apps = await listCatalogApps($store, c.id);
      for (const a of apps) {
        const meta  = a?.metadata || {};
        const spec  = a?.spec || {};
        const chart = spec?.chart?.metadata?.name || spec?.chartName || '';
        const ver   = spec?.chart?.metadata?.version || spec?.version || '';
        const rel   = meta?.name || '';
        const ns    = meta?.namespace || '';
        const hit = matchesSlug(chart, slug, chartNameGuess) || matchesSlug(rel, slug, chartNameGuess);
        if (hit) return { clusterId: c.id, release: rel, namespace: ns, chartName: chart, version: ver };
      }
    } catch { /* ignore */ }

    // 2) Helm v3 storage - cluster-wide search (optimized)
    try {
      const clusterWideUrl = `/k8s/clusters/${encodeURIComponent(c.id)}/api/v1/secrets?labelSelector=owner=helm&limit=500`;
      try {
        const response = await $store.dispatch('rancher/request', { url: clusterWideUrl, timeout: TIMEOUT_VALUES.CLUSTER });
        const allHelmSecrets = response?.data?.items || [];
        for (const s of allHelmSecrets) {
          const ns = s?.metadata?.namespace || '';
          const { release, chartBase, version } = extractHelmRelease(s);
          const hit = (release && matchesSlug(release, slug, chartNameGuess)) ||
                      (chartBase && matchesSlug(chartBase, slug, chartNameGuess));
          if (hit) return { clusterId: c.id, release: release || slug, namespace: ns, chartName: chartBase || slug, version: version || '' };
        }
      } catch {
        // Fallback to per-namespace search if cluster-wide search fails (RBAC restrictions)
        console.log('[SUSE-AI] Cluster-wide secret search not available, using per-namespace fallback');
        const nss = await listNamespaces($store, c.id);
        const nsResults = await Promise.allSettled(
          nss.map(ns => listNsHelmSecrets($store, c.id, ns).then(secs => ({ ns, secs })))
        );
        for (const r of nsResults) {
          if (r.status !== 'fulfilled') continue;
          const { ns, secs } = r.value;
          for (const s of secs) {
            const { release, chartBase, version } = extractHelmRelease(s);
            const hit = (release && matchesSlug(release, slug, chartNameGuess)) ||
                        (chartBase && matchesSlug(chartBase, slug, chartNameGuess));
            if (hit) return { clusterId: c.id, release: release || slug, namespace: ns, chartName: chartBase || slug, version: version || '' };
          }
        }
      }
    } catch { /* ignore */ }

    return null;
  };

  const results = await Promise.allSettled(order.map(searchCluster));
  const matches: ClusterMatch[] = [];
  for (const r of results) {
    if (r.status === 'fulfilled' && r.value !== null) matches.push(r.value);
  }

  if (matches.length === 0) return null;

  const canonical = matches.find(m => m.clusterId === preferClusterId) || matches[0];
  return {
    release:   canonical.release,
    namespace: canonical.namespace,
    chartName: canonical.chartName,
    version:   canonical.version,
    clusters:  matches.map(m => m.clusterId)
  };
}

/* =========================== charts: index + versions =========================== */

async function getRepoIndexLink($store: Dispatchable, repoName: string): Promise<string | null> {
  const found = await getClusterContext($store, { repoName: repoName});
  if (!found) {
    logger.warn(`ClusterRepo "${repoName}" not found in any cluster`);
    return null;
  }
  const { baseApi } = found
  try {
    const repo = encodeURIComponent(repoName);

    const url = `${baseApi}/catalog.cattle.io.clusterrepos/${repo}`;
    const res  = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.READ });

    const link = res?.data?.links?.index || res?.links?.index;
    log('repo index link:', link);
    return link || null;
  } catch {
    return null;
  }
}

async function getRepoIndex($store: Dispatchable, repoName: string): Promise<RepositoryIndex | null> {

  const indexLink = await getRepoIndexLink($store, repoName);
  if (!indexLink) return null;

  const res = await $store.dispatch('rancher/request', { url: indexLink, timeout: TIMEOUT_VALUES.READ });
  const payload = (res?.data ?? res);
  dbg('index payload', payload);
  if (typeof payload === 'string') return yaml.load(payload) as RepositoryIndex | null;
  if (payload && typeof payload === 'object' && 'entries' in payload) return payload as RepositoryIndex;
  if (payload && typeof payload === 'object' && 'data' in payload && payload.data && typeof payload.data === 'object' && 'entries' in payload.data) return payload.data as RepositoryIndex;
  return null;
}

export async function findChartInRepo(
  $store: Dispatchable,
  _repoClusterId: string,
  repoName: string,
  slug: string
): Promise<{ chartName: string; version: string } | null> {
  const index = await getRepoIndex($store, repoName);
  const names = index?.entries ? Object.keys(index.entries) : [];
  const match = names.find((n: string) => sameName(n, slug));
  if (match && index) {
    const latest = filterAndSortVersions((index.entries[match] || []).map((v: { version: string }) => v.version))[0];
    if (latest) return { chartName: match, version: latest };
  }
  return null;
}

export async function listChartVersions(
  $store: Dispatchable,
  _repoClusterId: string,
  repoName: string,
  chartName: string
): Promise<string[]> {
  const index = await getRepoIndex($store, repoName);
  const names = index?.entries ? Object.keys(index.entries) : [];
  const match = names.find((n: string) => sameName(n, chartName));
  if (match && index) {
    const out = filterAndSortVersions((index.entries[match] || []).map((v: { version: string }) => v.version));
    log('listChartVersions via index:', { chart: match, count: out.length });
    return out;
  }
  return [];
}

/* ======================= values.yaml extraction (robust) ======================= */

function decodeMaybeB64(s?: string): string {
  if (!s || typeof s !== 'string') return '';
  try {
    const t = atob(s.replace(/\s+/g, ''));
    if (/[:\n]/.test(t)) return t; // looks like YAML
  } catch {}
  return s;
}
function textFromFileEntry(v: FileEntry): string {
  if (!v) return '';
  if (typeof v === 'string') return decodeMaybeB64(v);
  if (typeof v === 'object') {
    const candidates = [v.content, v.contents, v.data, v.base64, v.value, v.Value, v.text];
    for (const c of candidates) if (typeof c === 'string' && c) return decodeMaybeB64(c);
  }
  return '';
}
// Note: Complex file fetching functions removed - now handled by ChartValuesService


export async function fetchChartYaml(
  $store: Dispatchable,
  repoName: string,
  chartName: string,
  version: string
): Promise<any | null> {
  const found = await getClusterContext($store, { repoName });

  if (!found) {
    logger.warn(`ClusterRepo "${repoName}" not found in any cluster`);
    return null;
  }

  const { baseApi } = found;
  const repoPath = `${baseApi}/catalog.cattle.io.clusterrepos/${encodeURIComponent(repoName)}`;
  const chartParams = `chartName=${encodeURIComponent(chartName)}&version=${encodeURIComponent(version)}`;

  // Try 1: ?link=files approach (gets all chart files)
  try {
    const response = await $store.dispatch('rancher/request', {
      url: `${repoPath}?link=files&${chartParams}`,
      timeout: TIMEOUT_VALUES.READ
    });
    const filesData = response?.data ?? response;

    if (filesData && typeof filesData === 'object') {
      for (const key of Object.keys(filesData)) {
        if (key.toLowerCase().endsWith('chart.yaml')) {
          const text = textFromFileEntry(filesData[key]);
          if (text && text.includes(':')) {
            return yaml.load(text);
          }
        }
      }
    }
  } catch { /* continue to next method */ }

  // Try 2: ?link=file approach (direct file fetch)
  try {
    const response = await $store.dispatch('rancher/request', {
      url: `${repoPath}?link=file&${chartParams}&name=${encodeURIComponent('Chart.yaml')}`,
      timeout: TIMEOUT_VALUES.READ
    });
    const text = textFromFileEntry(response?.data ?? response);

    if (text && text.includes(':')) {
      return yaml.load(text);
    }
  } catch { /* continue to next method */ }

  // Try 3: ?link=chart tar.gz approach (most reliable for OCI repos)
  try {
    const response = await $store.dispatch('rancher/request', {
      url: `${repoPath}?link=chart&${chartParams}`,
      responseType: 'arraybuffer',
      headers: { Accept: 'application/gzip, application/x-gzip, application/octet-stream' },
      timeout: TIMEOUT_VALUES.MEDIUM
    });

    const buffer = response?.data ?? response;
    if (buffer instanceof ArrayBuffer) {
      const text = await extractFileFromTarGz(buffer, 'chart.yaml');
      if (text && text.includes(':')) {
        return yaml.load(text);
      }
    }
  } catch { /* all methods failed */ }

  logger.warn(`Failed to fetch Chart.yaml for ${chartName}@${version} from ${repoName}`);
  return null;
}

export async function fetchChartDefaultValues(
  $store: Dispatchable,
  _repoClusterId: string,
  repoName: string,
  chartName: string,
  version: string
): Promise<string> {
  // Use simplified ChartValuesService instead of complex fallback chains
  const chartValuesService = createChartValuesService($store);
  return chartValuesService.getDefaultValues(repoName, chartName, version);
}

export async function fetchChartArchiveSize(
  $store: Dispatchable,
  _repoClusterId: string,
  repoName: string,
  chartName: string,
  version: string
): Promise<number | null> {
  const chartValuesService = createChartValuesService($store);
  return chartValuesService.getChartArchiveSize(repoName, chartName, version);
}

// Note: Complex tar.gz processing removed - now handled by ChartValuesService

/* ================== NEW: helpers for repo discovery & helm installs ============== */

export async function listClusterRepos($store: Dispatchable): Promise<ClusterResource[]> {
    const res = await $store.dispatch('rancher/request', {
    url: '/k8s/clusters/local/apis/catalog.cattle.io/v1/clusterrepos?limit=1000',
    timeout: TIMEOUT_VALUES.READ
  });
    return res?.data?.items || res?.data || res?.items || [];
}

export async function inferClusterRepoForChart(
  $store: Dispatchable,
  chartName: string,
  preferVersion?: string
): Promise<string | null> {
  const repos = await listClusterRepos($store);
  let best: string | null = null;

  for (const r of repos) {
    const name = r?.metadata?.name;
    if (!name) continue;
    try {
      const index = await getRepoIndex($store, name);
      const entries = index?.entries || {};
      const foundKey = Object.keys(entries).find((k) => sameName(k, chartName));
      if (!foundKey) continue;

      if (preferVersion) {
        const versions: string[] = (entries[foundKey] || []).map((e: { version: string }) => e?.version).filter(Boolean);
        if (versions.includes(preferVersion)) return name; // perfect match
      }
      if (!best) best = name; // fallback: chart exists, version may differ
    } catch { /* ignore this repo */ }
  }
  return best;
}


function clusterRepoNameFromUrl(repoUrl: string): string {
  const KNOWN: Record<string, string> = {
    [APP_COLLECTION_REPO_URL]:    'application-collection',
    [SUSE_REGISTRY_REPO_URL]:     'suse-ai-registry',
    [NVIDIA_REPO_URL]:            'nvidia',
    [NVIDIA_BLUEPRINT_REPO_URL]:  'nvidia-blueprints',
  };
  return KNOWN[repoUrl] ?? repoUrl
    .replace(/^oci:\/\//, '')
    .replace(/[^a-z0-9]+/gi, '-')
    .toLowerCase()
    .replace(/^-|-$/g, '');
}

async function upsertBasicAuthSecret(
  $store: Dispatchable,
  namespace: string,
  name: string,
  username: string,
  password: string,
): Promise<void> {
  const secretBody = {
    apiVersion: 'v1',
    kind:       'Secret',
    metadata:   { name, namespace },
    type:       'kubernetes.io/basic-auth',
    stringData: { username, password },
  };
  try {
    const res = await $store.dispatch('rancher/request', {
      url:     `/k8s/clusters/local/api/v1/namespaces/${namespace}/secrets/${name}`,
      timeout: TIMEOUT_VALUES.CLUSTER,
    });
    const existing = res?.data || res;
    await $store.dispatch('rancher/request', {
      url:    `/k8s/clusters/local/api/v1/namespaces/${namespace}/secrets/${name}`,
      method: 'PUT',
      data:   { ...secretBody, metadata: { ...secretBody.metadata, resourceVersion: existing?.metadata?.resourceVersion } },
      timeout: TIMEOUT_VALUES.MUTATION,
    });
  } catch {
    await $store.dispatch('rancher/request', {
      url:    `/k8s/clusters/local/api/v1/namespaces/${namespace}/secrets`,
      method: 'POST',
      data:   secretBody,
      timeout: TIMEOUT_VALUES.MUTATION,
    });
  }
}

export async function ensureClusterRepo(
  $store: Dispatchable,
  ociUrl: string,
  credentials?: { username: string; password: string },
): Promise<string> {
  const repos = await listClusterRepos($store);
  const existing = repos.find((r: any) => (r?.spec?.url || r?.spec?.ociRepo || '') === ociUrl);
  const name = existing?.metadata?.name || clusterRepoNameFromUrl(ociUrl);

  let clientSecret: { name: string; namespace: string } | undefined;
  if (credentials) {
    const secretName = `${name}-auth`;
    await upsertBasicAuthSecret($store, 'cattle-system', secretName, credentials.username, credentials.password);
    clientSecret = { name: secretName, namespace: 'cattle-system' };
  }

  if (existing) {
    // If we now have credentials and the repo didn't before, patch it
    if (clientSecret && existing.spec?.clientSecret?.name !== clientSecret.name) {
      const res = await $store.dispatch('rancher/request', {
        url:     `/k8s/clusters/local/apis/catalog.cattle.io/v1/clusterrepos/${name}`,
        timeout: TIMEOUT_VALUES.CLUSTER,
      });
      const full = res?.data || res;
      await $store.dispatch('rancher/request', {
        url:    `/k8s/clusters/local/apis/catalog.cattle.io/v1/clusterrepos/${name}`,
        method: 'PUT',
        data:   { ...full, spec: { ...full.spec, clientSecret } },
        timeout: TIMEOUT_VALUES.MUTATION,
      });
    }
    return name;
  }

  // Create the ClusterRepo
  const spec: any = { url: ociUrl };
  if (clientSecret) spec.clientSecret = clientSecret;
  await $store.dispatch('rancher/request', {
    url:    '/k8s/clusters/local/apis/catalog.cattle.io/v1/clusterrepos',
    method: 'POST',
    data:   { apiVersion: 'catalog.cattle.io/v1', kind: 'ClusterRepo', metadata: { name }, spec },
    timeout: TIMEOUT_VALUES.MUTATION,
  });

  // Poll until indexed (up to 60 s)
  for (let i = 0; i < 30; i++) {
    await new Promise(r => setTimeout(r, 2000));
    try {
      const fresh = await listClusterRepos($store);
      const created = fresh.find((r: any) => r?.metadata?.name === name);
      if (!created) continue;
      const ready = (created?.status?.conditions || []).some((c: any) =>
        ['OCIDownloaded', 'Downloaded', 'FollowerDownloaded'].includes(c.type) && c.status === 'True'
      );
      if (ready) return name;
    } catch { /* retry */ }
  }
  return name;
}


async function findHelmReleaseObjects(
  $store: Dispatchable,
  clusterId: string,
  namespace: string,
  releaseName: string
): Promise<{ secret?: HelmSecret }> {
  const errorHandler = createErrorHandler($store, 'RancherApps');

  try {
    // First try to find the latest version of the Helm release secret
    // List all secrets to find the highest version number
    try {
      const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}/secrets`;
      const response = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
      const secrets = response?.data || response?.items || response || [];

      // Find all Helm release secrets for this release
      const helmSecrets = secrets.filter((secret: HelmSecret) =>
        secret.metadata?.name?.startsWith(`sh.helm.release.v1.${releaseName}.v`)
      );

      if (helmSecrets.length > 0) {
        // Sort by version number (extract vN from the name)
        const sortedSecrets = helmSecrets.sort((a: HelmSecret, b: HelmSecret) => {
          const aVersion = parseInt(a.metadata.name.split('.v').pop() || '0');
          const bVersion = parseInt(b.metadata.name.split('.v').pop() || '0');
          return bVersion - aVersion; // Descending order (latest first)
        });

        const latestSecret = sortedSecrets[0];
        const secretName = latestSecret.metadata.name;

        // Now fetch the latest secret with includeHelmData=true
        const detailUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(secretName)}?exclude=metadata.managedFields&includeHelmData=true`;
        const secret = await $store.dispatch('rancher/request', { url: detailUrl, timeout: TIMEOUT_VALUES.CLUSTER });

        if (secret?.data?.release) {
          console.log('[SUSE-AI] Found Helm secret with includeHelmData=true:', secretName);
          return { secret };
        }
      }
    } catch (e: unknown) {
      const errorMsg = handleSimpleError(e, 'Failed to find latest Helm secret');
      console.log('[SUSE-AI] Failed to find Helm secret via list+filter:', errorMsg);
    }

    return {};
  } catch (error) {
    console.warn(`[SUSE-AI] Failed to find Helm release ${releaseName}:`, error);
    return {};
  }
}

export async function getInstalledHelmDetails(
  $store: Dispatchable,
  clusterId: string,
  namespace: string,
  releaseName: string
): Promise<{ chartName: string; chartVersion: string; values: Record<string, unknown> }> {
  const { secret } = await findHelmReleaseObjects($store, clusterId, namespace, releaseName);
  const { chartBase, version } = secret ? extractHelmRelease(secret) : { chartBase: undefined, version: undefined };

  let values: Record<string, unknown> = {};
  let chartVersion = version || '';
  let chartName = chartBase || releaseName;

  // First check if we have the Helm data directly (from includeHelmData=true)
  if (secret?.data?.release && typeof secret.data.release === 'object' && 'config' in secret.data.release) {
    const release = secret.data.release as {
      values?: Record<string, unknown>;
      config?: Record<string, unknown>;
      chart?: {
        values?: Record<string, unknown>;
        metadata?: {
          name?: string;
          version?: string;
        };
      };
      info?: Record<string, unknown>;
    };

    // Extract chart version from release metadata (most reliable source)
    if (release.chart?.metadata?.version) {
      chartVersion = release.chart.metadata.version;
    }

    // Extract chart name if available
    if (release.chart?.metadata?.name) {
      chartName = release.chart.metadata.name;
    }

    // Priority order for values retrieval:
    // 1. release.values - User-provided values (what we want for "Manage" workflow)
    // 2. release.config - Merged values (defaults + user values)
    // 3. release.chart.values - Chart default values

    // For Manage workflow, we want the complete values structure (defaults + customizations)
    // This matches what native Rancher shows: full schema with applied values
    if (release.chart?.values && Object.keys(release.chart.values).length > 0) {
      // Start with chart defaults (complete structure)
      values = JSON.parse(JSON.stringify(release.chart.values));

      // Merge user customizations on top
      if (release.config && Object.keys(release.config).length > 0) {
        values = deepMerge(values, release.config);
      } else if (release.values && Object.keys(release.values).length > 0) {
        values = deepMerge(values, release.values);
      }
    } else if (release.config && Object.keys(release.config).length > 0) {
      // Fallback: use config if no chart defaults available
      values = release.config;
    } else if (release.values && Object.keys(release.values).length > 0) {
      // Fallback: use user values if nothing else available
      values = release.values;
    }
  } else {
    // This path should not be reached when using includeHelmData=true
    console.warn('[SUSE-AI] Helm release data is not in expected object format. Check API response.');
  }

  return {
    chartName,
    chartVersion,
    values
  };
}

/* ======================== image pull secret helpers ======================== */

// helper: list secrets in a namespace (used to find already-created -dockercfg)
async function listNsSecrets(
  $store: Dispatchable,
  clusterId: string,
  namespace: string
): Promise<RegistrySecret[]> {
  const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}/secrets?limit=5000`;
  const res = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
  return (res?.data?.items || res?.data || []) as RegistrySecret[];
}

export async function ensureRegistrySecret(
  $store: Dispatchable,
  clusterId: string,
  namespace: string,
  registryHost: string,
  desiredName: string,
  username: string,
  password: string
): Promise<string> {
  const errorHandler = createErrorHandler($store, 'RancherApps');
  const asB64 = (s: string) => (typeof btoa === 'function' ? btoa(s) : Buffer.from(s).toString('base64'));
  const authB64 = asB64(`${username}:${password}`);

  const dockerCfgB64 = asB64(JSON.stringify({
    auths: {
      [registryHost]: { auth: authB64, username, password }
    }
  }));

  // Canonical base name like <clusterrepo-auth-xxxxx>-dockercfg
  const base = /^clusterrepo-auth-/.test(desiredName)
    ? `${desiredName}-dockercfg`
    : (desiredName.endsWith('-dockercfg') ? desiredName : `${desiredName}-dockercfg`);

  const baseUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}/secrets`;
  const getUrl  = (n: string) => `${baseUrl}/${encodeURIComponent(n)}`;

  log('ensureRegistrySecret begin ', { clusterId, namespace, registryHost, desiredName, candidates: [base] });

  // 0) If an existing usable secret already exists with the base prefix, reuse it (avoid races)
  try {
    const all = await listNsSecrets($store, clusterId, namespace);
    log('ensureRegistrySecret: List all secrets in the namespace', {secrets: [all]});
    const match = all.find((s: RegistrySecret) => s?.metadata?.name?.startsWith(base) &&
      s?.type === 'kubernetes.io/dockerconfigjson' &&
      typeof s?.data?.['.dockerconfigjson'] === 'string' &&
      s?.data?.['.dockerconfigjson']?.length > 0);

    if (match?.metadata?.name) {
      log('ensureRegistrySecret: reusing existing dockerconfigjson', { name: match.metadata.name });
      return match.metadata.name;
    }
  } catch (e) {
    const standardError = errorHandler.normalizeError(e);
    log('ensureRegistrySecret: list secrets failed (continuing)', standardError.status);
  }

  log('ensureRegistrySecret: No existing usable secret found');

  // 1) Try the canonical base name first (create if missing; do NOT delete anything anymore)
  try {
    const cur = await $store.dispatch('rancher/request', { url: getUrl(base), timeout: TIMEOUT_VALUES.CLUSTER })
      .catch((e: unknown) => {
        const standardError = errorHandler.normalizeError(e);
        return standardError.status === 404 ? null : Promise.reject(e);
      });

    if (cur) {
      const s = (cur?.data ?? cur) || {};
      if (s?.type === 'kubernetes.io/dockerconfigjson' && typeof s?.data?.['.dockerconfigjson'] === 'string') {
        log('secret GET', `${base} → exists & usable`);
        return base;
      }
      // wrong type → fall through to unique name to avoid fights with other controllers
      log('secret GET', `${base} → exists but wrong type; will create unique`);
    } else {
      log('secret GET', `${base} → 404`);
      // create canonical
      log('secret create POST → ', { clusterId, namespace, name: base });
      await $store.dispatch('rancher/request', {
        url: baseUrl, method: 'POST',
        data: {
          apiVersion: 'v1',
          kind: 'Secret',
          metadata: { name: base, namespace },
          type: 'kubernetes.io/dockerconfigjson',
          data: { '.dockerconfigjson': dockerCfgB64 }
        },
        timeout: TIMEOUT_VALUES.MUTATION
      });
      // Non-blocking readiness probe (best-effort)
      try { await waitForSecretReady($store, clusterId, namespace, base, 10_000, true); } catch {}
      return base;
    }
  } catch (e: unknown) {
    const standardError = errorHandler.normalizeError(e);
    log('secret create(base) failed (continuing with unique)', standardError.status, standardError.message);
  }

  // 2) Create a unique name if base is unsuitable or managed by someone else
  const unique = `${base}-${Math.random().toString(36).slice(2, 7)}`;
  log('secret create POST → ', { clusterId, namespace, name: unique });
  await $store.dispatch('rancher/request', {
    url: baseUrl, method: 'POST',
    data: {
      apiVersion: 'v1',
      kind: 'Secret',
      metadata: { name: unique, namespace },
      type: 'kubernetes.io/dockerconfigjson',
      data: { '.dockerconfigjson': dockerCfgB64 }
    },
    timeout: TIMEOUT_VALUES.MUTATION
  });

  try { await waitForSecretReady($store, clusterId, namespace, unique, 10_000, true); } catch (e: unknown) {
    const errorMsg = handleSimpleError(e, 'Secret readiness timeout');
    log('secret readiness timed out (continuing anyway)', { name: unique, err: errorMsg });
  }

  return unique;
}

export async function listServiceAccounts(
  $store: Dispatchable,
  clusterId: string,
  namespace: string
): Promise<string[]> {
  const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}/serviceaccounts?limit=5000`;
  const res = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
  const items = (res?.data?.items || res?.data || []) as ServiceAccount[];
  return items.map(sa => sa?.metadata?.name).filter(Boolean);
}

export async function ensureServiceAccountPullSecret(
  $store: Dispatchable,
  clusterId: string,
  namespace: string,
  saName: string,
  secretName: string
) {
  const base = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}/serviceaccounts`;
  const url  = `${base}/${encodeURIComponent(saName)}`;

  try {
    const cur = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
    const sa  = (cur?.data ?? cur) || {};
    const rv  = sa?.metadata?.resourceVersion;

    const orig = Array.isArray(sa.imagePullSecrets) ? sa.imagePullSecrets.slice() : [];
    const has  = orig.some((e: { name?: string }) => e?.name === secretName);
    const next = has ? orig : [...orig, { name: secretName }];

    await $store.dispatch('rancher/request', {
      url, method: 'PUT',
      data: {
        apiVersion: 'v1',
        kind: 'ServiceAccount',
        metadata: { name: saName, namespace, resourceVersion: rv },
        secrets: sa.secrets,
        automountServiceAccountToken: sa.automountServiceAccountToken,
        imagePullSecrets: next
      },
      timeout: TIMEOUT_VALUES.MUTATION
    });
  } catch (e) {
    try { console.warn('[SUSE-AI] could not update ServiceAccount imagePullSecrets', { namespace, saName, e }); } catch {}
  }
}

export async function ensurePullSecretOnAllSAs(
  $store: Dispatchable,
  clusterId: string,
  namespace: string,
  secretName: string
) {
  const sas = await listServiceAccounts($store, clusterId, namespace);
  for (const saName of sas) {
    try {
      await ensureServiceAccountPullSecret($store, clusterId, namespace, saName, secretName);
    } catch (e) {
      try { console.warn('[SUSE-AI] SA attach failed', { namespace, saName, e }); } catch {}
    }
  }
}

export async function ensureRegistrySecretSimple(
  $store: Dispatchable,
  clusterId: string,
  namespace: string,
  registryHost: string,
  desiredName: string,
  username: string,
  password: string
): Promise<string> {
  const errorHandler = createErrorHandler($store, 'RancherApps');

  logger.debug('Ensuring registry secret (simple)', {
    component: 'RancherApps',
    data: { clusterId, namespace, registryHost, desiredName }
  });

  // 1. Prepare secret data
  const asB64 = (s: string) => (typeof btoa === 'function' ? btoa(s) : Buffer.from(s).toString('base64'));
  const authB64 = asB64(`${username}:${password}`);
  const dockerCfgB64 = asB64(JSON.stringify({
    auths: {
      [registryHost]: { auth: authB64, username, password }
    }
  }));

  // 2. Define a predictable name based on the desiredName
  const secretName = `suse-ai-pull-secret-${desiredName.replace(/[^a-z0-9-]/g, '-')}`;

  logger.debug('Creating secret with predictable name', {
    component: 'RancherApps',
    data: { secretName }
  });

  const secretPayload = {
    apiVersion: 'v1',
    kind: 'Secret',
    metadata: { name: secretName, namespace } as any,
    type: 'kubernetes.io/dockerconfigjson',
    data: { '.dockerconfigjson': dockerCfgB64 }
  };

  const baseUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}/secrets`;
  const secretUrl = `${baseUrl}/${encodeURIComponent(secretName)}`;

  try {
    // 3. Try to get the existing secret to see if we need to update or create
    const existing = await $store.dispatch('rancher/request', { url: secretUrl, timeout: TIMEOUT_VALUES.CLUSTER });
    const resourceVersion = existing?.data?.metadata?.resourceVersion;

    // 4. If it exists, update it (PUT)
    logger.debug('Secret exists, updating', {
      component: 'RancherApps',
      data: { secretName }
    });

    if (resourceVersion) {
      secretPayload.metadata.resourceVersion = resourceVersion;
    }

    await $store.dispatch('rancher/request', {
      url: secretUrl,
      method: 'PUT',
      data: secretPayload,
      timeout: TIMEOUT_VALUES.MUTATION
    });

    logger.info('Secret updated successfully', {
      component: 'RancherApps',
      data: { secretName }
    });

  } catch (e: any) {
    const standardError = errorHandler.normalizeError(e);

    if (standardError.status === 404) {
      // 5. If it doesn't exist (404), create it (POST)
      logger.debug('Secret does not exist, creating', {
        component: 'RancherApps',
        data: { secretName }
      });

      await $store.dispatch('rancher/request', {
        url: baseUrl,
        method: 'POST',
        data: secretPayload,
        timeout: TIMEOUT_VALUES.MUTATION
      });

      logger.info('Secret created successfully', {
        component: 'RancherApps',
        data: { secretName }
      });
    } else if (standardError.status === 409) {
      // Conflict on update means it was created in the meantime, or we don't have resourceVersion. This is fine.
      logger.debug('Conflict updating secret, assuming up to date', {
        component: 'RancherApps',
        data: { secretName }
      });
    } else {
      // 6. For any other error (e.g., conflict on update, permissions), re-throw it.
      logger.error('Failed to ensure secret', e, {
        component: 'RancherApps',
        data: { secretName }
      });
      throw new Error(`Failed to create or update secret ${secretName}: ${standardError.message || 'Unknown error'}`);
    }
  }

  // 7. Return the predictable name
  return secretName;
}

export async function waitForSecretReady(
  $store: Dispatchable,
  clusterId: string,
  namespace: string,
  name: string,
  timeoutMs = 20_000,
  assumeReadyOn403_404 = true
) {
  const errorHandler = createErrorHandler($store, 'RancherApps');
  const start = Date.now();
  const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}/secrets/${encodeURIComponent(name)}`;

  for (;;) {
    try {
      const r = await $store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
      const s = (r?.data ?? r) || {};
      const ok = s?.type === 'kubernetes.io/dockerconfigjson' &&
                 typeof s?.data?.['.dockerconfigjson'] === 'string' &&
                 s.data['.dockerconfigjson'].length > 0;
      if (ok) return;
    } catch (e: unknown) {
      const standardError = errorHandler.normalizeError(e);

      if (assumeReadyOn403_404 && (standardError.status === 403 || standardError.status === 404)) {
        log('secret readiness probe blocked by RBAC/404; assuming ready', { ns: namespace, name });
        return;
      }
      // else: keep polling
    }
    if (Date.now() - start > timeoutMs) {
      throw new Error(`Timed out waiting for secret ${namespace}/${name} to be readable`);
    }
    await new Promise(r => setTimeout(r, 700));
  }
}
