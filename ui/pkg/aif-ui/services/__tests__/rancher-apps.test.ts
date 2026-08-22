import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  ensurePullSecretOnAllSAs,
  ensureRegistrySecret,
  ensureServiceAccountPullSecret,
  listServiceAccounts,
} from '../rancher-apps';
import type { Dispatchable } from '../../types/rancher-types';
import { httpStatus } from '../../utils/error-handler';

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

interface RequestConfig {
  url?: string;
  method?: string;
  headers?: Record<string, string>;
  data?: unknown;
  timeout?: number;
}

interface RecordedCall {
  action: string;
  payload: RequestConfig;
}

function k8sFailure(code: number, message: string) {
  const body = {
    apiVersion: 'v1',
    kind:       'Status',
    status:     'Failure',
    message,
    code,
  };

  // rancher/request attaches the HTTP status as a non-enumerable property and
  // rejects with the parsed Kubernetes Status body.
  Object.defineProperty(body, '_status', { value: code });
  return body;
}

function plainFailure(status: number, message: string) {
  const body = { data: message };

  Object.defineProperty(body, '_status', { value: status });
  return body;
}

function fakeStore(responses: unknown[]) {
  const calls: RecordedCall[] = [];
  const queue = [...responses];

  return {
    calls,
    dispatch: vi.fn(async (action: string, payload?: unknown) => {
      calls.push({ action, payload: (payload || {}) as RequestConfig });
      const next = queue.shift();
      if (next instanceof Error || (
        typeof next === 'object' &&
        next !== null &&
        Object.prototype.hasOwnProperty.call(next, '_status')
      )) {
        throw next;
      }
      return next;
    }),
  };
}

function asStore(store: ReturnType<typeof fakeStore>): Dispatchable {
  return store as unknown as Dispatchable;
}

interface ServiceAccountPatch {
  metadata: { resourceVersion: string };
  imagePullSecrets: Array<{ name: string }>;
}

