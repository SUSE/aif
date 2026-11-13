import { MANAGEMENT_CLUSTER, OPERATOR_NAMESPACE } from './constants';
import type { Dispatchable } from '../types/rancher-types';

const CONFIG_NAMESPACE = 'cattle-ui-plugin-system';
const CONFIG_MAP_NAME  = 'aif-ui-config';

/** The operator serves its API only through Rancher's APIService aggregation. */
const OPERATOR_BASE_URL = '/v1/aif-operator';

/** Rancher waits about 15 s for a disconnected backend before answering 503;
 *  the connection probe gives up sooner so pages don't hang on it. */
const PROBE_TIMEOUT_MS = 8000;

const SAFE_METHODS = ['GET', 'HEAD', 'OPTIONS'];

/** Error code for requests that never reached the operator. The UI renders
 *  the user-facing text from l10n (`suseai.operator.unreachable`). */
export const OPERATOR_UNREACHABLE = 'OPERATOR_UNREACHABLE';

/** Probe-only header: gives the connection probe its own key in the steve
 *  store's GET dedupe (headers + method + url), so it never joins a regular
 *  getSettings() and lends it its timeout or redirectUnauthorized: false.
 *  The operator ignores unknown headers. */
const PROBE_HEADERS = { 'X-Aif-Probe': '1' };

const DEFAULT_CODES: Record<number, string> = {
  401: 'UNAUTHORIZED',
  403: 'FORBIDDEN',
  404: 'NOT_FOUND',
  409: 'CONFLICT',
};

interface OperatorCache {
  namespace:         string;
  useStaticCatalog:  boolean;
}

export interface OperatorError extends Error {
  status:       number;
  code:         string;
  /** True when the request never reached the operator (Rancher has no
   *  connected backend, a proxy/tunnel failure, a timeout or a network error). */
  unreachable?: boolean;
}

export interface OperatorFetchOptions {
  method?:  string;
  /** JSON string, as produced by JSON.stringify. */
  body?:    string;
  headers?: Record<string, string>;
  signal?:  AbortSignal;
  /** Request timeout in milliseconds (axios). */
  timeout?: number;
  /** Set to false to stop the shell from logging out on 401 (connection probe only). */
  redirectUnauthorized?: boolean;
}

/** Parse a ConfigMap string value ("true"/"false") to boolean, using def when absent/empty. */
function parseConfigBool(v: unknown, def: boolean): boolean {
  if (v === undefined || v === null || v === '') return def;
  return String(v).toLowerCase() === 'true';
}

let store: Dispatchable | null = null;
let cache: OperatorCache | null = null;
let loadPromise: Promise<void> | null = null;
let connectionError: string | null = null;
let checkPromise: Promise<void> | null = null;

/** Give operator-config the Rancher store, so every operator call goes through
 *  the shell's `management/request` action (CSRF header, 401 → logout).
 *  Called once from product.ts init(). */
export function setOperatorStore(s: Dispatchable): void {
  store = s;
}

function requireStore(): Dispatchable {
  if (!store) {
    const err = new Error('AI Factory operator client used before the extension was initialised') as OperatorError;

    err.status = 0;
    err.code   = 'NOT_INITIALISED';
    throw err;
  }

  return store;
}

function configMapUrl(): string {
  return `/k8s/clusters/${ MANAGEMENT_CLUSTER }/api/v1/namespaces/${ CONFIG_NAMESPACE }/configmaps/${ CONFIG_MAP_NAME }`;
}

/** Text of a shell error body: the steve store wraps non-JSON bodies as `{ data: '<text>' }`. */
function errorText(e: any): string {
  return typeof e?.data === 'string' ? e.data : '';
}

/** Map an error from the shell's `request` action (the response body with a
 *  `_status` field, or an axios error when there was no response) onto
 *  OperatorError, so callers keep using `status`, `code` and `message`. */
export function toOperatorError(e: any): OperatorError {
  const status: number = typeof e?._status === 'number' ? e._status : 0;
  let err: OperatorError;

  if (!status) {
    const timedOut = e?.code === 'ECONNABORTED' || e?.code === 'ETIMEDOUT' || e?.code === 'ERR_CANCELED' || e?.name === 'AbortError' || e?.name === 'CanceledError';

    err = new Error(e?.message || (timedOut ? 'timeout' : 'network error')) as OperatorError;
    err.status      = 0;
    err.code        = timedOut ? 'TIMEOUT' : 'NETWORK_ERROR';
    err.unreachable = true;

    return err;
  }

  // The operator always answers with { error: '<CODE>', message }; anything
  // else on a 404 or 5xx came from Rancher or a proxy, not the operator.
  const fromOperator = typeof e?.error === 'string';
  const unreachable  = !fromOperator && (status === 404 || status >= 500);
  const message      = (fromOperator && typeof e?.message === 'string' && e.message) || errorText(e) || e?._statusText || `HTTP ${ status }`;

  err = new Error(message) as OperatorError;
  err.status      = status;
  err.code        = fromOperator ? e.error : (unreachable ? OPERATOR_UNREACHABLE : (DEFAULT_CODES[status] || 'INTERNAL_ERROR'));
  err.unreachable = unreachable;

  return err;
}

