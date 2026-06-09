<template>
  <div class="review-step">
    <!-- Application Details -->
    <h3>Application Details</h3>
    <div class="details-grid">
      <div class="detail-item">
        <label>Release Name:</label>
        <span>{{ release }}</span>
      </div>
      <div class="detail-item">
        <label>Namespace:</label>
        <span>{{ namespace }}</span>
      </div>
      <div class="detail-item">
        <label>Repository:</label>
        <span>{{ chartRepo }}</span>
      </div>
      <div class="detail-item">
        <label>Chart:</label>
        <span>{{ chartName }}</span>
      </div>
      <div class="detail-item">
        <label>Version:</label>
        <span>{{ chartVersion }}</span>
      </div>
      <div class="detail-item full-width clusters-row">
        <label>
          Target Cluster{{ clusters.length > 1 ? 's' : '' }}:
          <span v-if="clusters.length > 1" class="cluster-count">({{ clusters.length }})</span>
        </label>
        <div v-if="clusters.length === 0" class="no-clusters">— none —</div>
        <div v-else-if="clusters.length === 1" class="single-cluster">{{ clusters[0] }}</div>
        <div v-else class="cluster-chips">
          <span
            v-for="clusterId in clusters"
            :key="clusterId"
            class="cluster-chip"
          >{{ clusterId }}</span>
        </div>
      </div>
    </div>

    <!-- Configuration -->
    <h3 class="mt-30">Configuration</h3>
    <YamlEditor
      v-model:value="localValues"
      :as-object="true"
      class="values-editor"
      @update:value="$emit('values-edited')"
    />
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue';
import YamlEditor from '@shell/components/YamlEditor';

interface Props {
  mode: 'install' | 'manage';
  release: string;
  namespace: string;
  chartRepo: string;
  chartName: string;
  chartVersion: string;
  clusters: string[]; // Array-based selection for both modes
  values: Record<string, any>;
}

interface Emits {
  (e: 'update:values', values: Record<string, any>): void;
  (e: 'values-edited'): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const localValues = computed({
  get: () => props.values,
  set: (value: Record<string, any>) => emit('update:values', value)
});
</script>

<style scoped>
.review-step {
  width: 100%;
  max-width: 100%;
  box-sizing: border-box;
  overflow: hidden;
}

.review-step h3 {
  margin: 0 0 16px 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--body-text);
}

.mt-30 {
  margin-top: 30px;
}

.details-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px 20px;
  margin-bottom: 20px;
  width: 100%;
  max-width: 100%;
  box-sizing: border-box;
}

.detail-item {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0; /* Allow flexbox to shrink */
  overflow: hidden;
}

.detail-item.full-width {
  grid-column: 1 / -1;
}

.detail-item label {
  font-weight: 500;
  color: var(--body-text);
  min-width: 80px;
  flex-shrink: 0;
  white-space: nowrap;
}

.detail-item span {
  color: var(--muted);
  font-family: monospace;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 768px) {
  .details-grid {
    grid-template-columns: 1fr;
    gap: 8px;
  }
}

.values-editor {
  min-height: 300px;
}

/* Cluster display styles */
.clusters-row {
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}

.clusters-row label {
  display: flex;
  align-items: center;
  gap: 6px;
}

.cluster-count {
  font-weight: 400;
  color: var(--muted, #6b7280);
  font-size: 12px;
}

.no-clusters {
  color: var(--muted, #9ca3af);
  font-style: italic;
}

.single-cluster {
  font-family: monospace;
  color: var(--muted, #6b7280);
}

.cluster-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.cluster-chip {
  display: inline-flex;
  align-items: center;
  padding: 4px 12px;
  border-radius: 16px;
  font-size: 13px;
  font-weight: 500;
  background: var(--primary-banner-bg, rgba(59, 130, 246, 0.15));
  color: var(--primary, #2563eb);
  border: 1px solid var(--primary, #2563eb);
  font-family: monospace;
}
</style>