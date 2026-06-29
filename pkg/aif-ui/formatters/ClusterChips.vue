<template>
  <div class="cluster-chips" v-if="clusters.length">
    <small v-if="showLabel" class="status-label">{{ label }}:</small>
    <span 
      v-for="cluster in clusters" 
      :key="cluster"
      :class="{ 
        'cluster-chip': true,
        'clickable': clickable,
        'readonly': !clickable
      }"
      :title="getClusterTooltip(cluster)"
      @click="handleClusterClick(cluster)"
    >
      {{ getClusterDisplayName(cluster) }}
    </span>
  </div>
</template>

<script lang="ts">
import { defineComponent, getCurrentInstance } from 'vue';
import type { PropType } from 'vue';
import type { ClusterInfo } from '../types/rancher-types';

export default defineComponent({
  name: 'ClusterChips',
  
  props: {
    clusters: {
      type: Array as PropType<string[]>,
      required: true
    },
    
    clusterInfo: {
      type: Array as PropType<ClusterInfo[]>,
      default: () => []
    },
    
    clickable: {
      type: Boolean,
      default: true
    },
    
    showLabel: {
      type: Boolean,
      default: true
    },
    
    label: {
      type: String,
      default: 'Installed in'
    },
    
    tooltip: {
      type: String,
      default: ''
    },
    
    navigationPath: {
      type: String,
      default: '/c/{clusterId}/explorer#cluster-events'
    }
  },
  
  emits: [
    'cluster-click'
  ],
  
  setup(props, { emit }) {
    const vm = getCurrentInstance();
    const $router = (vm as any)?.proxy?.$router;
    
    const getClusterDisplayName = (clusterId: string): string => {
      const cluster = props.clusterInfo.find(c => c.id === clusterId);
      return cluster?.name || clusterId;
    };
    
    const getClusterTooltip = (clusterId: string): string => {
      if (props.tooltip) {
        return props.tooltip.replace('{clusterId}', clusterId);
      }
      
      if (props.clickable) {
        return `Click to view ${getClusterDisplayName(clusterId)} cluster dashboard`;
      }
      
      return `Installed in ${getClusterDisplayName(clusterId)}`;
    };
    
    const handleClusterClick = (clusterId: string) => {
      if (!props.clickable) return;
      
      emit('cluster-click', clusterId);
      
      // Default navigation behavior if no custom handler
      if ($router && props.navigationPath) {
        try {
          const targetUrl = props.navigationPath.replace('{clusterId}', clusterId);
          console.log('[SUSE-AI] Navigating to cluster:', { clusterId, targetUrl });
          $router.push(targetUrl);
        } catch (err) {
          console.error('[SUSE-AI] Failed to navigate to cluster:', err);
        }
      }
    };
    
    return {
      getClusterDisplayName,
      getClusterTooltip,
      handleClusterClick
    };
  }
});
</script>

<style scoped>
.cluster-chips {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.status-label {
  color: var(--muted);
  font-size: 12px;
  font-weight: 500;
  margin-right: 4px;
}

.cluster-chip {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  background: var(--accent-btn);
  border: 1px solid var(--accent-btn);
  border-radius: 12px;
  color: var(--accent-btn-text);
  font-size: 11px;
  font-weight: 500;
  text-decoration: none;
  transition: all 0.2s ease;
  white-space: nowrap;
}

.cluster-chip.clickable {
  cursor: pointer;
}

.cluster-chip.clickable:hover {
  background: var(--primary-light);
  border-color: var(--primary);
  color: var(--primary);
  transform: translateY(-1px);
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
}

.cluster-chip.clickable:active {
  transform: translateY(0);
}

.cluster-chip.readonly {
  cursor: default;
  opacity: 0.8;
}

.cluster-chip.readonly:hover {
  background: var(--accent-btn);
  border-color: var(--accent-btn);
  color: var(--accent-btn-text);
  transform: none;
  box-shadow: none;
}
</style>