import {
  MANAGEMENT_CLUSTER,
  OPERATOR_NAMESPACE,
  OPERATOR_SERVICE,
  OPERATOR_PORT,
} from './constants';

const CONFIG_NAMESPACE = 'cattle-ui-plugin-system';
const CONFIG_MAP_NAME  = 'aif-ui-config';

interface OperatorCache {
  namespace: string;
  service:   string;
}

let cache: OperatorCache | null = null;
let configMapFound = false;
let connectionError: string | null = null;
let checkPromise: Promise<void> | null = null;

function configMapUrl(): string {
  return `/k8s/clusters/${ MANAGEMENT_CLUSTER }/api/v1/namespaces/${ CONFIG_NAMESPACE }/configmaps/${ CONFIG_MAP_NAME }`;
}

/** Fetch the aif-ui-config ConfigMap and populate the in-memory cache.
 *  Falls back to hardcoded constants if the ConfigMap is missing or unreadable.
 *  Idempotent: subsequent calls are no-ops unless force=true. */
export async function loadOperatorConfig(force = false): Promise<void> {
  if (cache && !force) return;
  try {
    const res = await fetch(configMapUrl(), { headers: { Accept: 'application/json' } });
    if (res.ok) {
      const cm = await res.json();
      cache = {
        namespace: cm?.data?.operatorNamespace || OPERATOR_NAMESPACE,
        service:   cm?.data?.operatorService   || OPERATOR_SERVICE,
      };
      configMapFound = true;
      return;
    }
  } catch { /* fall through to defaults */ }
  cache = { namespace: OPERATOR_NAMESPACE, service: OPERATOR_SERVICE };
  configMapFound = false;
}

export function getOperatorNamespace(): string {
  return cache?.namespace ?? OPERATOR_NAMESPACE;
}

export function getOperatorService(): string {
  return cache?.service ?? OPERATOR_SERVICE;
}

export function getOperatorBaseUrl(): string {
  const ns  = getOperatorNamespace();
  const svc = getOperatorService();
  return `/k8s/clusters/${ MANAGEMENT_CLUSTER }/api/v1/namespaces/${ ns }/services/http:${ svc }:${ OPERATOR_PORT }/proxy`;
}

async function _runConnectionCheck(): Promise<void> {
  await loadOperatorConfig();
  const ns  = getOperatorNamespace();
  const url = `${ getOperatorBaseUrl() }/api/v1/settings`;

  try {
    const res = await fetch(url, { headers: { Accept: 'application/json' } });
    if (res.ok) return;

    if (res.status === 404) {
      const body = await res.json().catch(() => null);
      // Operator's own 404 (settings not yet configured) has {error, message} format.
      // A Rancher/k8s proxy 404 (service not found) has a different shape.
      if (body?.error) return;
    }

    connectionError = `Cannot connect to the SUSE AI operator in namespace "${ ns }".`;
  } catch { /* transient network error — ignore */ }
}

/** Check whether the operator is reachable. Runs at most once per session;
 *  subsequent calls return the cached promise immediately. Call with no await
 *  from product.ts init() for a background warm-up, then await it inside page
 *  fetch() hooks — the promise is shared so there is no duplicate request.
 *  Pass force=true (e.g. from a Retry button) to discard the cached result and
 *  re-run the check. */
export function checkOperatorConnection(force = false): Promise<void> {
  if (force) { connectionError = null; checkPromise = null; }
  if (!checkPromise) checkPromise = _runConnectionCheck();
  return checkPromise;
}

/** Error message set by checkOperatorConnection(), or null if reachable. */
export function getConnectionError(): string | null {
  return connectionError;
}

/** Return current resolved coordinates (for Settings page display). */
export function getOperatorConfig(): OperatorCache {
  return { namespace: getOperatorNamespace(), service: getOperatorService() };
}

export function isConfigMapFound(): boolean {
  return configMapFound;
}

export function invalidateOperatorConfig(): void {
  cache           = null;
  configMapFound  = false;
  connectionError = null;
  checkPromise    = null;
}

/** Write operator coordinates to the ConfigMap and refresh the in-memory cache.
 *  Tries PUT (update) first; falls back to POST (create) on 404 so it works
 *  for both container-based installs (ConfigMap pre-exists from Helm) and
 *  git-based installs (no chart deployed, ConfigMap created on first save). */
export async function saveOperatorConfig(namespace: string, service: string): Promise<void> {
  const url  = configMapUrl();
  const body = JSON.stringify({
    apiVersion: 'v1',
    kind:       'ConfigMap',
    metadata:   { name: CONFIG_MAP_NAME, namespace: CONFIG_NAMESPACE },
    data:       { operatorNamespace: namespace, operatorService: service },
  });
  const headers = { 'Content-Type': 'application/json', Accept: 'application/json' };

  let res = await fetch(url, { method: 'PUT', headers, body });

  if (res.status === 404) {
    // ConfigMap doesn't exist yet (git-based install, no Helm chart ran).
    res = await fetch(url.replace(`/${ CONFIG_MAP_NAME }`, ''), { method: 'POST', headers, body });
  }

  if (!res.ok) {
    const err = await res.json().catch(() => null);
    throw new Error(err?.message || `Failed to save operator config: ${ res.statusText }`);
  }

  invalidateOperatorConfig();
  await loadOperatorConfig();
}
