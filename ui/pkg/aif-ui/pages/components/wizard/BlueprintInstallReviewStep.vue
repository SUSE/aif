<template>
  <div class="step-content">
    <h2 class="step-title">{{ t('suseai.wizard.sections.reviewInstall', {}, true) }}</h2>

    <div class="review-section">
      <div class="review-row"><span class="label">{{ t('suseai.wizard.form.workloadName', 'Instance Name') }}</span><span>{{ workloadName }}</span></div>
      <div class="review-row"><span class="label">{{ t('suseai.wizard.form.installNamespace', 'Default Namespace') }}</span><span>{{ namespace }}</span></div>
      <div class="review-row"><span class="label">{{ t('suseai.wizard.labels.blueprint', 'Blueprint') }}</span><span>{{ displayName }} v{{ version }}</span></div>
      <div class="review-row"><span class="label">{{ t('suseai.wizard.form.deploymentType', 'Deployment Type') }}</span><span>{{ deployType }}</span></div>
      <div class="review-row">
        <span class="label">{{ t('suseai.wizard.labels.clusters', 'Clusters') }}</span>
        <span>{{ clusters.join(', ') || '—' }}</span>
      </div>
    </div>

    <div class="review-section">
      <h3 class="section-title">{{ t('suseai.wizard.labels.components', 'Components') }} ({{ componentCount }})</h3>
      <div v-for="comp in components" :key="comp.chartName" class="component-row" :class="{ 'is-excluded': excludedNames.has(comp.chartName) }">
        <span>{{ comp.chartName }}</span>
        <span class="text-muted">{{ comp.chartVersion }}</span>
        <span v-if="excludedNames.has(comp.chartName)" class="badge-excluded">
          {{ t('suseai.wizard.labels.excluded', 'Excluded') }}
        </span>
        <template v-else>
          <span v-if="comp.releaseName" class="comp-release text-muted">
            {{ t('suseai.wizard.labels.releaseName', 'Release') }}: {{ comp.releaseName }}
          </span>
          <span class="comp-target text-muted">
            → {{ comp.targetNamespace || namespace }}
            <template v-if="comp.targetNamespace">({{ t('suseai.wizard.labels.fixedNamespace', 'fixed') }})</template>
          </span>
        </template>
      </div>
    </div>

    <div v-if="customizedValues.length" class="review-section">
      <h3 class="section-title">{{ t('suseai.wizard.labels.customized', 'Customized') }} ({{ customizedValues.length }})</h3>
      <div v-for="ov in customizedValues" :key="ov.componentName" class="component-row">
        <span>{{ ov.componentName }}</span>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue';
import type { BlueprintComponent } from '../../../types/blueprint-types';
import type { ComponentValueOverride } from '../../../types/aiworkload-types';
import { useT } from '../../../composables/useT';

interface Props {
  workloadName:      string;
  namespace:         string;
  displayName:       string;
  version:           string;
  componentCount:    number;
  deployType:        string;
  clusters:          string[];
  components:        BlueprintComponent[];
  componentValues?:  ComponentValueOverride[];
}
const props = defineProps<Props>();

const t = useT();

const excludedNames = computed(() => new Set(
  (props.componentValues || []).filter((ov) => ov.enabled === false).map((ov) => ov.componentName),
));
// customizedValues excludes entries with no values (only toggled enabled) and entries for an
// excluded component — both already show up as the "Excluded" badge above, and re-listing an
// excluded component here as "Customized" is misleading: it won't deploy, edited values or not.
const customizedValues = computed(() => (props.componentValues || []).filter((ov) => ov.values !== undefined && ov.enabled !== false));
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
  .comp-target { margin-left: auto; }
  &.is-excluded { opacity: 0.6; }
}
.badge-excluded {
  margin-left: auto; font-size: 11px; font-weight: 600; color: var(--muted);
  border: 1px solid var(--border); border-radius: 10px; padding: 2px 8px;
}
.text-muted { color: var(--muted); font-size: 13px; }
</style>
