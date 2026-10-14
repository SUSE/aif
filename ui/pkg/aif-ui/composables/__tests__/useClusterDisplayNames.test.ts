import { describe, expect, it } from 'vitest';
import { useClusterDisplayNames } from '../useClusterDisplayNames';

function storeWith(clusters: unknown[]) {
  return { dispatch: async() => clusters };
}

describe('useClusterDisplayNames', () => {
  it('maps a management cluster ID to its display name', async() => {
    const { clusterNames, clusterDisplayName, loadClusterDisplayNames } = useClusterDisplayNames(storeWith([
      { id: 'c-m-abc123', metadata: { name: 'c-m-abc123' }, spec: { displayName: 'gpu-alpha' } },
    ]));

    await loadClusterDisplayNames();

    expect(clusterDisplayName('c-m-abc123')).toBe('gpu-alpha');
    expect(clusterNames.value).toEqual({ 'c-m-abc123': 'gpu-alpha' });
  });

  it('includes clusters that are not ready', async() => {
    const { clusterDisplayName, loadClusterDisplayNames } = useClusterDisplayNames(storeWith([
      { id: 'c-m-down', spec: { displayName: 'offline-gpu' }, status: { conditions: [{ type: 'Ready', status: 'False' }] } },
    ]));

    await loadClusterDisplayNames();

    expect(clusterDisplayName('c-m-down')).toBe('offline-gpu');
  });

  it('falls back to the ID for unknown clusters and before loading', async() => {
    const { clusterDisplayName, loadClusterDisplayNames } = useClusterDisplayNames(storeWith([]));

    expect(clusterDisplayName('c-m-abc123')).toBe('c-m-abc123');
    await loadClusterDisplayNames();
    expect(clusterDisplayName('c-m-abc123')).toBe('c-m-abc123');
  });

  it('falls back to the ID when clusters cannot be listed', async() => {
    const { clusterDisplayName, loadClusterDisplayNames } = useClusterDisplayNames({
      dispatch: async() => {
        throw new Error('unreachable');
      },
    });

    await loadClusterDisplayNames();

    expect(clusterDisplayName('c-m-abc123')).toBe('c-m-abc123');
  });
});
