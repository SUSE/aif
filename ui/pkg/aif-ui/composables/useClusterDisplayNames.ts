import { ref } from 'vue';
import { getAllClusters } from '../services/rancher-apps';
import type { Dispatchable } from '../types/rancher-types';

// Resolves Rancher management cluster IDs (e.g. "c-m-abc123") to the display
// names users see elsewhere in Rancher. Unready clusters are included so an
// unreachable target still shows its name; unknown IDs fall back to the ID.
export function useClusterDisplayNames(store: Dispatchable) {
  const clusterNames = ref<Record<string, string>>({});

  async function loadClusterDisplayNames(): Promise<void> {
    try {
      const clusters = await getAllClusters(store);

      clusterNames.value = Object.fromEntries(clusters.map((c) => [c.id, c.name]));
    } catch {
      clusterNames.value = {};
    }
  }

  function clusterDisplayName(clusterId: string): string {
    return clusterNames.value[clusterId] || clusterId;
  }

  return { clusterNames, clusterDisplayName, loadClusterDisplayNames };
}
