import { describe, it, expect, vi, beforeEach } from 'vitest';

const deleteApp = vi.fn();
const waitForAppUninstall = vi.fn();
const deleteAIWorkload = vi.fn();

vi.mock('../rancher-apps', () => ({
  deleteApp: (...a: any[]) => deleteApp(...a),
  waitForAppUninstall: (...a: any[]) => waitForAppUninstall(...a),
}));
vi.mock('../../utils/operator-api', () => ({ deleteAIWorkload: (...a: any[]) => deleteAIWorkload(...a) }));

import { uninstallWorkload, isHelmStrategy } from '../workload-uninstall';
import type { AIWorkload } from '../../types/aiworkload-types';

function wl(overrides: Partial<AIWorkload['spec']> = {}): AIWorkload {
  return {
    apiVersion: 'ai-factory.suse.com/v1alpha1',
    kind: 'AIWorkload',
    metadata: { name: 'qdrant-local', namespace: 'qdrant-system', uid: 'workload-uid' },
    spec: {
      displayName: 'Qdrant',
      deployStrategy: 'Helm',
      targetNamespace: 'qdrant-system',
      targetClusters: ['local'],
      source: { sourceType: 'App', app: { chartRepo: 'suse-ai', chartName: 'qdrant', chartVersion: '1.0.0', release: 'qdrant' } },
      ...overrides,
    },
  } as AIWorkload;
}

describe('uninstallWorkload', () => {
  const store = { dispatch: vi.fn() } as any;
  beforeEach(() => { deleteApp.mockReset(); deleteAIWorkload.mockReset(); waitForAppUninstall.mockReset().mockResolvedValue(undefined); });

  it('local Helm deletes only the CR so service-identity cleanup has a single executor', async () => {
    await uninstallWorkload(store, wl());
    expect(deleteApp).not.toHaveBeenCalled();
    expect(deleteAIWorkload).toHaveBeenCalledWith('qdrant-system', 'qdrant-local');
  });

  it('downstream Helm waits for caller-session cleanup before confirming CR deletion', async () => {
    await uninstallWorkload(store, wl({ targetClusters: ['c-downstream'] }));
    expect(deleteApp).toHaveBeenCalledWith(store, 'c-downstream', 'qdrant-system', 'qdrant');
    expect(waitForAppUninstall).toHaveBeenCalledWith(store, 'c-downstream', 'qdrant-system', 'qdrant');
    expect(deleteAIWorkload).toHaveBeenCalledWith('qdrant-system', 'qdrant-local', 'workload-uid/c-downstream/qdrant-system/qdrant');
    expect(waitForAppUninstall.mock.invocationCallOrder[0]).toBeLessThan(deleteAIWorkload.mock.invocationCallOrder[0]);
  });

  it.each([{ status: 404 }, { code: 404 }, { response: { status: 404 } }, { _status: 404 }, { status: 'Failure', code: 404 }])('tolerates an already removed downstream release: %j', async (error) => {
    deleteApp.mockRejectedValue(error);
    await uninstallWorkload(store, wl({ targetClusters: ['c-downstream'] }));
    expect(waitForAppUninstall).toHaveBeenCalled();
    expect(deleteAIWorkload).toHaveBeenCalled();
  });

  it('retains the CR when caller-session permissions deny uninstall', async () => {
    deleteApp.mockRejectedValue({ status: 403 });
    await expect(uninstallWorkload(store, wl({ targetClusters: ['c-downstream'] }))).rejects.toBeTruthy();
    expect(deleteAIWorkload).not.toHaveBeenCalled();
  });

  it('retains the CR when asynchronous cleanup has not completed', async () => {
    waitForAppUninstall.mockRejectedValue(new Error('cleanup pending'));
    await expect(uninstallWorkload(store, wl({ targetClusters: ['c-downstream'] }))).rejects.toThrow('cleanup pending');
    expect(deleteAIWorkload).not.toHaveBeenCalled();
  });

  it('refuses an ambiguous direct Helm target before making mutations', async () => {
    await expect(uninstallWorkload(store, wl({ targetClusters: ['c-a', 'c-b'] }))).rejects.toThrow('single Helm target');
    expect(deleteApp).not.toHaveBeenCalled();
    expect(deleteAIWorkload).not.toHaveBeenCalled();
  });

  it('FleetBundle: deletes only the CR (operator finalizer tears down)', async () => {
    deleteAIWorkload.mockResolvedValue(undefined);

    await uninstallWorkload(store, wl({ deployStrategy: 'FleetBundle' }));

    expect(deleteApp).not.toHaveBeenCalled();
    expect(deleteAIWorkload).toHaveBeenCalledWith('qdrant-system', 'qdrant-local');
  });

  it('isHelmStrategy defaults undefined strategy to Helm', () => {
    expect(isHelmStrategy(wl({ deployStrategy: undefined }))).toBe(true);
    expect(isHelmStrategy(wl({ deployStrategy: 'GitOps' }))).toBe(false);
  });
});
