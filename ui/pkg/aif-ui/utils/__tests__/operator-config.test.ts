import {
  describe, it, expect, vi, beforeEach,
} from 'vitest';
import {
  OPERATOR_UNREACHABLE,
  checkOperatorConnection,
  getConnectionError,
  getOperatorBaseUrl,
  getOperatorNamespace,
  getUseStaticCatalog,
  isOperatorNotFound,
  loadOperatorConfig,
  operatorErrorMessage,
  operatorFetch,
  recheckOperatorConnection,
  setOperatorStore,
  toOperatorError,
} from '../operator-config';
import {
  deleteAIWorkload, getRegistryStatus, getSettings, retryAIWorkload, rollbackAIWorkload,
} from '../operator-api';

const dispatch = vi.fn();

/** Mimics the steve store's responseObject(): the body plus a `_status` field. */
function shellResponse(status: number, body: any): any {
  const out = typeof body === 'object' && body !== null ? { ...body } : { data: body };

  Object.defineProperty(out, '_status', { value: status });

  return out;
}

function lastRequest(): any {
  const calls = dispatch.mock.calls;

  expect(calls[calls.length - 1][0]).toBe('management/request');

  return calls[calls.length - 1][1];
}

beforeEach(() => {
  dispatch.mockReset();
  setOperatorStore({ dispatch } as any);
});

describe('getOperatorBaseUrl', () => {
  it('returns the Rancher APIService aggregation path', () => {
    expect(getOperatorBaseUrl()).toBe('/v1/aif-operator');
  });
});

describe('operatorFetch transport', () => {
  it('sends a GET through management/request with the absolute aggregated URL', async() => {
    dispatch.mockResolvedValue(shellResponse(200, { spec: {} }));

    const out = await operatorFetch('/api/v1/settings');

    expect(out).toEqual({ spec: {} });
    const opt = lastRequest();

    expect(opt.url).toBe('/v1/aif-operator/api/v1/settings');
    expect(opt.method).toBe('GET');
    expect(opt.data).toBeUndefined();
    expect(opt.headers['Content-Type']).toBeUndefined();
    expect(opt.redirectUnauthorized).toBeUndefined();
  });

  it('sends a PUT body as JSON with Content-Type application/json', async() => {
    dispatch.mockResolvedValue(shellResponse(200, {}));

    await operatorFetch('/api/v1/settings', { method: 'PUT', body: JSON.stringify({ spec: { a: 1 } }) });

    const opt = lastRequest();

    expect(opt.method).toBe('PUT');
    expect(opt.headers['Content-Type']).toBe('application/json');
    expect(opt.data).toEqual({ spec: { a: 1 } });
  });

  it('sends an empty JSON object and Content-Type on a DELETE without a body', async() => {
    dispatch.mockResolvedValue(shellResponse(204, null));

    const out = await operatorFetch('/api/v1/namespaces/ns/aiworkloads/w', { method: 'DELETE' });

    expect(out).toBeUndefined();
    const opt = lastRequest();

    expect(opt.method).toBe('DELETE');
    expect(opt.headers['Content-Type']).toBe('application/json');
    expect(opt.data).toEqual({});
  });

  it('passes the timeout through to the request and sends no abort signal', async() => {
    dispatch.mockResolvedValue(shellResponse(200, {}));

    await operatorFetch('/api/v1/version', { timeout: 5000 });

    expect(lastRequest().timeout).toBe(5000);
    expect(lastRequest().signal).toBeUndefined();
  });

  it.each([
    ['deleteAIWorkload', () => deleteAIWorkload('ns', 'w')],
    ['rollbackAIWorkload', () => rollbackAIWorkload('ns', 'w')],
    ['retryAIWorkload', () => retryAIWorkload('ns', 'w')],
  ])('%s sends a JSON body so the operator does not answer 415', async(_name, call) => {
    dispatch.mockResolvedValue(shellResponse(200, {}));

    await call();

    const opt = lastRequest();

    expect(opt.method).not.toBe('GET');
    expect(opt.headers['Content-Type']).toBe('application/json');
    expect(opt.data).toEqual({});
  });
});

