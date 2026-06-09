<template>
  <div class="step-content">
    <h2 class="step-title">Review &amp; Install</h2>

    <div class="review-section">
      <div class="review-row"><span class="label">Workload Name</span><span>{{ workloadName }}</span></div>
      <div class="review-row"><span class="label">Namespace</span><span>{{ namespace }}</span></div>
      <div class="review-row"><span class="label">Blueprint</span><span>{{ displayName }} v{{ version }}</span></div>
      <div class="review-row"><span class="label">Deploy Type</span><span>{{ deployType }}</span></div>
      <div class="review-row">
        <span class="label">Clusters</span>
        <span>{{ clusters.join(', ') || '—' }}</span>
      </div>
    </div>

    <div class="review-section">
      <h3 class="section-title">Components ({{ componentCount }})</h3>
      <div v-for="comp in components" :key="comp.chartName" class="component-row">
        <span>{{ comp.chartName }}</span>
        <span class="text-muted">{{ comp.chartVersion }}</span>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import type { BlueprintComponent } from '../../../types/blueprint-types';

interface Props {
  workloadName:   string;
  namespace:      string;
  displayName:    string;
  version:        string;
  componentCount: number;
  deployType:     string;
  clusters:       string[];
  components:     BlueprintComponent[];
}
defineProps<Props>();
</script>

<style lang="scss" scoped>
.step-content { max-width: 600px; }
.step-title { margin: 0 0 24px; font-size: 18px; font-weight: 600; }
.review-section { margin-bottom: 24px; }
.section-title { font-size: 15px; font-weight: 600; margin: 0 0 12px; }
.review-row {
  display: flex; gap: 16px; padding: 8px 0; border-bottom: 1px solid var(--border);
  &:last-child { border-bottom: none; }
  .label { font-weight: 500; min-width: 120px; color: var(--muted); }
}
.component-row {
  display: flex; gap: 16px; padding: 6px 0; border-bottom: 1px solid var(--border);
  &:last-child { border-bottom: none; }
}
.text-muted { color: var(--muted); font-size: 13px; }
</style>
