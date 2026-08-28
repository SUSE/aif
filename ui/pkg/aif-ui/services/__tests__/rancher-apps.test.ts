import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  ensurePullSecretOnAllSAs,
  ensureServiceAccountPullSecret,
  listServiceAccounts,
} from '../rancher-apps';
import { log as logger } from '../../utils/logger';
import type { Dispatchable } from '../../types/rancher-types';

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

function withStatus<T extends object>(body: T, status: number): T {
  // rancher/request attaches the HTTP status as a non-enumerable property
  // before rejecting with the parsed response body.
  Object.defineProperty(body, '_status', { value: status });
  return body;
}

function k8sFailure(code: number, message: string) {
  return withStatus({
    apiVersion: 'v1',
    kind:       'Status',
    status:     'Failure',
    message,
    code,
  }, code);
}

function plainFailure(status: number, message: string) {
  return withStatus({ data: message }, status);
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

  it('propagates a forbidden GET without attempting a write', async () => {
    const store = fakeStore([
      k8sFailure(403, 'serviceaccounts is forbidden'),
    ]);

    await expect(ensureServiceAccountPullSecret(
      asStore(store), 'local', 'apps', 'app', 'new-pull-secret'
    )).rejects.toMatchObject({ code: 403 });

    expect(store.calls).toHaveLength(1);
    expect(store.calls[0].payload.method).toBeUndefined();
  });

  it('does not retry non-HTTP numeric status codes', async () => {
    const store = fakeStore([
      { metadata: { name: 'app', namespace: 'apps', resourceVersion: '42' } },
      Object.assign(new Error('operation aborted'), { code: 20 }),
    ]);

    await expect(ensureServiceAccountPullSecret(
      asStore(store), 'local', 'apps', 'app', 'new-pull-secret'
    )).rejects.toMatchObject({ message: 'operation aborted', code: 20 });

    expect(store.calls).toHaveLength(2);
    expect(store.calls.filter(call => call.payload.method === 'PATCH')).toHaveLength(1);
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
  it('uses the Helm label selector and includes default exactly once', async () => {
    const store = fakeStore([{
      items: [
        { metadata: { name: 'default', namespace: 'apps' } },
        { metadata: { name: 'app', namespace: 'apps' } },
      ],
    }]);

    await expect(listServiceAccounts(asStore(store), 'local', 'apps')).resolves.toEqual([
      'default',
      'app',
    ]);
    expect(store.calls[0].payload.url).toBe(
      '/k8s/clusters/local/api/v1/namespaces/apps/serviceaccounts?limit=5000' +
      '&labelSelector=app.kubernetes.io%2Fmanaged-by%3DHelm'
    );
  });

  it('ignores a ServiceAccount deleted after listing and updates the rest', async () => {
    const warn = vi.spyOn(logger, 'warn').mockImplementation(() => {});
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
    expect(warn).not.toHaveBeenCalled();
  });

  it('continues updating other ServiceAccounts after a forbidden update', async () => {
    const warn = vi.spyOn(logger, 'warn').mockImplementation(() => {});
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

    await expect(ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    )).resolves.toBeUndefined();

    const patch = store.calls.find(call => call.payload.method === 'PATCH');
    expect(patch?.payload.url).toContain('/serviceaccounts/app');
    expect(warn).toHaveBeenCalledOnce();
    expect(warn).toHaveBeenCalledWith('ServiceAccount pull-secret attachment failed', {
      component: 'RancherApps',
      action:    'attach image pull secret',
      data:      {
        namespace:      'apps',
        serviceAccount: 'default',
        status:         403,
        message:        'serviceaccount default is forbidden',
      },
    });
  });

  it('logs the fail-closed message when resourceVersion is missing', async () => {
    const warn = vi.spyOn(logger, 'warn').mockImplementation(() => {});
    const store = fakeStore([
      { items: [] },
      { metadata: { name: 'default', namespace: 'apps' } },
    ]);

    await expect(ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    )).resolves.toBeUndefined();

    expect(warn).toHaveBeenCalledOnce();
    expect(warn).toHaveBeenCalledWith('ServiceAccount pull-secret attachment failed', {
      component: 'RancherApps',
      action:    'attach image pull secret',
      data:      {
        namespace:      'apps',
        serviceAccount: 'default',
        status:         undefined,
        message:        'Refusing to update ServiceAccount apps/default: GET response is missing metadata.resourceVersion',
      },
    });
  });

  it('bounds logged error messages to 1000 characters', async () => {
    const warn = vi.spyOn(logger, 'warn').mockImplementation(() => {});
    const longMessage = 'x'.repeat(1_001);
    const store = fakeStore([
      { items: [] },
      k8sFailure(403, longMessage),
    ]);

    await ensurePullSecretOnAllSAs(asStore(store), 'local', 'apps', 'new-pull-secret');

    expect(warn).toHaveBeenCalledOnce();
    expect(warn).toHaveBeenCalledWith('ServiceAccount pull-secret attachment failed', {
      component: 'RancherApps',
      action:    'attach image pull secret',
      data:      {
        namespace:      'apps',
        serviceAccount: 'default',
        status:         403,
        message:        'x'.repeat(1_000),
      },
    });
  });

  it.each([
    ['data.message', withStatus({ data: { message: 'nested data message' } }, 403), 'nested data message'],
    [
      'response.data message',
      withStatus({ response: { data: { message: 'nested response message' }, status: 403 } }, 403),
      'nested response message',
    ],
    [
      'response.data error',
      withStatus({ response: { data: { error: 'nested response error' }, status: 403 } }, 403),
      'nested response error',
    ],
  ])('logs the %s error shape', async (_shape, failure, expectedMessage) => {
    const warn = vi.spyOn(logger, 'warn').mockImplementation(() => {});
    const store = fakeStore([
      { items: [] },
      failure,
    ]);

    await ensurePullSecretOnAllSAs(asStore(store), 'local', 'apps', 'new-pull-secret');

    expect(warn).toHaveBeenCalledOnce();
    expect(warn).toHaveBeenCalledWith('ServiceAccount pull-secret attachment failed', {
      component: 'RancherApps',
      action:    'attach image pull secret',
      data:      {
        namespace:      'apps',
        serviceAccount: 'default',
        status:         403,
        message:        expectedMessage,
      },
    });
  });

  it('retries transient discovery failures before updating all ServiceAccounts', async () => {
    vi.useFakeTimers();
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const warn = vi.spyOn(logger, 'warn').mockImplementation(() => {});
    const store = fakeStore([
      plainFailure(503, 'cluster agent unavailable'),
      {
        items: [{
          metadata: {
            name:      'app',
            namespace: 'apps',
            labels:    { 'app.kubernetes.io/managed-by': 'Helm' },
          }
        }],
      },
      { metadata: { name: 'default', namespace: 'apps', resourceVersion: '41' } },
      {},
      { metadata: { name: 'app', namespace: 'apps', resourceVersion: '42' } },
      {},
    ]);

    const sweep = ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    );
    await vi.runAllTimersAsync();
    await sweep;

    const listCalls = store.calls.filter(call => call.payload.url?.includes('?limit=5000'));
    const patches = store.calls.filter(call => call.payload.method === 'PATCH');
    expect(listCalls).toHaveLength(2);
    expect(patches.map(call => call.payload.url)).toEqual([
      '/k8s/clusters/local/api/v1/namespaces/apps/serviceaccounts/default',
      '/k8s/clusters/local/api/v1/namespaces/apps/serviceaccounts/app',
    ]);
    expect(warn).not.toHaveBeenCalled();
  });

  it('falls back to default after exhausting transient discovery retries', async () => {
    vi.useFakeTimers();
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const warn = vi.spyOn(logger, 'warn').mockImplementation(() => {});
    const store = fakeStore([
      ...Array.from({ length: 5 }, () => plainFailure(503, 'cluster agent unavailable')),
      { metadata: { name: 'default', namespace: 'apps', resourceVersion: '42' } },
      {},
    ]);

    const sweep = ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    );
    await vi.runAllTimersAsync();
    await sweep;

    const listCalls = store.calls.filter(call => call.payload.url?.includes('?limit=5000'));
    const patches = store.calls.filter(call => call.payload.method === 'PATCH');
    expect(listCalls).toHaveLength(5);
    expect(patches).toHaveLength(1);
    expect(patches[0].payload.url).toContain('/serviceaccounts/default');
    expect(warn).toHaveBeenCalledOnce();
    expect(warn).toHaveBeenCalledWith('ServiceAccount discovery failed; falling back to default', {
      component: 'RancherApps',
      action:    'discover service accounts',
      data:      {
        namespace: 'apps',
        status:    503,
        message:   'cluster agent unavailable',
      },
    });
  });

  it('falls back to default when ServiceAccount discovery is forbidden', async () => {
    const warn = vi.spyOn(logger, 'warn').mockImplementation(() => {});
    const store = fakeStore([
      k8sFailure(403, 'serviceaccounts is forbidden'),
      { metadata: { name: 'default', namespace: 'apps', resourceVersion: '42' } },
      {},
    ]);

    await expect(ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    )).resolves.toBeUndefined();

    const patch = store.calls.find(call => call.payload.method === 'PATCH');
    const listCalls = store.calls.filter(call => call.payload.url?.includes('?limit=5000'));
    expect(patch?.payload.url).toContain('/serviceaccounts/default');
    expect(listCalls).toHaveLength(1);
    expect(warn).toHaveBeenCalledOnce();
    expect(warn).toHaveBeenCalledWith('ServiceAccount discovery failed; falling back to default', {
      component: 'RancherApps',
      action:    'discover service accounts',
      data:      {
        namespace: 'apps',
        status:    403,
        message:   'serviceaccounts is forbidden',
      },
    });
  });
});
