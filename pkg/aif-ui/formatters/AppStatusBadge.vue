<template>
  <span 
    :class="`badge-state ${statusClass}`"
    :title="statusTooltip"
  >
    <i v-if="isLoading" class="icon icon-spinner icon-spin"></i>
    <span>{{ statusText }}</span>
  </span>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue';
import type { PropType } from 'vue';
import type { AppCollectionItem } from '../services/app-collection';

type InstallInfo = {
  installed: boolean;
  clusters: string[];
  release?: string;
  namespace?: string;
};

export default defineComponent({
  name: 'AppStatusBadge',
  
  props: {
    app: {
      type: Object as PropType<AppCollectionItem>,
      required: true
    },
    
    installInfo: {
      type: Object as PropType<InstallInfo>,
      default: () => ({ installed: false, clusters: [] })
    },
    
    isDeleting: {
      type: Boolean,
      default: false
    }
  },
  
  setup(props) {
    const statusClass = computed(() => {
      if (props.isDeleting) return 'bg-warning';
      if (!props.installInfo.installed) return 'bg-info';
      return 'bg-success';
    });
    
    const statusText = computed(() => {
      if (props.isDeleting) return 'Deleting...';
      if (!props.installInfo.installed) return 'Available';
      return 'Installed';
    });
    
    const statusTooltip = computed(() => {
      if (props.isDeleting) {
        return `Deleting ${props.app.name}...`;
      }
      
      if (props.installInfo.installed) {
        const clusterText = props.installInfo.clusters.length === 1 
          ? `1 cluster` 
          : `${props.installInfo.clusters.length} clusters`;
        
        let tooltip = `Installed in ${clusterText}`;
        
        if (props.installInfo.namespace && props.installInfo.release) {
          tooltip += `\nNamespace: ${props.installInfo.namespace}\nRelease: ${props.installInfo.release}`;
        }
        
        return tooltip;
      }
      
      return `${props.app.name} is available for installation`;
    });
    
    const isLoading = computed(() => props.isDeleting);
    
    return {
      statusClass,
      statusText,
      statusTooltip,
      isLoading
    };
  }
});
</script>

<style scoped>
.badge-state {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  white-space: nowrap;
}

.bg-info {
  background: var(--info);
  color: var(--info-text);
}

.bg-success {
  background: var(--success);
  color: var(--success-text);
}

.bg-warning {
  background: var(--warning);
  color: var(--warning-text);
}

.icon-spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>