import { describe, expect, it } from 'vitest';
import { appRepository, includeUnavailableApps } from '../repository-health';
import type { AppCollectionItem, ManagedRepo } from '../app-collection';
import { isRepoReady, repoNotReadyMessage } from '../app-collection';

const qdrant: AppCollectionItem = { name: 'Qdrant', slug_name: 'qdrant', library: 'suse-ai', repository_url: 'oci://registry.suse.com/ai/charts' };
const runai: AppCollectionItem = { name: 'Run:ai', slug_name: 'runai', library: 'nvidia', repository_url: 'https://helm.ngc.nvidia.com/nvidia/runai' };
const mirror: ManagedRepo = { name: 'suse-ai-registry', url: 'oci://mirror.internal/custom/suse', library: 'suse-ai', ready: false, message: '401 Unauthorized' };

describe('application repository availability', () => {
  it('resolves static SUSE entries by managed identity after moving to a mirror', () => {
    expect(appRepository(qdrant, [mirror])).toBe(mirror);
  });

  it('does not replace a missing NVIDIA team repo with a healthy public org repo', () => {
    expect(appRepository(runai, [{ name: 'nvidia', url: 'https://helm.ngc.nvidia.com/nvidia', library: 'nvidia', ready: true }])).toBeUndefined();
  });

  it('resolves NVIDIA team entries to the configured OCI mirror', () => {
    const nvidiaMirror: ManagedRepo = { name: 'nvidia', url: 'oci://mirror.internal/nvidia', library: 'nvidia', ready: false };
    expect(appRepository(runai, [nvidiaMirror])).toBe(nvidiaMirror);
  });

  it('keeps known unavailable apps visible with their actual mirror identity', () => {
    expect(includeUnavailableApps([], [qdrant], [mirror])).toEqual([{ ...qdrant, repository_name: mirror.name, repository_url: mirror.url }]);
  });

  it('does not invent missing apps in a healthy partial mirror', () => {
    expect(includeUnavailableApps([], [qdrant], [{ ...mirror, ready: true }])).toEqual([]);
  });

  it('does not duplicate a discovered app or let another repository replace its authoritative name', () => {
    const discovered = { ...qdrant, repository_name: 'suse-ai-registry' };
    expect(includeUnavailableApps([discovered], [qdrant], [mirror])).toEqual([discovered]);
    expect(appRepository({ ...qdrant, repository_name: 'removed-repo' }, [mirror])).toBeUndefined();
  });
});

describe('consistent readiness across catalog, overview and wizard', () => {
  it.each(['generation', 'condition', 'disabled'])('rejects a stale index with %s pending', (kind) => {
    const repo = {
      metadata: { generation: 4 }, spec: { enabled: kind !== 'disabled' },
      status: { indexConfigMapName: 'old-index', observedGeneration: kind === 'generation' ? 3 : 4, conditions: [{ type: 'OCIDownloaded', status: kind === 'condition' ? 'Unknown' : 'True' }] },
    };
    expect(isRepoReady(repo)).toBe(false);
  });

  it('prioritizes a concrete access failure over a pending follower message', () => {
    expect(repoNotReadyMessage({ status: { conditions: [
      { type: 'FollowerDownloaded', status: 'Unknown', message: 'Waiting' },
      { type: 'OCIDownloaded', status: 'False', message: '401 Unauthorized' },
    ] } })).toBe('401 Unauthorized');
  });
});
