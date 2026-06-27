<template>
  <div class="cluster-resource-table">
    <!-- Loading state -->
    <div
      v-if="loading"
      class="table-loading"
    >
      <div class="loading-text">
        Checking cluster resources...
      </div>
    </div>

    <!-- Error state -->
    <div
      v-else-if="error"
      class="table-error"
    >
      <div class="error-text">
        {{ error }}
      </div>
      <div class="error-hint">
        Showing basic cluster information only
      </div>
    </div>

    <!-- Cluster selection table -->
    <div
      v-else-if="clusters.length > 0"
      class="table-container"
    >
      <table class="cluster-table table">
        <thead>
          <tr>
            <th class="col-select">
              <!-- Select All checkbox for multi-select mode -->
              <Checkbox
                v-if="multiSelect"
                :value="allSelectableSelected"
                :indeterminate="someButNotAllSelected"
                :disabled="disabled"
                title="Select all ready clusters"
                @update:value="toggleSelectAllSelectable"
              />
            </th>
            <th class="col-cluster">
              Cluster
            </th>
            <th class="col-nodes">
              Nodes
            </th>
            <th class="col-cpu">
              CPU
            </th>
            <th class="col-memory">
              Memory
            </th>
            <th class="col-gpu">
              GPU
            </th>
            <th class="col-status">
              Status
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="cluster in clusters"
            :key="cluster.clusterId"
            class="cluster-row"
            :class="{
              'row-selected': isClusterSelected(cluster.clusterId),
              'row-disabled': disabled,
              'row-unavailable': cluster.status === 'unavailable'
            }"
            @click="multiSelect ? toggleCluster(cluster.clusterId) : selectSingleCluster(cluster.clusterId)"
          >
            <td class="col-select">
              <!-- Checkbox for multi-select mode -->
              <span
                v-if="multiSelect"
                @click.stop
              >
                <Checkbox
                  :value="isClusterSelected(cluster.clusterId)"
                  :disabled="disabled || cluster.status === 'unavailable'"
                  @update:value="toggleCluster(cluster.clusterId)"
                />
              </span>
              <!-- Radio button for single-select mode -->
              <input
                v-else
                type="radio"
                :name="`cluster-select-${tableId}`"
                :value="cluster.clusterId"
                :checked="isClusterSelected(cluster.clusterId)"
                :disabled="disabled || cluster.status === 'unavailable'"
                class="cluster-radio"
                @change="selectSingleCluster(cluster.clusterId)"
              />
            </td>
            <td class="col-cluster">
              <div class="cluster-name">
                {{ cluster.name }}
              </div>
            </td>
            <td class="col-nodes">
              <span v-if="cluster.nodeCount > 0">{{ cluster.nodeCount }}</span>
              <span
                v-else
                class="no-resource"
              >—</span>
            </td>
            <td class="col-cpu">
              <div
                v-if="cluster.resources.cpu.total > 0"
                class="resource-bar-container"
              >
                <ProgressBarMulti
                  :values="[{ color: getResourceBarColor(cluster.resources.cpu.used, cluster.resources.cpu.total), value: cluster.resources.cpu.used }]"
                  :max="cluster.resources.cpu.total"
                  class="resource-bar"
                />
                <div class="resource-percentage">
                  {{ Math.ceil((cluster.resources.cpu.used / cluster.resources.cpu.total) * 100) }}%
                </div>
              </div>
              <span
                v-else
                class="no-resource"
              >Unknown</span>
            </td>
            <td class="col-memory">
              <div
                v-if="cluster.resources.memory.total > 0"
                class="resource-bar-container"
              >
                <ProgressBarMulti
                  :values="[{ color: getResourceBarColor(cluster.resources.memory.used, cluster.resources.memory.total), value: cluster.resources.memory.used }]"
                  :max="cluster.resources.memory.total"
                  class="resource-bar"
                />
                <div class="resource-percentage">
                  {{ Math.ceil((cluster.resources.memory.used / cluster.resources.memory.total) * 100) }}%
                </div>
              </div>
              <span
                v-else
                class="no-resource"
              >Unknown</span>
            </td>
            <td class="col-gpu">
              <div
                v-if="cluster.resources.gpu && cluster.resources.gpu.total > 0"
                class="resource-bar-container"
              >
                <ProgressBarMulti
                  :values="[{ color: getResourceBarColor(cluster.resources.gpu.used, cluster.resources.gpu.total), value: cluster.resources.gpu.used }]"
                  :max="cluster.resources.gpu.total"
                  class="resource-bar"
                />
                <div class="resource-percentage">
                  {{ Math.ceil((cluster.resources.gpu.used / cluster.resources.gpu.total) * 100) }}%
                </div>
              </div>
              <span
                v-else
                class="no-resource"
              >—</span>
            </td>
            <td class="col-status">
              <StatusBadge
                :status="getStatusBadgeStatus(cluster.status)"
                :title="cluster.statusMessage || (cluster.status === 'unavailable' ? 'Not ready' : 'Ready')"
              />
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- No clusters state -->
    <div
      v-else
      class="no-clusters"
    >
      <div class="no-clusters-text">
        No clusters available
      </div>
      <div class="no-clusters-hint">
        Check your cluster connections and permissions
      </div>
    </div>

    <!-- Selected cluster details (single-select mode) -->
    <div
      v-if="!multiSelect && selectedClusters.length === 1 && selectedClusterInfo"
      class="selected-info"
    >
      <div class="selected-header">
        Selected: {{ selectedClusterInfo.name }}
      </div>
    </div>

    <!-- Selected clusters display (multi-select mode) -->
    <div
      v-if="multiSelect && selectedClusters.length > 0"
      class="selected-info"
    >
      <div class="selected-header">
        Selected: {{ selectedClusters.length }} cluster{{ selectedClusters.length !== 1 ? 's' : '' }}
      </div>
      <div class="selected-clusters-chips">
        <span
          v-for="clusterId in selectedClusters"
          :key="clusterId"
          class="cluster-chip"
          :class="getClusterChipClass(clusterId)"
        >
          {{ getClusterName(clusterId) }}
          <button
            class="chip-remove"
            :disabled="disabled"
            title="Remove"
            @click="toggleCluster(clusterId)"
          >×</button>
        </span>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted, PropType, getCurrentInstance } from 'vue';
