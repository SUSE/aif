import { describe, it, expect, vi } from 'vitest';
import {
  mintOperatorToken,
  ensureTokenSecret,
  deleteToken,
  TOKEN_EXPIRES_ANNOTATION,
  TOKEN_NAME_ANNOTATION,
} from '../rancher-token';

// The two shapes @rancher/shell/plugins/steve/actions.js actually rejects with.
function k8sNotFound(msg: string): any {
  const body: any = {
    kind: 'Status', apiVersion: 'v1', metadata: {}, status: 'Failure',
    message: msg, reason: 'NotFound', code: 404,
  };
  Object.defineProperty(body, '_status', { value: 404 });
  return body;
}

function plainNotFound(): any {
  const body: any = { data: '404 page not found' };
  Object.defineProperty(body, '_status', { value: 404 });
  return body;
}

// Minimal stand-in for the Vuex store: records dispatches and replays canned
// responses in order. Throws when the next response is an Error or has _status
// (rancher/request rejection shapes have _status, not instanceof Error).
function fakeStore(responses: any[]) {
  const calls: any[] = [];
  const queue = [...responses];
  const store = {
    calls,
    dispatch: vi.fn(async (action: string, payload: any) => {
      calls.push({ action, payload });
      const next = queue.shift();
      if (next instanceof Error || (next && Object.prototype.hasOwnProperty.call(next, '_status'))) {
        throw next;
      }
      return next;
    }),
  };
  return store;
}

describe('mintOperatorToken', () => {
  it('mints via tokens.ext.cattle.io and returns the bearer token', async () => {
    const store = fakeStore([
      { id: 'user-c4f4g', principalIds: ['local://user-c4f4g'] },
      {
        metadata: { name: 'token-86swv' },
        status:   { bearerToken: 'token-86swv:zzz', expiresAt: '2026-10-27T22:44:47Z' },
      },
    ]);

    const minted = await mintOperatorToken(store as any);

    expect(minted.value).toBe('token-86swv:zzz');
    expect(minted.expiresAt).toBe('2026-10-27T22:44:47Z');
    expect(minted.tokenName).toBe('token-86swv');

    const create = store.calls[store.calls.length - 1];
    expect(create.payload.url).toContain('ext.cattle.io');
    expect(create.payload.data.spec.ttl).toBe(0);
  });

  it('accepts a principal different from the one sent', async () => {
    // Rancher always mints for the requesting user and overwrites the principal
    // in the request. That is expected, not an error.
    const store = fakeStore([
      { id: 'user-c4f4g', principalIds: ['local://user-xxxxx'] },
      {
        metadata: { name: 'token-1' },
        spec:     { userPrincipal: { name: 'local://user-c4f4g' } },
        status:   { bearerToken: 'token-1:aaa', expiresAt: '2026-10-27T00:00:00Z' },
      },
    ]);

    await expect(mintOperatorToken(store as any)).resolves.toMatchObject({ value: 'token-1:aaa' });
  });

  it('falls back to /v3/tokens when the ext resource is absent (plain 404)', async () => {
    const store = fakeStore([
      { id: 'user-c4f4g', principalIds: ['local://user-c4f4g'] },
      plainNotFound(),
      { name: 'token-legacy', token: 'token-legacy:bbb', expiresAt: '2026-10-27T00:00:00Z' },
    ]);

    const minted = await mintOperatorToken(store as any);

    expect(minted.value).toBe('token-legacy:bbb');
    expect(minted.tokenName).toBe('token-legacy');
    expect(store.calls[store.calls.length - 1].payload.url).toContain('/v3/tokens');
  });

  it('does not fall back on 403', async () => {
    const forbidden: any = { message: 'Forbidden' };
    Object.defineProperty(forbidden, '_status', { value: 403 });
    const store = fakeStore([
      { id: 'user-c4f4g', principalIds: ['local://user-c4f4g'] },
      forbidden,
    ]);

    await expect(mintOperatorToken(store as any)).rejects.toMatchObject({ _status: 403 });
    expect(store.calls).toHaveLength(2); // /v3/users + ext tokens only, no /v3/tokens
  });
});

describe('ensureTokenSecret', () => {
  it('creates the Secret when absent', async () => {
    const store = fakeStore([
      k8sNotFound('secrets "aif-rancher-token" not found'),
      {},
    ]);

    await ensureTokenSecret(store as any, 'aif', 'aif-rancher-token', {
      value:     'token-1:aaa',
      expiresAt: '2026-10-27T00:00:00Z',
      tokenName: 'token-1',
    });

    expect(store.calls).toHaveLength(2);
    const [get, post] = store.calls;
    expect(get.payload.url).toContain('/k8s/clusters/local/api/v1/namespaces/aif/secrets/aif-rancher-token');
    expect(post.payload.method).toBe('POST');
    expect(post.payload.url).toContain('/k8s/clusters/local/api/v1/namespaces/aif/secrets');
    expect(post.payload.data.data.token).toBe(btoa('token-1:aaa'));
    expect(post.payload.data.metadata.annotations[TOKEN_EXPIRES_ANNOTATION]).toBe('2026-10-27T00:00:00Z');
    expect(post.payload.data.metadata.annotations[TOKEN_NAME_ANNOTATION]).toBe('token-1');
  });

  it('updates the Secret when it exists', async () => {
    const store = fakeStore([
      { metadata: { name: 'aif-rancher-token' } },
      {},
    ]);

    await ensureTokenSecret(store as any, 'aif', 'aif-rancher-token', {
      value:     'token-2:bbb',
      expiresAt: '2026-10-28T00:00:00Z',
      tokenName: 'token-2',
    });

    expect(store.calls).toHaveLength(2);
    const [get, put] = store.calls;
    expect(get.payload.url).toContain('/k8s/clusters/local/api/v1/namespaces/aif/secrets/aif-rancher-token');
    expect(put.payload.method).toBe('PUT');
    expect(put.payload.url).toContain('/k8s/clusters/local/api/v1/namespaces/aif/secrets/aif-rancher-token');
    expect(put.payload.data.data.token).toBe(btoa('token-2:bbb'));
  });
});

describe('deleteToken', () => {
  it('is best-effort when ext DELETE returns 404', async () => {
    const store = fakeStore([k8sNotFound('tokens.ext.cattle.io "token-1" not found')]);

    await expect(deleteToken(store as any, 'token-1')).resolves.toBeUndefined();
    expect(store.calls).toHaveLength(1); // ext DELETE only, no legacy fallback
  });

  it('falls back to legacy when ext DELETE fails with 403', async () => {
    const forbidden: any = { message: 'Forbidden' };
    Object.defineProperty(forbidden, '_status', { value: 403 });
    const store = fakeStore([forbidden, {}]);

    await expect(deleteToken(store as any, 'token-1')).resolves.toBeUndefined();
    expect(store.calls).toHaveLength(2);
    expect(store.calls[0].payload.url).toContain('/apis/ext.cattle.io/v1/tokens/token-1');
    expect(store.calls[1].payload.url).toContain('/v3/tokens/token-1');
  });

  it('resolves even when both endpoints fail', async () => {
    const forbidden: any = { message: 'Forbidden' };
    Object.defineProperty(forbidden, '_status', { value: 403 });
    const forbidden2: any = { message: 'Forbidden' };
    Object.defineProperty(forbidden2, '_status', { value: 403 });
    const store = fakeStore([forbidden, forbidden2]);

    await expect(deleteToken(store as any, 'token-1')).resolves.toBeUndefined();
    expect(store.calls).toHaveLength(2);
  });
});
