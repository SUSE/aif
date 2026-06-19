<template>
  <div class="step-content">
    <h2 class="step-title">{{ isEdit ? t('suseai.wizard.sections.reviewSaveVersion', 'Review & Save as New Version') : t('suseai.wizard.sections.reviewCreate', 'Review & Create') }}</h2>

    <div class="review-section">
      <div class="review-row">
        <span class="review-label">{{ t('suseai.wizard.form.blueprintName', 'Name') }}</span>
        <span class="review-value">{{ form.displayName }}</span>
      </div>
      <div class="review-row">
        <span class="review-label">{{ t('suseai.wizard.form.version', 'Version') }}</span>
        <span class="review-value">{{ form.version }}</span>
      </div>
      <div v-if="form.description" class="review-row">
        <span class="review-label">{{ t('suseai.wizard.form.description', 'Description') }}</span>
        <span class="review-value">{{ form.description }}</span>
      </div>
    </div>

    <div class="review-section">
      <h3 class="section-title">{{ t('suseai.wizard.labels.applications', 'Applications') }} ({{ form.components.length }})</h3>
      <div v-for="comp in form.components" :key="comp.chartName" class="component-row">
        <span class="comp-name">{{ comp.chartName }}</span>
        <span class="comp-version text-muted">{{ comp.chartVersion }}</span>
        <span class="comp-repo text-muted">{{ comp.chartRepo }}</span>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import type { BlueprintSpec } from '../../../types/blueprint-types';
import { useT } from '../../../composables/useT';

interface Props { form: BlueprintSpec; isEdit?: boolean }
defineProps<Props>();

const t = useT();
</script>

<style lang="scss" scoped>
.step-content { max-width: 600px; }
.step-title { margin: 0 0 24px; font-size: 18px; font-weight: 600; }
.review-section { margin-bottom: 24px; }
.section-title { font-size: 15px; font-weight: 600; margin: 0 0 12px; }
.review-row {
  display: flex; gap: 16px; padding: 8px 0;
  border-bottom: 1px solid var(--border);
  &:last-child { border-bottom: none; }
}
.review-label { font-weight: 500; min-width: 100px; color: var(--muted); }
.review-value  { color: var(--body-text); }
.component-row {
  display: flex; align-items: center; gap: 16px;
  padding: 8px 0; border-bottom: 1px solid var(--border);
  &:last-child { border-bottom: none; }
  .comp-name    { font-weight: 500; min-width: 140px; }
  .comp-version { min-width: 80px; }
  .comp-repo    { font-size: 12px; }
}
.text-muted { color: var(--muted); font-size: 13px; }
</style>
