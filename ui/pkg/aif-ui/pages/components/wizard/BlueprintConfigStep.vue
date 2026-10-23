<template>
  <div class="step-content">
    <h2 class="step-title">{{ t('suseai.wizard.sections.configureApps', 'Configure Applications') }}</h2>
    <p class="text-muted mb-20">{{ t('suseai.wizard.sections.configureAppsDesc', 'Set default Helm values for each application in this blueprint.') }}</p>

    <Banner v-if="dependsOnError" color="error" class="mb-20">
      {{ dependsOnError }}
    </Banner>

    <div v-for="(comp, idx) in components" :key="comp.chartName" class="accordion-panel">
      <div
        class="accordion-header"
        @click="togglePanel(idx)"
      >
        <span class="panel-title">{{ comp.chartName }}</span>
        <span class="panel-meta text-muted">{{ comp.chartVersion }}</span>
        <i :class="['icon', expandedPanels.has(idx) ? 'icon-chevron-up' : 'icon-chevron-down']" />
      </div>

      <div v-if="expandedPanels.has(idx)" class="accordion-body">
        <LabeledSelect
          :value="comp.dependsOn || []"
          :options="siblingNames(comp.chartName)"
          :multiple="true"
          :label="t('suseai.wizard.labels.dependsOn', 'Deploys after')"
          :mode="'edit'"
          class="mb-20"
          @update:value="onDependsOnUpdate(comp.chartName, $event)"
        />
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

        <div class="kustomize-section">
          <h4 class="kustomize-title">{{ t('suseai.wizard.labels.kustomizeOverlay', 'Kustomize overlay (optional)') }}</h4>
          <RadioGroup
            :name="`kustomize-source-${comp.chartName}`"
            :options="kustomizeSourceOptions"
            :value="kustomizeSourceMap[comp.chartName] || 'none'"
            :row="true"
            class="mb-20"
            @update:value="onKustomizeSourceUpdate(comp.chartName, $event)"
          />

          <template v-if="kustomizeSourceMap[comp.chartName] === 'inline'">
            <Banner color="info" class="mb-20">
              {{ t('suseai.wizard.labels.kustomizeInlineHint', 'Inline supports a single kustomization.yaml. For overlays that reference patch or resource files, use the Git source instead.') }}
            </Banner>
            <LabeledInput
              :value="kustomizeDirMap[comp.chartName] || ''"
              :label="t('suseai.wizard.labels.kustomizeDir', 'Overlay directory')"
              placeholder="overlay"
              class="mb-20"
              @update:value="onKustomizeDirUpdate(comp.chartName, $event)"
            />
            <YamlEditor
              :value="kustomizeBodyMap[comp.chartName] || ''"
              class="kustomize-editor"
              @update:value="onKustomizeBodyUpdate(comp.chartName, $event)"
            />
          </template>

          <template v-else-if="kustomizeSourceMap[comp.chartName] === 'git'">
            <LabeledSelect
              :value="kustomizeChartRepoMap[comp.chartName] || ''"
              :options="gitClusterRepoOptions"
              :label="t('suseai.wizard.labels.kustomizeChartRepo', 'Git ClusterRepo')"
              :mode="'edit'"
              :taggable="true"
              class="mb-20"
              @update:value="onKustomizeChartRepoUpdate(comp.chartName, $event)"
            />
            <LabeledInput
              :value="kustomizePathMap[comp.chartName] || ''"
              :label="t('suseai.wizard.labels.kustomizePath', 'Path in repo')"
              placeholder="overlays/prod"
              class="mb-20"
              @update:value="onKustomizePathUpdate(comp.chartName, $event)"
            />
            <LabeledInput
              :value="kustomizeDirMap[comp.chartName] || ''"
              :label="t('suseai.wizard.labels.kustomizeDir', 'Overlay directory')"
              placeholder="overlay"
              class="mb-20"
              @update:value="onKustomizeDirUpdate(comp.chartName, $event)"
            />
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, computed, watch, getCurrentInstance } from 'vue';
import { useT } from '../../../composables/useT';
import yaml from 'js-yaml';
import { Banner } from '@components/Banner';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import { LabeledInput } from '@components/Form/LabeledInput';
import { RadioGroup } from '@components/Form/Radio';
import YamlEditor from '@shell/components/YamlEditor';
import ValuesStep from './ValuesStep.vue';
import { fetchChartDefaultValues, listClusterRepos } from '../../../services/rancher-apps';
import { validateDependsOn } from '../../../validators/blueprint';
import { buildInlineKustomize, buildGitRefKustomize } from './kustomize-authoring';
import type { BlueprintComponent } from '../../../types/blueprint-types';