describe('operatorFetch errors', () => {
  it('maps a shell 403 error onto an OperatorError', async() => {
    dispatch.mockRejectedValue(shellResponse(403, { error: 'FORBIDDEN', message: 'not allowed to get settings' }));

    await expect(operatorFetch('/api/v1/version')).rejects.toMatchObject({
      status: 403, code: 'FORBIDDEN', message: 'not allowed to get settings',
    });
  });

  it('defaults the code from the status when the body has no operator envelope', () => {
    const err = toOperatorError(shellResponse(403, { type: 'error', status: 403 }));

    expect(err).toBeInstanceOf(Error);
    expect(err.status).toBe(403);
    expect(err.code).toBe('FORBIDDEN');
    expect(err.unreachable).toBe(false);
  });

  it('propagates a 401 and leaves the logout redirect to the shell', async() => {
    dispatch.mockRejectedValue(shellResponse(401, { error: 'UNAUTHORIZED', message: 'session expired' }));

    await expect(operatorFetch('/api/v1/settings')).rejects.toMatchObject({ status: 401, code: 'UNAUTHORIZED' });
    expect(lastRequest().redirectUnauthorized).toBeUndefined();
    expect(dispatch).toHaveBeenCalledTimes(1);
  });

  it('marks a 503 Handler disconnected as unreachable with the OPERATOR_UNREACHABLE code', () => {
    const err = toOperatorError(shellResponse(503, 'Handler disconnected'));

    expect(err.status).toBe(503);
    expect(err.code).toBe('OPERATOR_UNREACHABLE');
    expect(err.unreachable).toBe(true);
    // Technical detail only; the UI text comes from l10n via the code.
    expect(err.message).toBe('Handler disconnected');
  });

  it('marks a timeout without a response as unreachable', () => {
    const err = toOperatorError(Object.assign(new Error('timeout of 8000ms exceeded'), { code: 'ECONNABORTED' }));

    expect(err.status).toBe(0);
    expect(err.code).toBe('TIMEOUT');
    expect(err.unreachable).toBe(true);
  });

  it('does not mark an operator-originated 500 as unreachable', () => {
    const err = toOperatorError(shellResponse(500, { error: 'INTERNAL_ERROR', message: 'boom' }));

    expect(err.unreachable).toBe(false);
    expect(err.message).toBe('boom');
  });
});

describe('operatorFetch retry', () => {
  it('retries a GET once on 502', async() => {
    dispatch
      .mockRejectedValueOnce(shellResponse(502, 'Bad Gateway'))
      .mockResolvedValueOnce(shellResponse(200, { ok: true }));

    await expect(operatorFetch('/api/v1/settings')).resolves.toEqual({ ok: true });
    expect(dispatch).toHaveBeenCalledTimes(2);
  });

  it('retries a GET once on 500 tunnel disconnect', async() => {
    dispatch
      .mockRejectedValueOnce(shellResponse(500, 'tunnel disconnect'))
      .mockResolvedValueOnce(shellResponse(200, { ok: true }));

    await expect(operatorFetch('/api/v1/settings')).resolves.toEqual({ ok: true });
    expect(dispatch).toHaveBeenCalledTimes(2);
  });

  it('retries a GET only once', async() => {
    dispatch.mockRejectedValue(shellResponse(502, 'Bad Gateway'));

    await expect(operatorFetch('/api/v1/settings')).rejects.toMatchObject({ status: 502 });
    expect(dispatch).toHaveBeenCalledTimes(2);
  });

  it('does not retry an operator-originated 500', async() => {
    dispatch.mockRejectedValue(shellResponse(500, { error: 'INTERNAL_ERROR', message: 'boom' }));

    await expect(operatorFetch('/api/v1/settings')).rejects.toMatchObject({ status: 500 });
    expect(dispatch).toHaveBeenCalledTimes(1);
  });

  it('never retries a POST', async() => {
    dispatch.mockRejectedValue(shellResponse(502, 'Bad Gateway'));

    await expect(operatorFetch('/api/v1/git/publish', { method: 'POST', body: '{}' })).rejects.toMatchObject({ status: 502 });
    expect(dispatch).toHaveBeenCalledTimes(1);
  });
});