describe('ensureServiceAccountPullSecret', () => {
  it('patches only the complete imagePullSecrets union', async () => {
    const store = fakeStore([
      {
        apiVersion: 'v1',
        kind:       'ServiceAccount',
        metadata:   {
          name:            'app',
          namespace:       'apps',
          resourceVersion: '42',
          labels:          { 'app.kubernetes.io/managed-by': 'Helm' },
          annotations:     { 'meta.helm.sh/release-name': 'app' },
          ownerReferences: [{ name: 'owner' }],
        },
        secrets:                      [{ name: 'token' }],
        automountServiceAccountToken: false,
        imagePullSecrets:             [{ name: 'existing-pull-secret' }],
      },
      {},
    ]);

    await ensureServiceAccountPullSecret(asStore(store), 'local', 'apps', 'app', 'new-pull-secret');

    expect(store.calls).toHaveLength(2);
    const update = store.calls[1].payload;
    expect(update.method).toBe('PATCH');
    expect(update.headers).toEqual({ 'Content-Type': 'application/merge-patch+json' });
    expect(update.data).toEqual({
      metadata:         { resourceVersion: '42' },
      imagePullSecrets: [
        { name: 'existing-pull-secret' },
        { name: 'new-pull-secret' },
      ],
    });
  });

  it('adds the first pull secret when imagePullSecrets is absent', async () => {
    const store = fakeStore([
      { metadata: { name: 'app', namespace: 'apps', resourceVersion: '42' } },
      {},
    ]);

    await ensureServiceAccountPullSecret(asStore(store), 'local', 'apps', 'app', 'new-pull-secret');

    expect(store.calls[1].payload.data).toEqual({
      metadata:         { resourceVersion: '42' },
      imagePullSecrets: [{ name: 'new-pull-secret' }],
    });
  });

  it('accepts the wrapped response shape', async () => {
    const store = fakeStore([
      {
        data: {
          metadata:         { name: 'app', namespace: 'apps', resourceVersion: '42' },
          imagePullSecrets: [{ name: 'existing-pull-secret' }],
        },
      },
      {},
    ]);

    await ensureServiceAccountPullSecret(asStore(store), 'local', 'apps', 'app', 'new-pull-secret');

    expect(store.calls[1].payload.method).toBe('PATCH');
  });

  it('does not write when the pull secret is already attached', async () => {
    const store = fakeStore([{
      metadata:         { name: 'app', namespace: 'apps', resourceVersion: '42' },
      imagePullSecrets: [{ name: 'existing-pull-secret' }],
    }]);

    await ensureServiceAccountPullSecret(asStore(store), 'local', 'apps', 'app', 'existing-pull-secret');

    expect(store.calls).toHaveLength(1);
    expect(store.calls[0].payload.method).toBeUndefined();
  });

  it('fails closed when the GET response has no resourceVersion', async () => {
    const store = fakeStore([{
      metadata:         { name: 'app', namespace: 'apps' },
      imagePullSecrets: [{ name: 'existing-pull-secret' }],
    }]);

    await expect(ensureServiceAccountPullSecret(
      asStore(store), 'local', 'apps', 'app', 'new-pull-secret'
    )).rejects.toThrow('missing metadata.resourceVersion');

    expect(store.calls).toHaveLength(1);
  });

  it('re-reads and preserves a concurrent update after a 409', async () => {
    vi.useFakeTimers();
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const store = fakeStore([
      {
        metadata:         { name: 'app', namespace: 'apps', resourceVersion: '42' },
        imagePullSecrets: [{ name: 'existing-pull-secret' }],
      },
      plainFailure(409, 'the object has been modified'),
      {
        metadata: { name: 'app', namespace: 'apps', resourceVersion: '43' },
        imagePullSecrets: [
          { name: 'existing-pull-secret' },
          { name: 'concurrent-pull-secret' },
        ],
      },
      {},
    ]);

    const update = ensureServiceAccountPullSecret(
      asStore(store), 'local', 'apps', 'app', 'new-pull-secret'
    );
    await vi.runAllTimersAsync();
    await update;

    const patches = store.calls.filter(call => call.payload.method === 'PATCH');
    expect(patches).toHaveLength(2);
    expect(patches[1].payload.data as ServiceAccountPatch).toEqual({
      metadata:         { resourceVersion: '43' },
      imagePullSecrets: [
        { name: 'existing-pull-secret' },
        { name: 'concurrent-pull-secret' },
        { name: 'new-pull-secret' },
      ],
    });
  });

  it('surfaces a conflict after exhausting five fresh-read attempts', async () => {
    vi.useFakeTimers();
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const responses = Array.from({ length: 5 }, (_, index) => [
      {
        metadata: {
          name:            'app',
          namespace:       'apps',
          resourceVersion: String(42 + index),
        },
      },
      plainFailure(409, `conflict ${index + 1}`),
    ]).flat();
    const store = fakeStore(responses);

    const update = ensureServiceAccountPullSecret(
      asStore(store), 'local', 'apps', 'app', 'new-pull-secret'
    );
    const rejection = expect(update).rejects.toMatchObject({ data: 'conflict 5' });
    await vi.runAllTimersAsync();
    await rejection;

    expect(store.calls.filter(call => call.payload.method === 'PATCH')).toHaveLength(5);
    expect(store.calls.filter(call => !call.payload.method)).toHaveLength(5);
  });

  it('propagates a forbidden PATCH', async () => {
    const store = fakeStore([
      { metadata: { name: 'app', namespace: 'apps', resourceVersion: '42' } },
      k8sFailure(403, 'serviceaccounts is forbidden'),
    ]);

    await expect(ensureServiceAccountPullSecret(
      asStore(store), 'local', 'apps', 'app', 'new-pull-secret'
    )).rejects.toMatchObject({ code: 403 });
  });

  it('encodes every resource-path segment', async () => {
    const store = fakeStore([
      { metadata: { name: '../secrets/admin', namespace: 'apps', resourceVersion: '42' } },
      {},
    ]);

    await ensureServiceAccountPullSecret(
      asStore(store),
      'local/../c-malice',
      'apps/../kube-system',
      '../secrets/admin',
      'new-pull-secret',
    );

    const expectedUrl = '/k8s/clusters/local%2F..%2Fc-malice/api/v1/namespaces/' +
      'apps%2F..%2Fkube-system/serviceaccounts/..%2Fsecrets%2Fadmin';
    expect(store.calls[0].payload.url).toBe(expectedUrl);
    expect(store.calls[1].payload.url).toBe(expectedUrl);
  });
});

