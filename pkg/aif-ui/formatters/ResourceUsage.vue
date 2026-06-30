<template>
  <div class="resource-usage">
    <div v-for="resource in displayResources" :key="resource.name" class="resource-item">
      <div class="resource-header">
        <span class="resource-name">{{ resource.name }}</span>
        <span class="resource-values">
          {{ formatValue(resource.used, resource.unit) }} /
          {{ formatValue(resource.total, resource.unit) }}
        </span>
      </div>
      <div class="resource-bar">
        <div
          class="resource-fill"
          :style="{ width: `${resource.percentage}%` }"
          :class="{
            'usage-normal': resource.percentage < 70,
            'usage-warning': resource.percentage >= 70 && resource.percentage < 90,
            'usage-critical': resource.percentage >= 90
          }"
        ></div>
      </div>
      <div v-if="showPercentage" class="resource-percentage">
        {{ Math.round(resource.percentage) }}%
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue';
import type { PropType } from 'vue';

interface ResourceUsageItem {
  name: string;
  used: number;
  total: number;
  unit?: string;
}

export default defineComponent({
  name: 'ResourceUsage',
  props: {
    resources: {
      type: Array as PropType<ResourceUsageItem[]>,
      default: () => []
    },
    showPercentage: {
      type: Boolean,
      default: true
    },
    compactMode: {
      type: Boolean,
      default: false
    }
  },
  setup(props) {
    const displayResources = computed(() => {
      return props.resources.map(resource => ({
        ...resource,
        percentage: resource.total > 0 ? (resource.used / resource.total) * 100 : 0,
        unit: resource.unit || ''
      }));
    });

    const formatValue = (value: number, unit: string) => {
      if (unit === 'bytes') {
        return formatBytes(value);
      }
      if (unit === 'cores') {
        return `${value.toFixed(2)} cores`;
      }
      return `${value}${unit ? ` ${unit}` : ''}`;
    };

    const formatBytes = (bytes: number) => {
      if (bytes === 0) return '0 B';
      const k = 1024;
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.floor(Math.log(bytes) / Math.log(k));
      return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    };

    return {
      displayResources,
      formatValue
    };
  }
});
</script>

<style scoped>
.resource-usage {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.resource-item {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.resource-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 0.875rem;
}

.resource-name {
  font-weight: 500;
  color: var(--text-primary);
}

.resource-values {
  color: var(--text-muted);
  font-family: monospace;
  font-size: 0.75rem;
}

.resource-bar {
  height: 6px;
  background-color: var(--border);
  border-radius: 3px;
  overflow: hidden;
}

.resource-fill {
  height: 100%;
  transition: width 0.3s ease;
  border-radius: 3px;
}

.usage-normal {
  background-color: var(--success);
}

.usage-warning {
  background-color: var(--warning);
}

.usage-critical {
  background-color: var(--error);
}

.resource-percentage {
  font-size: 0.75rem;
  color: var(--text-muted);
  text-align: right;
  font-weight: 500;
}

/* Compact mode */
.resource-usage.compact-mode {
  gap: 0.5rem;
}

.resource-usage.compact-mode .resource-header {
  font-size: 0.75rem;
}

.resource-usage.compact-mode .resource-bar {
  height: 4px;
}

.resource-usage.compact-mode .resource-values {
  font-size: 0.625rem;
}
</style>