describe('checkOperatorConnection', () => {
  it('reports the operator as unreachable on 503 Handler disconnected', async() => {
    dispatch.mockRejectedValue(shellResponse(503, 'Handler disconnected'));

    await checkOperatorConnection(true);

    expect(getConnectionError()).toBe(OPERATOR_UNREACHABLE);
    expect(OPERATOR_UNREACHABLE).toBe('OPERATOR_UNREACHABLE');
  });

  it('reports the operator as unreachable on a timeout and uses a short probe timeout', async() => {
    dispatch.mockRejectedValue(Object.assign(new Error('timeout of 8000ms exceeded'), { code: 'ECONNABORTED' }));

    await checkOperatorConnection(true);

    expect(getConnectionError()).toBe(OPERATOR_UNREACHABLE);
    const opt = lastRequest();

    expect(opt.url).toBe('/v1/aif-operator/api/v1/settings');
    expect(opt.timeout).toBeGreaterThanOrEqual(5000);
    expect(opt.timeout).toBeLessThanOrEqual(8000);
  });

  it('reports the operator as unreachable when Rancher has no handler for the path (404 without envelope)', async() => {
    dispatch.mockRejectedValue(shellResponse(404, { type: 'error', status: 404, code: 'NotFound' }));

    await checkOperatorConnection(true);

    expect(getConnectionError()).toBe(OPERATOR_UNREACHABLE);
  });

  it.each([
    ['200', () => dispatch.mockResolvedValue(shellResponse(200, {}))],
    ['operator 404 (settings not configured)', () => dispatch.mockRejectedValue(shellResponse(404, { error: 'NOT_FOUND', message: 'no settings' }))],
    ['403 (no permission)', () => dispatch.mockRejectedValue(shellResponse(403, { error: 'FORBIDDEN', message: 'denied' }))],
    ['401', () => dispatch.mockRejectedValue(shellResponse(401, { error: 'UNAUTHORIZED', message: 'no session' }))],
  ])('treats %s as reachable', async(_name, arrange) => {
    arrange();

    await checkOperatorConnection(true);

    expect(getConnectionError()).toBeNull();
  });
});

describe('checkOperatorConnection dedupe key', () => {
  it('sends headers that differ from getSettings so the steve store never merges the two GETs', async() => {
    dispatch.mockResolvedValue(shellResponse(200, {}));

    await checkOperatorConnection(true);
    const probe = lastRequest();

    await operatorFetch('/api/v1/settings');
    const regular = lastRequest();

    expect(probe.url).toBe(regular.url);
    expect(probe.method).toBe(regular.method);
    // The steve request action dedupes on JSON.stringify(headers) + method + url.
    expect(JSON.stringify(probe.headers)).not.toBe(JSON.stringify(regular.headers));
    expect(regular.headers['X-Aif-Probe']).toBeUndefined();
  });
});

