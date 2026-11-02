<template>
  <div class="step-content">
    <h2 class="step-title">{{ t('suseai.wizard.sections.configureApps', 'Configure Applications') }}</h2>
    <p class="text-muted mb-20">{{ t('suseai.wizard.sections.configureAppsDesc', 'Set default Helm values for each application in this blueprint.') }}</p>

    <Banner v-if="stepError" color="error" class="mb-20">
      {{ stepError }}
    </Banner>

    <div v-for="(comp, idx) in components" :key="comp.chartName" class="accordion-panel">
      <div
        class="accordion-header"
        @click="togglePanel(idx)"
      >
        <span class="panel-title">{{ comp.chartName }}</span>
        <span class="panel-meta text-muted">{{ comp.chartVersion }}</span>
        <BadgeState
          v-if="comp.kustomize"
          color="bg-info"
          :label="t('suseai.wizard.labels.patched', 'Patched')"
          :title="t('suseai.wizard.hints.patched', 'This component has a kustomize patch applied.')"
        />
        <BadgeState
          v-if="comp.dependsOn?.length"
          color="bg-info"
          :label="t('suseai.wizard.labels.ordered', 'Ordered')"
          :title="t('suseai.wizard.hints.ordered', 'This component has a deploy-order dependency.')"
        />
        <i :class="['icon', expandedPanels.has(idx) ? 'icon-chevron-up' : 'icon-chevron-down']" />
      </div>

      <div v-if="expandedPanels.has(idx)" class="accordion-body">
        <Tabbed
          default-tab="values"
          :use-hash="false"
          :show-extension-tabs="false"
        >
          <Tab
            name="values"
            :label="t('suseai.wizard.labels.valuesTab', 'Values')"
            :weight="2"
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
          </Tab>

          <Tab
            name="advanced"
            :label="t('suseai.wizard.labels.advancedTab', 'Advanced')"
            :weight="1"
          >
            <h3>{{ t('suseai.wizard.labels.deployOrder', 'Deploy Order') }}</h3>
            <div class="row mb-30">
              <div class="col span-6">
                <LabeledSelect
                  v-if="siblingNames(comp.chartName).length"
                  :value="comp.dependsOn || []"
                  :options="siblingNames(comp.chartName)"
                  :multiple="true"
                  :clearable="true"
                  :close-on-select="false"
                  :label="t('suseai.wizard.labels.dependsOn', 'Deploys after')"
                  :tooltip="t('suseai.wizard.labels.dependsOnTooltip', 'This application is deployed only after the selected applications are Ready.')"
                  :mode="'edit'"
                  @update:value="onDependsOnUpdate(comp.chartName, $event)"
                />
                <p v-else class="text-muted">
                  {{ t('suseai.wizard.labels.dependsOnSingleApp', 'This blueprint has a single application, so there is nothing to order it after.') }}
                </p>
              </div>
            </div>

            <h3>{{ t('suseai.wizard.labels.kustomizeOverlay', 'Kustomize overlay') }}</h3>
            <KustomizeOverlayEditor
              :value="kustomizeMap[comp.chartName]"
              :name="comp.chartName"
              :git-repo-options="gitClusterRepoOptions"
              @update:value="onKustomizeUpdate(comp.chartName, $event)"
            />
          </Tab>
        </Tabbed>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, computed, watch, getCurrentInstance } from 'vue';
import { useT } from '../../../composables/useT';
import yaml from 'js-yaml';
import { Banner } from '@components/Banner';
import { BadgeState } from '@components/BadgeState';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import Tabbed from '@shell/components/Tabbed';
import Tab from '@shell/components/Tabbed/Tab';
import ValuesStep from './ValuesStep.vue';
import KustomizeOverlayEditor from './KustomizeOverlayEditor.vue';
import { fetchChartDefaultValues, listGitClusterRepoNames } from '../../../services/rancher-apps';
import { validateDependsOn } from '../../../validators/blueprint';
import type { BlueprintComponent, ComponentKustomize } from '../../../types/blueprint-types';

interface Props { components: BlueprintComponent[] }
interface Emits {
  (e: 'update:components', v: BlueprintComponent[]): void;
  (e: 'update:valid', v: boolean): void;
}

const props = defineProps<Props>();
const emit  = defineEmits<Emits>();

// Custom git ClusterRepo names: the sources a Git overlay can be fetched from (null while loading).
const gitClusterRepoOptions = ref<string[] | null>(null);

const stepError = computed(() => validateDependsOn(props.components));

// Surfaces validity to the wizard the same way BlueprintCustomizeStep does
// (watch + immediate emit), so Next/Create is gated on a valid dependsOn graph
// instead of only showing the Banner.
watch(stepError, (err) => emit('update:valid', err === null), { immediate: true });

function siblingNames(self: string) {
  return props.components.map(c => c.chartName).filter(n => n !== self);
}
function onDependsOnUpdate(chartName: string, deps: string[]) {
  emit('update:components', props.components.map(c => c.chartName === chartName ? { ...c, dependsOn: deps.length ? deps : undefined } : c));
}

const vm    = getCurrentInstance()!.proxy as any;
const store = vm.$store;

const t = useT();

const expandedPanels    = ref(new Set<number>([0]));
const loadingMap        = ref<Record<string, boolean>>({});
const questionsLoadingMap = ref<Record<string, boolean>>({});
const versionInfoMap    = ref<Record<string, any>>({});
const valuesKeys        = ref<Record<string, number>>({});

const compValues = ref<Record<string, Record<string, any>>>(
  Object.fromEntries(props.components.map(c => [c.chartName, { ...(c.values || {}) }]))
);

// Kustomize overlay per chartName; KustomizeOverlayEditor owns the authoring state.
const kustomizeMap = ref<Record<string, ComponentKustomize | undefined>>(
  Object.fromEntries(props.components.map(c => [c.chartName, c.kustomize]))
);

async function loadGitClusterRepos() {
  try {
    gitClusterRepoOptions.value = await listGitClusterRepoNames(store);
  } catch {
    // Empty: the ClusterRepo select falls back to free-typing (taggable).
    gitClusterRepoOptions.value = [];
  }
}
loadGitClusterRepos();

function onKustomizeUpdate(chartName: string, kustomize: ComponentKustomize | undefined) {
  kustomizeMap.value = { ...kustomizeMap.value, [chartName]: kustomize };
  emitComponents();
}

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
  info: any,
) {
  let parsed: Record<string, any> = {};

  if (info?.values && Object.keys(info.values).length) {
    parsed = JSON.parse(JSON.stringify(info.values));
  } else {
    try {
      const raw = await fetchChartDefaultValues(store, 'local', chartRepo, chartName, chartVersion);
      if (raw?.trim()) {
        parsed = (yaml.load(raw) as Record<string, any>) || {};
      }
    } catch { /* leave empty */ }
  }

  if (Object.keys(parsed).length) {
    compValues.value = { ...compValues.value, [chartName]: parsed };
    valuesKeys.value = { ...valuesKeys.value, [chartName]: (valuesKeys.value[chartName] || 0) + 1 };
    emitComponents();
  }
}

function onValuesUpdate(chartName: string, newValues: Record<string, any>) {
  compValues.value = { ...compValues.value, [chartName]: newValues };
  emitComponents();
}

function emitComponents() {
  emit('update:components', props.components.map(c => ({
    ...c,
    values: Object.keys(compValues.value[c.chartName] || {}).length > 0
      ? compValues.value[c.chartName]
      : undefined,
    kustomize: kustomizeMap.value[c.chartName],
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
.mb-30 { margin-bottom: 30px; }
</style>
