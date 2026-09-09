<template>
  <div class="step-content">
    <h2 class="step-title">{{ t('suseai.wizard.sections.customize', 'Customize') }}</h2>
    <p class="text-muted mb-20">
      {{ t('suseai.wizard.sections.customizeDesc', "Override this blueprint's default Helm values for this deployment. Leave a component untouched to keep the blueprint's defaults.") }}
    </p>

    <div v-for="(comp, idx) in components" :key="comp.chartName" class="accordion-panel">
      <div class="accordion-header" @click="togglePanel(idx)">
        <span class="panel-title">{{ comp.chartName }}</span>
        <span class="panel-meta text-muted">{{ comp.chartVersion }}</span>
        <span v-if="touchedNames.has(comp.chartName)" class="badge-customized">
          {{ t('suseai.wizard.labels.customized', 'Customized') }}
        </span>
        <i :class="['icon', expandedPanels.has(idx) ? 'icon-chevron-up' : 'icon-chevron-down']" />
      </div>

      <div v-if="expandedPanels.has(idx)" class="accordion-body">
        <ValuesStep
          :values="editedValues[comp.chartName] || {}"
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
          :mode="mode"
          :in-store="'cluster'"
          @update:values="onValuesUpdate(comp.chartName, $event)"
          @values-edited="() => onValuesUpdate(comp.chartName, editedValues[comp.chartName] || {})"
        />
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, computed, getCurrentInstance } from 'vue';
import { useT } from '../../../composables/useT';
import ValuesStep from './ValuesStep.vue';
import { seedComponentValues, diffComponentValues } from '../../../utils/blueprint-customize';
import type { BlueprintComponent } from '../../../types/blueprint-types';
import type { ComponentValueOverride } from '../../../types/aiworkload-types';

interface Props {
  components:      BlueprintComponent[];
  existingValues?: ComponentValueOverride[];
  // Forwarded to the nested ValuesStep, which normalizes 'install' → Rancher
  // Questions' 'create' mode and anything else → 'edit'. Defaults to
  // 'install' so Task 6 (install-mode wiring) needs no changes here; Task 8
  // passes 'manage' explicitly when this step is used from the manage flow,
  // so the Questions/YAML editor gets correct edit-vs-create semantics
  // instead of always behaving as if this were a fresh install.
  mode?: 'install' | 'manage';
}
interface Emits {
  (e: 'update:modelValue', v: ComponentValueOverride[]): void;
}

const props = withDefaults(defineProps<Props>(), { mode: 'install' });
const emit  = defineEmits<Emits>();
const vm    = getCurrentInstance()!.proxy as any;
const store = vm.$store;

const t = useT();

const expandedPanels      = ref(new Set<number>([0]));
const loadingMap          = ref<Record<string, boolean>>({});
const questionsLoadingMap = ref<Record<string, boolean>>({});
const versionInfoMap      = ref<Record<string, any>>({});

// seed is the read-once starting point (blueprint defaults, deep-merged with
// any existing override) — never mutated after setup. editedValues is the
// live form state, initialized from seed and updated as the user edits.
const seed = seedComponentValues(props.components, props.existingValues || []);
const editedValues = ref<Record<string, Record<string, any>>>(
  Object.fromEntries(Object.entries(seed).map(([k, v]) => [k, JSON.parse(JSON.stringify(v))])),
);

const touchedNames = computed(() => new Set(diffComponentValues(seed, editedValues.value).map((o) => o.componentName)));

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
  loadingMap.value          = { ...loadingMap.value, [chartName]: true };
  questionsLoadingMap.value = { ...questionsLoadingMap.value, [chartName]: true };
  try {
    await store.dispatch('catalog/load');
    const info = await store.dispatch('catalog/getVersionInfo', {
      repoType: 'cluster', repoName: chartRepo, chartName, versionName: chartVersion,
    });
    versionInfoMap.value = { ...versionInfoMap.value, [chartName]: info };
  } catch {
    // Form view falls back to YAML when questions can't be loaded.
  } finally {
    loadingMap.value          = { ...loadingMap.value, [chartName]: false };
    questionsLoadingMap.value = { ...questionsLoadingMap.value, [chartName]: false };
  }
}

function onValuesUpdate(chartName: string, newValues: Record<string, any>) {
  editedValues.value = { ...editedValues.value, [chartName]: newValues };
  emit('update:modelValue', diffComponentValues(seed, editedValues.value));
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
.badge-customized {
  font-size: 11px; font-weight: 600; color: var(--primary);
  border: 1px solid var(--primary); border-radius: 10px; padding: 2px 8px;
}
.accordion-body { padding: 16px; }
</style>