describe('operatorFetch timeout dedupe key', () => {
  async function headersFor(options?: { timeout?: number }): Promise<string> {
    dispatch.mockResolvedValue(shellResponse(200, {}));
    await operatorFetch('/api/v1/settings/registry-status', options);

    return JSON.stringify(lastRequest().headers);
  }

  it('sends different headers for different timeouts so the steve store keeps the GETs apart', async() => {
    const short = await headersFor({ timeout: 5000 });
    const long  = await headersFor({ timeout: 30000 });

    expect(short).not.toBe(long);
    expect(JSON.parse(short)['X-Aif-Timeout']).toBe('5000');
    expect(JSON.parse(long)['X-Aif-Timeout']).toBe('30000');
  });

  it('sends identical headers for the same timeout so those GETs still share one request', async() => {
    expect(await headersFor({ timeout: 5000 })).toBe(await headersFor({ timeout: 5000 }));
  });

  it('sends no timeout header when no timeout is set', async() => {
    expect(JSON.parse(await headersFor())['X-Aif-Timeout']).toBeUndefined();
  });

  /** A dispatch that merges concurrent GETs the way the steve request action
   *  does (key: JSON.stringify(headers) + method + url) and records each
   *  request that actually goes out. */
  function dedupingDispatch() {
    const sent: any[] = [];
    const pending: Record<string, { opt: any; promise: Promise<any>; resolve:(v: any) => void }> = {};

    dispatch.mockImplementation((_action: string, opt: any) => {
      const key = JSON.stringify(opt.headers || {}) + opt.method.toLowerCase() + opt.url;

      if (pending[key]) return pending[key].promise;

      let resolve!: (v: any) => void;
      const promise = new Promise((r) => {
        resolve = r;
      });

      pending[key] = {
        opt, promise, resolve
      };
      sent.push(opt);

      return promise;
    });

    return {
      sent,
      settle: () => Object.values(pending).forEach(p => p.resolve(shellResponse(200, {}))),
    };
  }

  it('keeps getRegistryStatus(5000) from joining an in-flight getRegistryStatus() and its 30 s timeout', async() => {
    const steve = dedupingDispatch();
    const long  = getRegistryStatus();
    const short = getRegistryStatus(5000);

    steve.settle();
    await Promise.all([long, short]);

    expect(steve.sent.map(o => o.timeout)).toEqual([30000, 5000]);
  });

  it('keeps getSettings(timeout) from joining an in-flight getSettings() without one', async() => {
    const steve = dedupingDispatch();
    const plain = getSettings();
    const timed = getSettings(8000);

    steve.settle();
    await Promise.all([plain, timed]);

    expect(steve.sent.map(o => o.timeout)).toEqual([undefined, 8000]);
  });

  it('still merges callers that use the same timeout', async() => {
    const steve = dedupingDispatch();
    const first  = getRegistryStatus();
    const second = getRegistryStatus();

    steve.settle();
    await Promise.all([first, second]);

    expect(steve.sent).toHaveLength(1);
  });
});

describe('operator recovery after an unreachable check', () => {
  beforeEach(async() => {
    dispatch.mockRejectedValueOnce(shellResponse(503, 'Handler disconnected'));
    await checkOperatorConnection(true);
    expect(getConnectionError()).toBe(OPERATOR_UNREACHABLE);
    dispatch.mockReset();
  });

  it.each([
    ['a successful call', () => dispatch.mockResolvedValueOnce(shellResponse(200, { items: [] }))],
    ['an operator error answer (403)', () => dispatch.mockRejectedValueOnce(shellResponse(403, { error: 'FORBIDDEN', message: 'forbidden' }))],
  ])('clears the cached failure on %s, without probing again', async(_name, arrange) => {
    arrange();

    await operatorFetch('/api/v1/aiworkloads').catch(() => undefined);
    await checkOperatorConnection();

    expect(getConnectionError()).toBeNull();
    expect(dispatch).toHaveBeenCalledTimes(1);
  });

  it('keeps the cached failure when the call does not reach the operator', async() => {
    dispatch.mockRejectedValueOnce(shellResponse(503, 'Handler disconnected'));

    await operatorFetch('/api/v1/aiworkloads').catch(() => undefined);

    expect(getConnectionError()).toBe(OPERATOR_UNREACHABLE);
  });

  it('recheckOperatorConnection probes again and reports whether the operator is back', async() => {
    dispatch.mockRejectedValueOnce(shellResponse(503, 'Handler disconnected'));
    expect(await recheckOperatorConnection()).toBe(false);

    dispatch.mockResolvedValueOnce(shellResponse(200, {}));
    expect(await recheckOperatorConnection()).toBe(true);
    expect(getConnectionError()).toBeNull();
    expect(dispatch).toHaveBeenCalledTimes(2);
  });
});

describe('checkOperatorConnection before login', () => {
  it('does not cache a 401 from the warm-up and does not ask the shell to log out', async() => {
    dispatch.mockRejectedValueOnce(shellResponse(401, { error: 'UNAUTHORIZED', message: 'no session' }));

    await checkOperatorConnection(true);

    expect(lastRequest().redirectUnauthorized).toBe(false);

    dispatch.mockRejectedValueOnce(shellResponse(503, 'Handler disconnected'));
    await checkOperatorConnection();

    expect(dispatch).toHaveBeenCalledTimes(2);
    expect(getConnectionError()).toBe(OPERATOR_UNREACHABLE);
  });
});

