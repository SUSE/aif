import { describe, it, expect, vi } from 'vitest';
import {
  mintOperatorToken,
  ensureTokenSecret,
  TOKEN_EXPIRES_ANNOTATION,
  TOKEN_NAME_ANNOTATION,
} from '../rancher-token';

// Minimal stand-in for the Vuex store: records dispatches and replays canned
// responses in order.
function fakeStore(responses: any[]) {
  const calls: any[] = [];
  const queue = [...responses];
  const store = {
    calls,
    dispatch: vi.fn(async (action: string, payload: any) => {
      calls.push({ action, payload });
      const next = queue.shift();
      if (next instanceof Error) throw next;
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

  it('falls back to /v3/tokens when the ext resource is absent', async () => {
    const notFound = Object.assign(new Error('not found'), { status: 404 });
    const store = fakeStore([
      { id: 'user-c4f4g', principalIds: ['local://user-c4f4g'] },
      notFound,
      { name: 'token-legacy', token: 'token-legacy:bbb', expiresAt: '2026-10-27T00:00:00Z' },
    ]);

    const minted = await mintOperatorToken(store as any);

    expect(minted.value).toBe('token-legacy:bbb');
    expect(minted.tokenName).toBe('token-legacy');
    expect(store.calls[store.calls.length - 1].payload.url).toContain('/v3/tokens');
  });
});

describe('ensureTokenSecret', () => {
  it('writes the token and annotates expiry and token name', async () => {
    const store = fakeStore([Object.assign(new Error('not found'), { status: 404 }), {}]);

    await ensureTokenSecret(store as any, 'aif', 'aif-rancher-token', {
      value:     'token-1:aaa',
      expiresAt: '2026-10-27T00:00:00Z',
      tokenName: 'token-1',
    });

    const write = store.calls[store.calls.length - 1];
    expect(write.payload.data.metadata.annotations[TOKEN_EXPIRES_ANNOTATION]).toBe('2026-10-27T00:00:00Z');
    expect(write.payload.data.metadata.annotations[TOKEN_NAME_ANNOTATION]).toBe('token-1');
    // The value must be base64-encoded into data.token.
    expect(write.payload.data.data.token).toBe(btoa('token-1:aaa'));
  });
});