/** True only for the operator's own 404 (e.g. Settings not created yet).
 *  toOperatorError keeps status 404 on a Rancher/steve 404 (no APIService
 *  registered, i.e. an old operator) but flags it `unreachable`, so testing
 *  `status === 404` alone would treat "operator unreachable" as "not found". */
export function isOperatorNotFound(e: any): boolean {
  return e?.status === 404 && !e?.unreachable;
}

const UNREACHABLE_CODES = [OPERATOR_UNREACHABLE, 'TIMEOUT', 'NETWORK_ERROR'];

/** Display text for an operator error: the l10n "operator is unreachable" text
 *  when the request never reached the operator (instead of raw "Handler
 *  disconnected" or "timeout of …ms exceeded"), else the error's own message,
 *  then `fallback`. Pass the page's translate function (useT() or this.t). */
export function operatorErrorMessage(e: any, t: (key: string, ...rest: any[]) => string, fallback?: string): string {
  const raw = e?.message || fallback || String(e);

  if (e?.unreachable || UNREACHABLE_CODES.includes(e?.code)) {
    return t('suseai.operator.unreachable') || raw;
  }

  return raw;
}

/** Transient tunnel failures seen while a Rancher pod restarts (500 "tunnel disconnect", 502). */
function isTransient(e: any): boolean {
  const status = e?._status;

  return status === 502 || (status === 500 && /tunnel disconnect/i.test(errorText(e)));
}

/** Shared wrapper for operator API calls through Rancher's APIService
 *  aggregation. Sends writes as JSON (an empty object when there is no body,
 *  since the operator answers 415 otherwise), returns undefined for 204, retries
 *  an idempotent request once on a transient tunnel/proxy failure, and throws
 *  OperatorError. A 401 is left to the shell, which logs the user out. */
export async function operatorFetch(path: string, options: OperatorFetchOptions = {}): Promise<any> {
  const dispatch = requireStore().dispatch;
  const method   = (options.method || 'GET').toUpperCase();
  const safe     = SAFE_METHODS.includes(method);
  const opt: Record<string, any> = {
    url:     `${ getOperatorBaseUrl() }${ path }`,
    method,
    headers: {
      Accept: 'application/json',
      ...(safe ? {} : { 'Content-Type': 'application/json' }),
      ...(options.headers || {}),
    },
  };

  if (!safe) opt.data = options.body ? JSON.parse(options.body) : {};
  if (options.signal) opt.signal = options.signal;
  if (options.timeout) opt.timeout = options.timeout;
  if (options.redirectUnauthorized === false) opt.redirectUnauthorized = false;

  for (let attempt = 0; ; attempt++) {
    try {
      // The steve request action mutates opt (url), so pass a fresh copy each time.
      const res = await dispatch('management/request', { ...opt });

      return res?._status === 204 ? undefined : res;
    } catch (e) {
      if (safe && attempt === 0 && isTransient(e)) continue;
      throw toOperatorError(e);
    }
  }
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
    const cm = await requireStore().dispatch('management/request', {
      url:     configMapUrl(),
      method:  'GET',
      headers: { Accept: 'application/json' },
    });

    cache = {
      namespace:        cm?.data?.operatorNamespace || OPERATOR_NAMESPACE,
      useStaticCatalog: parseConfigBool(cm?.data?.useStaticCatalog, true),
    };
  } catch {
    cache = {
      namespace:        OPERATOR_NAMESPACE,
      useStaticCatalog: true,
    };
  }
}

export function getOperatorNamespace(): string {
  return cache?.namespace ?? OPERATOR_NAMESPACE;
}

export function getUseStaticCatalog(): boolean {
  return cache?.useStaticCatalog ?? true;
}

export function getOperatorBaseUrl(): string {
  return OPERATOR_BASE_URL;
}

async function runConnectionCheck(): Promise<void> {
  try {
    // Any answer from the operator itself (including 401, 403 or its own 404
    // when settings aren't configured yet) means it is reachable. This probe
    // also runs from init() before login, so it must not trigger a logout.
    await operatorFetch('/api/v1/settings', {
      headers: PROBE_HEADERS, timeout: PROBE_TIMEOUT_MS, redirectUnauthorized: false,
    });
  } catch (e: any) {
    if (e?.unreachable) connectionError = OPERATOR_UNREACHABLE;
    // A 401 from the pre-login warm-up says nothing useful; check again once
    // a page asks after sign-in.
    if (e?.status === 401) checkPromise = null;
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

/** Error code set by checkOperatorConnection() (OPERATOR_UNREACHABLE), or null
 *  if reachable. Render it with OperatorErrorBanner, which translates it. */
export function getConnectionError(): string | null {
  return connectionError;
}
