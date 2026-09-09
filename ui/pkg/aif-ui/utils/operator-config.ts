import {
  MANAGEMENT_CLUSTER,
  OPERATOR_NAMESPACE,
  OPERATOR_SERVICE,
  OPERATOR_PORT,
} from './constants';

const CONFIG_NAMESPACE = 'cattle-ui-plugin-system';
const CONFIG_MAP_NAME  = 'aif-ui-config';

interface OperatorCache {
  namespace:         string;
  service:           string;
  useStaticCatalog:  boolean;
}

export interface OperatorError extends Error {
  status: number;
  code:   string;
}

/** Parse a ConfigMap string value ("true"/"false") to boolean, using def when absent/empty. */
function parseConfigBool(v: unknown, def: boolean): boolean {
  if (v === undefined || v === null || v === '') return def;
  return String(v).toLowerCase() === 'true';
}

let cache: OperatorCache | null = null;
let loadPromise: Promise<void> | null = null;
let connectionError: string | null = null;
let checkPromise: Promise<void> | null = null;

function configMapUrl(): string {
  return `/k8s/clusters/${ MANAGEMENT_CLUSTER }/api/v1/namespaces/${ CONFIG_NAMESPACE }/configmaps/${ CONFIG_MAP_NAME }`;
}

/** Shared fetch wrapper for operator API calls. Handles 204 No Content, JSON error
 *  extraction, and attaches typed status/code fields to thrown errors. */
export async function operatorFetch(path: string, options: RequestInit = {}): Promise<any> {
  await loadOperatorConfig();
  const res = await fetch(`${ getOperatorBaseUrl() }${ path }`, {
    ...options,
    headers: {
      Accept: 'application/json',
      ...(options.body ? { 'Content-Type': 'application/json' } : {}),
      ...(options.headers || {}),
    },
  });
  if (res.status === 204) return undefined;
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    const err = new Error(body?.message || res.statusText) as OperatorError;
    err.status = res.status;
    err.code   = body?.error || 'INTERNAL_ERROR';
    throw err;
  }
  return body;
}

/** Fetch the aif-ui-config ConfigMap and populate the in-memory cache.
 *  Falls back to hardcoded constants if the ConfigMap is missing or unreadable.
 *  Idempotent: subsequent calls return the shared in-flight promise to avoid
 *  duplicate requests; pass force=true to discard both cache and in-flight promise. */
export function loadOperatorConfig(force = false): Promise<void> {
  if (force) { cache = null; loadPromise = null; }
  if (cache) return Promise.resolve();
  if (!loadPromise) loadPromise = _doLoad();
  return loadPromise;
}

async function _doLoad(): Promise<void> {
  try {
    const res = await fetch(configMapUrl(), { headers: { Accept: 'application/json' } });
    if (res.ok) {
      const cm = await res.json();
      cache = {
        namespace:        cm?.data?.operatorNamespace || OPERATOR_NAMESPACE,
        service:          cm?.data?.operatorService   || OPERATOR_SERVICE,
        useStaticCatalog: parseConfigBool(cm?.data?.useStaticCatalog, true),
      };
      return;
    }
  } catch { /* fall through to defaults */ }
  cache = {
    namespace:        OPERATOR_NAMESPACE,
    service:          OPERATOR_SERVICE,
    useStaticCatalog: true,
  };
}

export function getOperatorNamespace(): string {
  return cache?.namespace ?? OPERATOR_NAMESPACE;
}

export function getOperatorService(): string {
  return cache?.service ?? OPERATOR_SERVICE;
}

export function getUseStaticCatalog(): boolean {
  return cache?.useStaticCatalog ?? true;
}

export function getOperatorBaseUrl(): string {
  const ns  = getOperatorNamespace();
  const svc = getOperatorService();
  return `/k8s/clusters/${ MANAGEMENT_CLUSTER }/api/v1/namespaces/${ ns }/services/http:${ svc }:${ OPERATOR_PORT }/proxy`;
}

async function runConnectionCheck(): Promise<void> {
  await loadOperatorConfig();
  const ns  = getOperatorNamespace();
  const url = `${ getOperatorBaseUrl() }/api/v1/settings`;

  try {
    const res = await fetch(url, { headers: { Accept: 'application/json' } });
    if (res.ok) return;

    if (res.status === 404) {
      const body = await res.json().catch(() => null);
      // Distinguish operator's own 404 (settings not yet configured) from a
      // Kubernetes/Rancher proxy 404 (service not found).  The k8s API machinery
      // returns a Status object ({ kind: "Status", ... }); the operator returns its
      // own error envelope without `kind`.  Checking for the k8s shape is more
      // stable than checking for an operator-specific field.
      if (body !== null && body?.kind !== 'Status') return;
    }

    connectionError = `Cannot connect to the SUSE AI operator in namespace "${ ns }".`;
  } catch (e: any) {
    connectionError = `Cannot connect to the SUSE AI operator in namespace "${ ns }": ${ e?.message || 'network error' }`;
  }
}

/** Check whether the operator is reachable. Runs at most once per session;
 *  subsequent calls return the cached promise immediately. Call with no await
 *  from product.ts init() for a background warm-up, then await it inside page
 *  fetch() hooks — the promise is shared so there is no duplicate request.
 *  Pass force=true (e.g. from a Retry button) to discard the cached result and
 *  re-run the check. */
export function checkOperatorConnection(force = false): Promise<void> {
  if (force) { connectionError = null; checkPromise = null; }
  if (!checkPromise) checkPromise = runConnectionCheck();
  return checkPromise;
}

/** Error message set by checkOperatorConnection(), or null if reachable. */
export function getConnectionError(): string | null {
  return connectionError;
}