describe('ServiceAccount discovery and sweep', () => {
  it('reads raw lists, scopes to Helm-managed accounts, and includes default exactly once', async () => {
    const store = fakeStore([{
      items: [
        { metadata: { name: 'default', namespace: 'apps' } },
        {
          metadata: {
            name:      'app',
            namespace: 'apps',
            labels:    { 'app.kubernetes.io/managed-by': 'Helm' },
          }
        },
        { metadata: { name: 'worker', namespace: 'apps' } },
      ],
    }]);

    await expect(listServiceAccounts(asStore(store), 'local', 'apps')).resolves.toEqual([
      'default',
      'app',
    ]);
    expect(store.calls[0].payload.url).toBe(
      '/k8s/clusters/local/api/v1/namespaces/apps/serviceaccounts?limit=5000'
    );
  });

  it('ignores a ServiceAccount deleted after listing and updates the rest', async () => {
    const store = fakeStore([
      {
        items: [{
          metadata: {
            name:      'app',
            namespace: 'apps',
            labels:    { 'app.kubernetes.io/managed-by': 'Helm' },
          }
        }],
      },
      plainFailure(404, 'serviceaccount default not found'),
      { metadata: { name: 'app', namespace: 'apps', resourceVersion: '42' } },
      {},
    ]);

    await ensurePullSecretOnAllSAs(asStore(store), 'local', 'apps', 'new-pull-secret');

    const patch = store.calls.find(call => call.payload.method === 'PATCH');
    expect(patch?.payload.url).toContain('/serviceaccounts/app');
  });

  it('propagates a forbidden ServiceAccount update', async () => {
    const store = fakeStore([
      { items: [] },
      { metadata: { name: 'default', namespace: 'apps', resourceVersion: '42' } },
      k8sFailure(403, 'serviceaccounts is forbidden'),
    ]);

    const error = await ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    ).catch(e => e);

    expect(error).toBeInstanceOf(Error);
    expect(error.message).toContain('default: serviceaccounts is forbidden');
    expect(httpStatus(error)).toBe(403);
  });

  it('continues updating other ServiceAccounts before surfacing a failure', async () => {
    const store = fakeStore([
      {
        items: [{
          metadata: {
            name:      'app',
            namespace: 'apps',
            labels:    { 'app.kubernetes.io/managed-by': 'Helm' },
          }
        }],
      },
      plainFailure(403, 'serviceaccount default is forbidden'),
      { metadata: { name: 'app', namespace: 'apps', resourceVersion: '42' } },
      {},
    ]);

    const error = await ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    ).catch(e => e);

    expect(error.message).toContain('default: serviceaccount default is forbidden');
    expect(httpStatus(error)).toBe(403);
    const patch = store.calls.find(call => call.payload.method === 'PATCH');
    expect(patch?.payload.url).toContain('/serviceaccounts/app');
  });

  it('aggregates multiple failures and preserves their common status', async () => {
    const store = fakeStore([
      {
        items: [{
          metadata: {
            name:      'app',
            namespace: 'apps',
            labels:    { 'app.kubernetes.io/managed-by': 'Helm' },
          }
        }],
      },
      plainFailure(503, 'default unavailable'),
      plainFailure(503, 'app unavailable'),
    ]);

    const error = await ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    ).catch(e => e);

    expect(error.message).toContain('default: default unavailable');
    expect(error.message).toContain('app: app unavailable');
    expect(httpStatus(error)).toBe(503);
  });

  it('leaves a mixed-status aggregate unclassified', async () => {
    const store = fakeStore([
      {
        items: [{
          metadata: {
            name:      'app',
            namespace: 'apps',
            labels:    { 'app.kubernetes.io/managed-by': 'Helm' },
          }
        }],
      },
      plainFailure(403, 'default forbidden'),
      plainFailure(503, 'app unavailable'),
    ]);

    const error = await ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    ).catch(e => e);

    expect(error.message).toContain('default: default forbidden');
    expect(error.message).toContain('app: app unavailable');
    expect(httpStatus(error)).toBeUndefined();
  });
});

describe('registry Secret discovery', () => {
  it('reuses a Secret from a raw Kubernetes list response', async () => {
    const store = fakeStore([{
      items: [{
        metadata: { name: 'clusterrepo-auth-app-dockercfg', namespace: 'apps' },
        type:     'kubernetes.io/dockerconfigjson',
        data:     { '.dockerconfigjson': 'sensitive-base64-data' },
      }],
    }]);

    await expect(ensureRegistrySecret(
      asStore(store),
      'local',
      'apps',
      'registry.example.com',
      'clusterrepo-auth-app',
      'user',
      'password',
    )).resolves.toBe('clusterrepo-auth-app-dockercfg');

    expect(store.calls).toHaveLength(1);
  });
});
