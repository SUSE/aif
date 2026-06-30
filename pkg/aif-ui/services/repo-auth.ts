import logger from '../utils/logger';
import { getClusterContext } from '../utils/cluster-operations';
import { TIMEOUT_VALUES } from '../utils/constants';

export interface RepoAuth { username: string; password: string; }
type SecretRef = string | { name?: string; namespace?: string } | null | undefined;

const norm = (u?: string) => (u || '').replace(/\/+$/, '');
const b64 = (s?: string) => { try { return s ? atob(s) : ''; } catch { return ''; } };

// -------------------- secret ref parsing + extraction --------------------

export function dockerconfigAuthKeyForHost(host?: string): string {
  const h = (host || '').toLowerCase().trim();
  if (!h || h === 'docker.io' || h === 'index.docker.io' || h.endsWith('.docker.com')) {
    return 'https://index.docker.io/v1/'; // canonical Hub key
  }
  return h;
}

// Generic host parser (supports oci:// and http(s)://)
function parseRegistryHost(url?: string): string {
  if (!url) return '';
  const u = url.trim();
  if (u.startsWith('oci://')) return u.slice('oci://'.length).split('/')[0] || '';
  try { return new URL(u).host || ''; } catch { return u.split('/')[0] || ''; }
}

function extractFromDockerCfg(sec: any): RepoAuth | null {
  const blob = sec?.data?.['.dockerconfigjson'] || sec?.data?.dockerconfigjson;
  if (!blob) return null;
  try {
    const cfg = JSON.parse(b64(blob)); const auths = cfg?.auths || cfg;
    for (const k of Object.keys(auths || {})) {
      const e = auths[k];
      if (e?.auth) {
        const pair = atob(e.auth); const i = pair.indexOf(':');
        if (i > 0) return { username: pair.slice(0, i), password: pair.slice(i + 1) };
      }
      if (e?.username && e?.password) return { username: e.username, password: e.password };
    }
  } catch {}
  return null;
}

function extract(sec: any): RepoAuth | null {
  const data = sec || {};

  // kubernetes.io/basic-auth
  const u = b64(data.username), p = b64(data.password);
  if (u && p) return { username: u, password: p };

  // single "auth" (b64 user:pass)
  if (data.auth) {
    try {
      const pair = atob(b64(data.auth)); const i = pair.indexOf(':');
      if (i > 0) return { username: pair.slice(0, i), password: pair.slice(i + 1) };
    } catch {}
  }

  // token-only
  if (data.token) {
    const t = b64(data.token);
    if (t) return { username: 'token', password: t };
  }

  // accessKey/secretKey
  const ak = b64(data.accessKey), sk = b64(data.secretKey);
  if (ak && sk) return { username: ak, password: sk };

  // dockerconfigjson
  return extractFromDockerCfg(sec);
}

/** Try multiple Rancher paths to get a Secret that actually contains `.data` */
async function fetchSecret(store: any, ns: string, name: string, baseApi: string | null) {
  if (!baseApi) {
    logger.warn(`fetchSecret: baseApi is null — skipping request for ${ns}/${name}`);
    return {};
  }
  
  try {
    const r3 = await store.dispatch('rancher/request', { url: `${baseApi}/secrets/${encodeURIComponent(ns)}/${encodeURIComponent(name)}`, timeout: TIMEOUT_VALUES.CLUSTER });
    const s3 = r3?.data || r3 || {};

    if (Object.keys(s3 || {}).length) return s3;
  } catch {}

  return {};
}

// -------------------- NEW: repo → host + secret + creds --------------------

export interface RepoInstallContext {
  /** The registry host derived from spec.url, e.g. "dp.apps.rancher.io" */
  registryHost: string;
  /** The name of the secret referenced by the ClusterRepo (in cattle-system) */
  secretName?: string;
  /** Basic auth (username/password) extracted from that secret */
  auth?: RepoAuth;
}

export interface RepoInstallContext {
  registryHost: string;
  secretName?: string;     // the repo's configured secret name (in cattle-system unless overridden)
  auth?: RepoAuth;         // parsed username/password
}

/**
 * Resolve creds + registry host for a specific ClusterRepo (by metadata.name).
 */
export async function getRepoAuthForClusterRepo(store: any, clusterRepoName: string): Promise<RepoInstallContext> {
  if (!clusterRepoName) throw new Error('ClusterRepo name is required');

  const found = await getClusterContext(store, { repoName: clusterRepoName });
  if (!found) {
    logger.warn(`ClusterRepo "${clusterRepoName}" not found in any cluster`);
    return {
      registryHost: '',
      secretName: undefined,
      auth: undefined
    };
  }

  const baseApi = found.baseApi;

  const url = `${baseApi}/catalog.cattle.io.clusterrepos/${encodeURIComponent(clusterRepoName)}`;
  const r   = await store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.READ });
  const repo = r?.data ?? r;
  if (!repo?.spec) throw new Error(`ClusterRepo ${clusterRepoName} not found`);

  const registryHost = parseRegistryHost(repo?.spec?.url) || 'docker.io';

  // Resolve the referenced secret, same logic as getDpRepoAuth()
  const ref =
    (repo?.spec?.clientSecret ? { name: repo.spec.clientSecret.name, namespace: repo.spec.clientSecret.namespace } : null)

  if (!ref) {
    return {
      registryHost,
      secretName: undefined,
      auth: undefined
    };
  } else {
    const sec = await fetchSecret(store, ref.namespace, ref.name, baseApi) || {};
    const auth = extract(sec);
    if (!auth) {
      const keys = Object.keys(sec?.data || {});
      throw new Error(`Credentials not found in ${ref.namespace}/${ref.name}. Found keys: ${keys.join(', ') || 'none'}.`);
    }
    return {
      registryHost,
      secretName: ref.name,
      auth
    };
  }
}

// Optional alias to satisfy any lingering import:
export const getRepoInstallContext = getRepoAuthForClusterRepo;
