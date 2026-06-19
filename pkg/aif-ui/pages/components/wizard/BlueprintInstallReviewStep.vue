<template>
  <div class="step-content">
    <h2 class="step-title">{{ t('suseai.wizard.sections.reviewInstall', 'Review & Install') }}</h2>

    <div class="review-section">
      <div class="review-row"><span class="label">{{ t('suseai.wizard.form.workloadName', 'Instance Name') }}</span><span>{{ workloadName }}</span></div>
      <div class="review-row"><span class="label">{{ t('suseai.wizard.form.namespace', 'Namespace') }}</span><span>{{ namespace }}</span></div>
      <div class="review-row"><span class="label">{{ t('suseai.wizard.labels.blueprint', 'Blueprint') }}</span><span>{{ displayName }} v{{ version }}</span></div>
      <div class="review-row"><span class="label">{{ t('suseai.wizard.form.deploymentType', 'Deployment Type') }}</span><span>{{ deployType }}</span></div>
      <div class="review-row">
        <span class="label">{{ t('suseai.wizard.labels.clusters', 'Clusters') }}</span>
        <span>{{ clusters.join(', ') || '—' }}</span>
      </div>
    </div>

    <div class="review-section">
      <h3 class="section-title">{{ t('suseai.wizard.labels.components', 'Components') }} ({{ componentCount }})</h3>
      <div v-for="comp in components" :key="comp.chartName" class="component-row">
        <span>{{ comp.chartName }}</span>
        <span class="text-muted">{{ comp.chartVersion }}</span>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import type { BlueprintComponent } from '../../../types/blueprint-types';
import { useT } from '../../../composables/useT';

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

const t = useT();
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
