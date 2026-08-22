import { describe, expect, it, vi } from 'vitest';
import {
  ensurePullSecretOnAllSAs,
  ensureRegistrySecret,
  ensureServiceAccountPullSecret,
  listServiceAccounts,
} from '../rancher-apps';
import type { Dispatchable } from '../../types/rancher-types';

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

    await ensureServiceAccountPullSecret(asStore(store), 'local', 'apps', 'app', 'new-pull-secret');

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

    await expect(ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    )).rejects.toMatchObject({ code: 403 });
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

    await expect(ensurePullSecretOnAllSAs(
      asStore(store), 'local', 'apps', 'new-pull-secret'
    )).rejects.toMatchObject({ data: 'serviceaccount default is forbidden' });

    const patch = store.calls.find(call => call.payload.method === 'PATCH');
    expect(patch?.payload.url).toContain('/serviceaccounts/app');
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
