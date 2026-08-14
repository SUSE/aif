import { describe, expect, it, vi } from 'vitest';
import { ensureServiceAccountPullSecret } from '../rancher-apps';
import type { Dispatchable, RancherRequestConfig } from '../../types/rancher-types';

function fakeStore(responses: unknown[]) {
  const calls: Array<{ action: string; payload: RancherRequestConfig }> = [];
  const queue = [...responses];

  return {
    calls,
    dispatch: vi.fn(async (action: string, payload: RancherRequestConfig) => {
      calls.push({ action, payload });
      return queue.shift();
    }),
  };
}

describe('ensureServiceAccountPullSecret', () => {
  it('patches only imagePullSecrets and leaves ServiceAccount metadata untouched', async () => {
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

    await ensureServiceAccountPullSecret(
      store as unknown as Dispatchable,
      'local',
      'apps',
      'app',
      'new-pull-secret',
    );

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
    expect(update.data).not.toHaveProperty('apiVersion');
    expect(update.data).not.toHaveProperty('kind');
    expect(update.data).not.toHaveProperty('secrets');
    expect(update.data).not.toHaveProperty('automountServiceAccountToken');
    expect(update.data.metadata).not.toHaveProperty('labels');
    expect(update.data.metadata).not.toHaveProperty('annotations');
    expect(update.data.metadata).not.toHaveProperty('ownerReferences');
  });

  it('does not write when the pull secret is already attached', async () => {
    const store = fakeStore([{
      metadata:         { name: 'app', namespace: 'apps', resourceVersion: '42' },
      imagePullSecrets: [{ name: 'existing-pull-secret' }],
    }]);

    await ensureServiceAccountPullSecret(
      store as unknown as Dispatchable,
      'local',
      'apps',
      'app',
      'existing-pull-secret',
    );

    expect(store.calls).toHaveLength(1);
    expect(store.calls[0].payload.method).toBeUndefined();
  });
});