interface Props { components: BlueprintComponent[] }
interface Emits {
  (e: 'update:components', v: BlueprintComponent[]): void;
  (e: 'update:valid', v: boolean): void;
}

const props = defineProps<Props>();
const emit  = defineEmits<Emits>();

const dependsOnError = computed(() => validateDependsOn(props.components));

// Surfaces validity to the wizard the same way BlueprintCustomizeStep does
// (watch + immediate emit), so Next/Create is gated on a valid dependsOn graph
// instead of only showing the Banner.
watch(dependsOnError, (err) => emit('update:valid', err === null), { immediate: true });

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

// Kustomize overlay authoring state, keyed by chartName.
type KustomizeSource = 'none' | 'inline' | 'git';
const kustomizeSourceMap = ref<Record<string, KustomizeSource>>(
  Object.fromEntries(props.components.map(c => [c.chartName, c.kustomize?.gitRef ? 'git' : c.kustomize ? 'inline' : 'none']))
);
const kustomizeDirMap = ref<Record<string, string>>(
  Object.fromEntries(props.components.map(c => [c.chartName, c.kustomize?.dir || '']))
);
const kustomizeBodyMap = ref<Record<string, string>>(
  Object.fromEntries(props.components.map(c => [c.chartName, c.kustomize?.dir ? (c.kustomize.files?.[`${c.kustomize.dir}/kustomization.yaml`] || '') : '']))
);
const kustomizeChartRepoMap = ref<Record<string, string>>(
  Object.fromEntries(props.components.map(c => [c.chartName, c.kustomize?.gitRef?.chartRepo || '']))
);
const kustomizePathMap = ref<Record<string, string>>(
  Object.fromEntries(props.components.map(c => [c.chartName, c.kustomize?.gitRef?.path || '']))
);
const kustomizeSourceOptions = [
  { value: 'none', label: t('suseai.wizard.labels.kustomizeNone', 'None') },
  { value: 'inline', label: t('suseai.wizard.labels.kustomizeInline', 'Inline') },
  { value: 'git', label: t('suseai.wizard.labels.kustomizeGit', 'Git') },
];

// git-backed ClusterRepos available as the overlay source. Reuses the same
// listClusterRepos() call the app-selector step already makes (via
// inferClusterRepoForChart) rather than standing up new store plumbing.
const gitClusterRepoOptions = ref<string[]>([]);
async function loadGitClusterRepos() {
  try {
    const repos = await listClusterRepos(store);
    gitClusterRepoOptions.value = repos
      .filter(r => !!r?.spec?.gitRepo)
      .map(r => r?.metadata?.name)
      .filter((n): n is string => !!n);
  } catch { /* leave empty — the ClusterRepo select falls back to free-typing (taggable) */ }
}
loadGitClusterRepos();

function onKustomizeSourceUpdate(chartName: string, value: KustomizeSource) {
  kustomizeSourceMap.value = { ...kustomizeSourceMap.value, [chartName]: value };
  emitComponents();
}
function onKustomizeDirUpdate(chartName: string, dir: string) {
  kustomizeDirMap.value = { ...kustomizeDirMap.value, [chartName]: dir };
  emitComponents();
}
function onKustomizeBodyUpdate(chartName: string, body: string) {
  kustomizeBodyMap.value = { ...kustomizeBodyMap.value, [chartName]: body };
  emitComponents();
}
function onKustomizeChartRepoUpdate(chartName: string, chartRepo: string) {
  kustomizeChartRepoMap.value = { ...kustomizeChartRepoMap.value, [chartName]: chartRepo };
  emitComponents();
}
function onKustomizePathUpdate(chartName: string, path: string) {
  kustomizePathMap.value = { ...kustomizePathMap.value, [chartName]: path };
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
    kustomize: kustomizeSourceMap.value[c.chartName] === 'inline'
      ? buildInlineKustomize(kustomizeDirMap.value[c.chartName] || '', kustomizeBodyMap.value[c.chartName] || '')
      : kustomizeSourceMap.value[c.chartName] === 'git'
      ? buildGitRefKustomize(
          kustomizeDirMap.value[c.chartName] || '',
          kustomizeChartRepoMap.value[c.chartName] || '',
          kustomizePathMap.value[c.chartName] || '',
        )
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
.kustomize-section { margin-top: 20px; padding-top: 16px; border-top: 1px solid var(--border); }
.kustomize-title { margin: 0 0 12px; font-size: 14px; font-weight: 600; }
.kustomize-editor { min-height: 240px; }
</style>