import { getAllClusters } from '../../services/rancher-apps';
import { Checkbox } from '@components/Form/Checkbox';
import ProgressBarMulti from '@shell/components/ProgressBarMulti';
import StatusBadge from '@shell/components/StatusBadge';
import {
  getAllClusterResourceMetrics,
  type ClusterResourceSummary
} from '../../services/cluster-resources';
import logger from '../../utils/logger';

let tableIdCounter = 0;

export default defineComponent({
  name: 'ClusterResourceTable',
  components: { Checkbox, ProgressBarMulti, StatusBadge },
  props: {
    // Array-based selection - works for both single and multi-select modes
    // For single-select, parent enforces array.length <= 1
    selectedClusters: { type: Array as PropType<string[]>, default: () => [] },
    // Controls visual appearance: radio buttons (false) vs checkboxes (true)
    multiSelect: { type: Boolean, default: false },
    disabled: { type: Boolean, default: false }
  },
  emits: ['update:selectedClusters'],
  setup(props, { emit }) {
    const tableId = ++tableIdCounter; // Unique ID for radio button grouping
    const loading = ref(true);
    const error = ref<string | null>(null);
    const clusters = ref<ClusterResourceSummary[]>([]);

    // Get info for the first selected cluster (used in single-select mode display)
    const selectedClusterInfo = computed(() => {
      if (props.selectedClusters.length === 0) return null;
      return clusters.value.find(c => c.clusterId === props.selectedClusters[0]);
    });

    // Selectable clusters (all non-unavailable)
    const selectableClusters = computed(() => {
      return clusters.value.filter(c => c.status !== 'unavailable');
    });

    // Check if all selectable clusters are selected
    const allSelectableSelected = computed(() => {
      if (selectableClusters.value.length === 0) return false;
      return selectableClusters.value.every(c => props.selectedClusters.includes(c.clusterId));
    });

    // Check if some but not all selectable clusters are selected (for indeterminate state)
    const someButNotAllSelected = computed(() => {
      if (selectableClusters.value.length === 0) return false;
      const selectedSelectable = selectableClusters.value.filter(c => props.selectedClusters.includes(c.clusterId));
      return selectedSelectable.length > 0 && selectedSelectable.length < selectableClusters.value.length;
    });

    // Helper to emit updated selection
    function emitSelection(newSelection: string[]) {
      emit('update:selectedClusters', newSelection);
    }

    async function loadClusterResources() {
      try {
        loading.value = true;
        error.value = null;

        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const vm = getCurrentInstance()?.proxy as any;
        const store = vm?.$store;

        logger.debug('[SUSE-AI] ClusterResourceTable: Loading cluster resources...');
        const clusterSummaries = await getAllClusterResourceMetrics(store);

        clusters.value = clusterSummaries;
        logger.debug(`[SUSE-AI] ClusterResourceTable: Loaded ${clusters.value.length} clusters`);

        // Auto-select first ready cluster if none selected
        if (props.selectedClusters.length === 0 && clusterSummaries.length > 0) {
          const firstSelectable = clusterSummaries.find(c => c.status !== 'unavailable');
          if (firstSelectable) {
            emitSelection([firstSelectable.clusterId]);
            logger.debug(`[SUSE-AI] ClusterResourceTable: Auto-selected first cluster: ${firstSelectable.clusterId}`);
          }
        }
      } catch (e: unknown) {
        logger.error('[SUSE-AI] ClusterResourceTable: Failed to load cluster resources', e);
        error.value = (e instanceof Error ? e.message : null) || 'Failed to load cluster information';

        // Try to load basic cluster list as fallback — use getAllClusters so readiness
        // is applied and unhealthy clusters stay non-selectable even in the error path.
        try {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const vm = getCurrentInstance()?.proxy as any;
          const store = vm.$store;
          const basicClusters = await getAllClusters(store);
          clusters.value = (basicClusters || []).map((c) => ({
            clusterId:     c.id,
            name:          c.name,
            nodeCount:     0,
            resources:     { cpu: { used: 0, total: 0 }, memory: { used: 0, total: 0 } },
            status:        c.ready ? 'ready' : 'unavailable',
            statusMessage: c.ready ? 'Resource information unavailable' : 'Cluster is not ready',
            storageClasses: [],
            lastUpdated:   new Date(),
            nodes:         []
          }));
        } catch (fallbackError) {
          logger.error('[SUSE-AI] ClusterResourceTable: Fallback also failed', fallbackError);
        }
      } finally {
        loading.value = false;
      }
    }

    // Check if cluster is selected
    function isClusterSelected(clusterId: string): boolean {
      return props.selectedClusters.includes(clusterId);
    }

    // Single-select mode: replace selection with single cluster
    function selectSingleCluster(clusterId: string) {
      if (props.disabled) return;
      const cluster = clusters.value.find(c => c.clusterId === clusterId);
      if (cluster?.status === 'unavailable') return;
      emitSelection([clusterId]);
    }

    // Multi-select mode: toggle cluster in selection
    function toggleCluster(clusterId: string) {
      if (props.disabled) return;
      const cluster = clusters.value.find(c => c.clusterId === clusterId);
      if (cluster?.status === 'unavailable') return;

      const current = [...props.selectedClusters];
      const index = current.indexOf(clusterId);

      if (index === -1) {
        current.push(clusterId);
      } else {
        current.splice(index, 1);
      }

      emitSelection(current);
    }

    // Multi-select mode: toggle select all selectable clusters
    function toggleSelectAllSelectable() {
      if (props.disabled) return;

      if (allSelectableSelected.value) {
        const selectableIds = selectableClusters.value.map(c => c.clusterId);
        emitSelection(props.selectedClusters.filter(id => !selectableIds.includes(id)));
      } else {
        const selectableIds = selectableClusters.value.map(c => c.clusterId);
        const current = new Set(props.selectedClusters);
        selectableIds.forEach(id => current.add(id));
        emitSelection(Array.from(current));
      }
    }

    // Get cluster name by ID
    function getClusterName(clusterId: string): string {
      const cluster = clusters.value.find(c => c.clusterId === clusterId);
      return cluster?.name || clusterId;
    }

    // Get chip class based on cluster status
    function getClusterChipClass(clusterId: string): string {
      const cluster = clusters.value.find(c => c.clusterId === clusterId);
      if (!cluster) return '';
      return cluster.status === 'unavailable' ? 'chip-unavailable' : '';
    }

    function getResourceBarColor(used: number, total: number): string {
      if (total === 0) return 'bg-success';
      const pct = (used / total) * 100;
      if (pct >= 90) return 'bg-error';
      if (pct >= 70) return 'bg-warning';
      return 'bg-success';
    }

    function getStatusBadgeStatus(status: ClusterResourceSummary['status']): 'success' | 'error' {
      return status === 'unavailable' ? 'error' : 'success';
    }

    onMounted(() => {
      loadClusterResources();
    });

    return {
      tableId,
      loading,
      error,
      clusters,
      selectedClusterInfo,
      selectableClusters,
      allSelectableSelected,
      someButNotAllSelected,
      isClusterSelected,
      selectSingleCluster,
      toggleCluster,
      toggleSelectAllSelectable,
      getClusterName,
      getClusterChipClass,
      getResourceBarColor,
      getStatusBadgeStatus
    };
  }
});
</script>

