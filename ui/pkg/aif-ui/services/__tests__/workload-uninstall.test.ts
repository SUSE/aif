import { describe, it, expect, vi, beforeEach } from 'vitest';

const deleteApp = vi.fn();
const deleteAIWorkload = vi.fn();

vi.mock('../rancher-apps', () => ({ deleteApp: (...a: any[]) => deleteApp(...a) }));
vi.mock('../../utils/operator-api', () => ({ deleteAIWorkload: (...a: any[]) => deleteAIWorkload(...a) }));

import { uninstallWorkload, isHelmStrategy } from '../workload-uninstall';
import type { AIWorkload } from '../../types/aiworkload-types';

function wl(overrides: Partial<AIWorkload['spec']> = {}): AIWorkload {
  return {
    apiVersion: 'ai-factory.suse.com/v1alpha1',
    kind: 'AIWorkload',
    metadata: { name: 'qdrant-local', namespace: 'qdrant-system' },
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
  beforeEach(() => { deleteApp.mockReset(); deleteAIWorkload.mockReset(); });

  it('Helm: uninstalls the release before deleting the CR', async () => {
    deleteApp.mockResolvedValue(undefined);
    deleteAIWorkload.mockResolvedValue(undefined);

    await uninstallWorkload(store, wl());

    expect(deleteApp).toHaveBeenCalledWith(store, 'local', 'qdrant-system', 'qdrant');
    expect(deleteAIWorkload).toHaveBeenCalledWith('qdrant-system', 'qdrant-local');
    // Order: uninstall first, then CR delete.
    expect(deleteApp.mock.invocationCallOrder[0]).toBeLessThan(deleteAIWorkload.mock.invocationCallOrder[0]);
  });

  it('Helm: still deletes the CR when the release is already gone (404-tolerant)', async () => {
    deleteApp.mockRejectedValue({ status: 404, message: 'not found' });
    deleteAIWorkload.mockResolvedValue(undefined);

    await uninstallWorkload(store, wl());

    expect(deleteAIWorkload).toHaveBeenCalledWith('qdrant-system', 'qdrant-local');
  });

  it('Helm: 404-tolerant when the status is carried on a nested response object', async () => {
    deleteApp.mockRejectedValue({ response: { status: 404, data: { message: 'not found' } } });
    deleteAIWorkload.mockResolvedValue(undefined);

    await uninstallWorkload(store, wl());

    expect(deleteAIWorkload).toHaveBeenCalledWith('qdrant-system', 'qdrant-local');
  });

  it('Helm: 404-tolerant when the status is carried on `code`', async () => {
    deleteApp.mockRejectedValue({ code: 404, message: 'not found' });
    deleteAIWorkload.mockResolvedValue(undefined);

    await uninstallWorkload(store, wl());

    expect(deleteAIWorkload).toHaveBeenCalledWith('qdrant-system', 'qdrant-local');
  });

  it('Helm: does NOT delete the CR when uninstall fails for a real reason', async () => {
    deleteApp.mockRejectedValue({ status: 403, message: 'forbidden' });

    await expect(uninstallWorkload(store, wl())).rejects.toBeTruthy();
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