describe('loadOperatorConfig', () => {
  it('reads the namespace and catalog mode from the aif-ui-config ConfigMap through the store', async() => {
    dispatch.mockResolvedValue(shellResponse(200, { data: { operatorNamespace: 'custom-ns', useStaticCatalog: 'false' } }));

    await loadOperatorConfig(true);

    expect(lastRequest().url).toBe('/k8s/clusters/local/api/v1/namespaces/cattle-ui-plugin-system/configmaps/aif-ui-config');
    expect(getOperatorNamespace()).toBe('custom-ns');
    expect(getUseStaticCatalog()).toBe(false);
  });

  it('falls back to defaults when the ConfigMap cannot be read', async() => {
    dispatch.mockRejectedValue(shellResponse(403, { type: 'error', status: 403 }));

    await loadOperatorConfig(true);

    expect(getOperatorNamespace()).toBe('aif-operator');
    expect(getUseStaticCatalog()).toBe(true);
  });
});

describe('isOperatorNotFound', () => {
  it('is true for the operator\'s own NOT_FOUND 404 (Settings not created yet)', () => {
    const err = toOperatorError(shellResponse(404, { error: 'NOT_FOUND', message: 'settings not found' }));

    expect(isOperatorNotFound(err)).toBe(true);
  });

  it('is false for a Rancher/steve 404 (no APIService registered): that is unreachable, not missing', () => {
    const err = toOperatorError(shellResponse(404, { type: 'error', status: 404, code: 'NotFound' }));

    expect(err.unreachable).toBe(true);
    expect(isOperatorNotFound(err)).toBe(false);
  });

  it('is false for other statuses and for empty input', () => {
    expect(isOperatorNotFound(toOperatorError(shellResponse(503, 'Handler disconnected')))).toBe(false);
    expect(isOperatorNotFound(toOperatorError(shellResponse(403, { error: 'FORBIDDEN', message: 'no' })))).toBe(false);
    expect(isOperatorNotFound(undefined)).toBe(false);
  });
});

describe('operatorErrorMessage', () => {
  const t = vi.fn((key: string) => `T(${ key })`);

  it('returns the translated unreachable text for a 503 Handler disconnected', () => {
    expect(operatorErrorMessage(toOperatorError(shellResponse(503, 'Handler disconnected')), t, 'fallback'))
      .toBe('T(suseai.operator.unreachable)');
  });

  it('returns the translated unreachable text for a steve 404', () => {
    expect(operatorErrorMessage(toOperatorError(shellResponse(404, { type: 'error', status: 404, code: 'NotFound' })), t))
      .toBe('T(suseai.operator.unreachable)');
  });

  it('returns the translated unreachable text for a timeout and a network error', () => {
    const timeout = toOperatorError(Object.assign(new Error('timeout of 8000ms exceeded'), { code: 'ECONNABORTED' }));
    const network = toOperatorError(new Error('Network Error'));

    expect(operatorErrorMessage(timeout, t)).toBe('T(suseai.operator.unreachable)');
    expect(operatorErrorMessage(network, t)).toBe('T(suseai.operator.unreachable)');
  });

  it('recognises the codes alone, without the unreachable flag', () => {
    for (const code of ['OPERATOR_UNREACHABLE', 'TIMEOUT', 'NETWORK_ERROR']) {
      expect(operatorErrorMessage({ code, message: 'raw' }, t)).toBe('T(suseai.operator.unreachable)');
    }
  });

  it('keeps the operator\'s own message for errors that reached it', () => {
    const err = toOperatorError(shellResponse(409, { error: 'CONFLICT', message: 'workload already exists' }));

    expect(operatorErrorMessage(err, t, 'fallback')).toBe('workload already exists');
  });

  it('uses the fallback when there is no message, and String(e) without a fallback', () => {
    expect(operatorErrorMessage({}, t, 'Failed to load')).toBe('Failed to load');
    expect(operatorErrorMessage('boom', t)).toBe('boom');
  });

  it('falls back to the raw message when the translation is missing', () => {
    const missing = () => undefined as unknown as string;

    expect(operatorErrorMessage(toOperatorError(shellResponse(503, 'Handler disconnected')), missing)).toBe('Handler disconnected');
  });
});