<style scoped>
.cluster-resource-table {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.table-loading,
.table-error,
.no-clusters {
  padding: 24px;
  text-align: center;
  color: var(--muted, #64748b);
  border: 1px solid var(--border, #e2e8f0);
  border-radius: 8px;
  background: var(--box-bg, var(--body-bg));
}

.error-text {
  color: var(--error, #dc2626);
  font-weight: 600;
  margin-bottom: 4px;
}

.error-hint {
  font-size: 12px;
  color: var(--muted, #64748b);
}

.no-clusters-text {
  font-weight: 600;
  margin-bottom: 4px;
}

.no-clusters-hint {
  font-size: 12px;
}

.table-container {
  border: 1px solid var(--border, #e2e8f0);
  border-radius: var(--border-radius-lg, 8px);
  overflow: hidden;
  background: var(--body-bg);
  box-shadow: 0 2px 4px rgba(15, 23, 42, 0.05);
}

.cluster-table {
  width: 100%;
  border-collapse: collapse;
  background: inherit;
}

.cluster-table th {
  background: var(--sortable-table-header-bg);
  border-bottom: 1px solid var(--border);
  padding: 12px;
  text-align: left;
  font-size: 13px;
  font-weight: 600;
  color: var(--body-text);
}

.cluster-table th.col-select {
  width: 40px;
  text-align: center;
}

.cluster-table th.col-cluster {
  text-align: left;
  width: auto;
  min-width: 120px;
}

.cluster-table th.col-nodes {
  width: 60px;
  text-align: center;
}

.cluster-table th.col-cpu,
.cluster-table th.col-memory,
.cluster-table th.col-gpu {
  width: 120px;
  text-align: center;
}

.cluster-table th.col-status {
  width: 60px;
  text-align: center;
}

.cluster-row {
  cursor: pointer;
  transition: background-color 0.15s ease;
}

.cluster-row:hover {
  background: var(--sortable-table-accent-bg);
}

.cluster-row.row-disabled {
  cursor: default;
  pointer-events: none;
}

.cluster-row.row-disabled:hover {
  background: inherit;
}

.cluster-row.row-selected {
  background: var(--primary-banner-bg, rgba(59, 130, 246, 0.15));
}

.cluster-table td {
  padding: 12px;
  border-bottom: 1px solid var(--border);
  text-align: left;
  font-size: 14px;
  vertical-align: middle;
}

.cluster-table tr:last-child td {
  border-bottom: none;
}


.cluster-table td.col-select,
.cluster-table td.col-nodes,
.cluster-table td.col-cpu,
.cluster-table td.col-memory,
.cluster-table td.col-gpu,
.cluster-table td.col-status {
  text-align: center;
}

.cluster-name {
  font-weight: 600;
  color: var(--body-text, #111827);
}

.resource-bar-container {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 100px;
}

.resource-bar {
  flex: 1;
  min-width: 60px;
}

.resource-percentage {
  font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', monospace;
  font-size: 12px;
  color: var(--body-text, #374151);
  font-weight: 600;
  min-width: 35px;
  text-align: right;
}

.no-resource {
  color: var(--muted, #9ca3af);
}

.cluster-radio {
  cursor: pointer;
}

.selected-clusters-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}

.cluster-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px 4px 12px;
  border-radius: 16px;
  font-size: 13px;
  font-weight: 500;
  background: var(--primary-banner-bg, rgba(59, 130, 246, 0.15));
  color: var(--primary, #2563eb);
  border: 1px solid var(--primary, #2563eb);
}

.cluster-chip.chip-unavailable {
  background: var(--error-banner-bg, rgba(220, 38, 38, 0.15));
  color: var(--error, #dc2626);
  border-color: var(--error, #dc2626);
}

.cluster-row.row-unavailable {
  cursor: not-allowed;
  opacity: 0.55;
}

.cluster-row.row-unavailable:hover {
  background: transparent;
}

.chip-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border: none;
  background: transparent;
  color: inherit;
  cursor: pointer;
  border-radius: 50%;
  font-size: 14px;
  font-weight: bold;
  line-height: 1;
  padding: 0;
  opacity: 0.7;
  transition: opacity 0.15s ease, background-color 0.15s ease;
}

.chip-remove:hover {
  opacity: 1;
  background: rgba(0, 0, 0, 0.1);
}

.chip-remove:disabled,
.chip-remove[disabled] {
  cursor: default;
  opacity: 0.4;
}

.chip-remove:disabled:hover,
.chip-remove[disabled]:hover {
  opacity: 0.4;
  background: transparent;
}

.selected-info {
  padding: 12px 16px;
  border: 1px solid var(--border, #e2e8f0);
  border-radius: 8px;
  background: var(--box-bg, var(--body-bg));
}

.selected-header {
  font-weight: 600;
  color: var(--body-text, #111827);
  margin-bottom: 8px;
}

/* Responsive design */
@media (max-width: 768px) {
  .cluster-table {
    font-size: 12px;
  }
  
  .cluster-table th,
  .cluster-table td {
    padding: 8px 4px;
  }
  
  .cluster-table th.col-nodes,
  .cluster-table th.col-cpu,
  .cluster-table th.col-memory,
  .cluster-table th.col-gpu {
    width: 100px;
  }
  
  .resource-bar-container {
    min-width: 80px;
    gap: 6px;
  }
  
  .resource-bar-track {
    min-width: 40px;
  }
  
  .resource-percentage {
    font-size: 11px;
    min-width: 30px;
  }
}
</style>
