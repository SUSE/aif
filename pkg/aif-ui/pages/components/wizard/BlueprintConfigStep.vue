<template>
  <div class="step-content">
    <h2 class="step-title">
      {{ t('suseai.wizard.sections.configureApps', 'Configure Applications') }}
    </h2>
    <p class="text-muted mb-20">
      {{ t('suseai.wizard.sections.configureAppsDesc', 'Set default Helm values for each application in this blueprint.') }}
    </p>

    <div
      v-for="(comp, idx) in components"
      :key="comp.chartName"
      class="accordion-panel"
    >
      <div
        class="accordion-header"
        @click="togglePanel(idx)"
      >
        <span class="panel-title">{{ comp.chartName }}</span>
        <span class="panel-meta text-muted">{{ comp.chartVersion }}</span>
        <i :class="['icon', expandedPanels.has(idx) ? 'icon-chevron-up' : 'icon-chevron-down']" />
      </div>

      <div
        v-if="expandedPanels.has(idx)"
        class="accordion-body"
      >
        <ValuesStep
          :key="`${comp.chartName}-${valuesKeys[comp.chartName] || 0}`"
          :values="compValues[comp.chartName] || {}"
          :chart-repo="comp.chartRepo"
          :chart-name="comp.chartName"
          :chart-version="comp.chartVersion"
          :loading-values="!!loadingMap[comp.chartName]"
          :version-dirty="false"
          :has-questions="!!(versionInfoMap[comp.chartName]?.questions)"
          :questions-source="versionInfoMap[comp.chartName] || null"
          :questions-loading="!!questionsLoadingMap[comp.chartName]"
          :ignore-variables="[]"
          :target-namespace="''"
          mode="install"
          :in-store="'cluster'"
          @update:values="onValuesUpdate(comp.chartName, $event)"
          @load-defaults="reloadDefaults(comp.chartName, comp.chartRepo, comp.chartVersion)"
          @values-edited="() => onValuesUpdate(comp.chartName, compValues[comp.chartName] || {})"
        />
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, getCurrentInstance } from 'vue';
import { useT } from '../../../composables/useT';
import yaml from 'js-yaml';
import ValuesStep from './ValuesStep.vue';
import { fetchChartDefaultValues } from '../../../services/rancher-apps';
import type { BlueprintComponent } from '../../../types/blueprint-types';

interface Props { components: BlueprintComponent[] }
interface Emits { (e: 'update:components', v: BlueprintComponent[]): void }

const props = defineProps<Props>();
const emit  = defineEmits<Emits>();

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const vm    = getCurrentInstance()?.proxy as any;
const store = vm?.$store;

const t = useT();

const expandedPanels    = ref(new Set<number>([0]));
const loadingMap        = ref<Record<string, boolean>>({});
const questionsLoadingMap = ref<Record<string, boolean>>({});
const versionInfoMap    = ref<Record<string, unknown>>({});
const valuesKeys        = ref<Record<string, number>>({});

const compValues = ref<Record<string, Record<string, unknown>>>(
  Object.fromEntries(props.components.map(c => [c.chartName, { ...(c.values || {}) }]))
);

// Load info for the first panel on mount.
if (props.components.length > 0) {
  const first = props.components[0];
  loadChartInfo(first.chartName, first.chartRepo, first.chartVersion);
}

function togglePanel(idx: number) {
  const next = new Set(expandedPanels.value);
  if (next.has(idx)) {
    next.delete(idx);
  } else {
    next.add(idx);
    const comp = props.components[idx];
    if (comp && !versionInfoMap.value[comp.chartName]) {
      loadChartInfo(comp.chartName, comp.chartRepo, comp.chartVersion);
    }
  }
  expandedPanels.value = next;
}

async function loadChartInfo(chartName: string, chartRepo: string, chartVersion: string) {
  if (loadingMap.value[chartName]) return;

  loadingMap.value        = { ...loadingMap.value,        [chartName]: true };
  questionsLoadingMap.value = { ...questionsLoadingMap.value, [chartName]: true };

  try {
    await store.dispatch('catalog/load');
    const info = await store.dispatch('catalog/getVersionInfo', {
      repoType:    'cluster',
      repoName:    chartRepo,
      chartName,
      versionName: chartVersion,
    });
    versionInfoMap.value = { ...versionInfoMap.value, [chartName]: info };
    questionsLoadingMap.value = { ...questionsLoadingMap.value, [chartName]: false };

    // Populate initial values only if not already set.
    if (!Object.keys(compValues.value[chartName] || {}).length) {
      await applyDefaults(chartName, chartRepo, chartVersion, info);
    }
  } catch {
    questionsLoadingMap.value = { ...questionsLoadingMap.value, [chartName]: false };
  } finally {
    loadingMap.value = { ...loadingMap.value, [chartName]: false };
  }
}

// Called by the "Load defaults" button — always reloads values.
async function reloadDefaults(chartName: string, chartRepo: string, chartVersion: string) {
  if (loadingMap.value[chartName]) return;
  loadingMap.value = { ...loadingMap.value, [chartName]: true };
  try {
    const info = versionInfoMap.value[chartName];
    await applyDefaults(chartName, chartRepo, chartVersion, info);
  } finally {
    loadingMap.value = { ...loadingMap.value, [chartName]: false };
  }
}

async function applyDefaults(
  chartName: string,
  chartRepo: string,
  chartVersion: string,
  info: unknown,
) {
  let parsed: Record<string, unknown> = {};

  const infoRec = info as Record<string, unknown> | null | undefined;
  if (infoRec?.values && Object.keys(infoRec.values as object).length) {
    parsed = JSON.parse(JSON.stringify(infoRec.values));
  } else {
    try {
      const raw = await fetchChartDefaultValues(store, 'local', chartRepo, chartName, chartVersion);
      if (raw?.trim()) {
        parsed = (yaml.load(raw) as Record<string, unknown>) || {};
      }
    } catch { /* leave empty */ }
  }

  if (Object.keys(parsed).length) {
    compValues.value = { ...compValues.value, [chartName]: parsed };
    valuesKeys.value = { ...valuesKeys.value, [chartName]: (valuesKeys.value[chartName] || 0) + 1 };
    emitComponents();
  }
}

function onValuesUpdate(chartName: string, newValues: Record<string, unknown>) {
  compValues.value = { ...compValues.value, [chartName]: newValues };
  emitComponents();
}

function emitComponents() {
  emit('update:components', props.components.map(c => ({
    ...c,
    values: Object.keys(compValues.value[c.chartName] || {}).length > 0
      ? compValues.value[c.chartName]
      : undefined,
  })));
}
</script>

<style lang="scss" scoped>
.step-content { width: 100%; }
.step-title { margin: 0 0 8px; font-size: 18px; font-weight: 600; }
.mb-20 { margin-bottom: 20px; }
.text-muted { color: var(--muted); font-size: 14px; }
.accordion-panel {
  border: 1px solid var(--border); border-radius: 8px; margin-bottom: 12px; overflow: hidden;
}
.accordion-header {
  display: flex; align-items: center; gap: 12px;
  padding: 14px 16px; cursor: pointer; background: var(--sortable-table-header-bg);
  &:hover { background: var(--hover-bg); }
}
.panel-title { font-weight: 600; font-size: 14px; flex: 1; }
.panel-meta  { font-size: 12px; color: var(--muted); }
.accordion-body { padding: 16px; }
</style>
