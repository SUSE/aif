<template>
  <div class="step-content">
    <p class="text-muted mb-20">
      {{ t('suseai.wizard.sections.customizeDesc', "Override this blueprint's default Helm values for this deployment, or exclude a component entirely. Use the Advanced tab to change the deploy order or add a Kustomize overlay. Leave a component untouched to keep the blueprint's defaults.") }}
    </p>

    <Banner v-if="allDisabled" color="error" class="mb-20">
      {{ t('suseai.wizard.errors.allComponentsDisabled', 'At least one component must remain enabled.') }}
    </Banner>
    <Banner v-if="advancedError" color="error" class="mb-20">
      {{ advancedError }}
    </Banner>
    <Banner v-if="mode === 'manage' && Object.keys(locked).length > 0" color="info" class="mb-20">
      {{ t('suseai.wizard.hints.lockedValues', 'These values were set at install and are locked. Leave them unchanged in the values below; saving a different value is rejected.') }}
      <ul class="mt-10">
        <li v-for="(pins, compName) in locked" :key="compName">
          <strong>{{ compName }}</strong>: {{ pins.map((p) => `${p.path} = ${p.value}`).join(', ') }}
        </li>
      </ul>
    </Banner>

    <div v-for="(comp, idx) in components" :key="comp.chartName" class="accordion-panel" :class="{ 'is-disabled': !editedEnabled[comp.chartName] }">
      <div class="accordion-header" @click="togglePanel(idx)">
        <Checkbox
          :value="editedEnabled[comp.chartName]"
          :label="t('suseai.wizard.labels.includeComponent', 'Include')"
          class="panel-checkbox"
          @click.stop
          @update:value="onEnabledUpdate(comp.chartName, $event)"
        />
        <span class="panel-title">{{ comp.chartName }}</span>
        <span class="panel-meta text-muted">{{ comp.chartVersion }}</span>
        <BadgeState
          v-if="customizedNames.has(comp.chartName)"
          color="bg-info"
          :label="t('suseai.wizard.labels.customized', 'Customized')"
          :title="t('suseai.wizard.hints.customized', 'This component differs from the blueprint for this deployment.')"
        />
        <BadgeState
          v-if="comp.kustomize || editedKustomize[comp.chartName]"
          color="bg-info"
          :label="t('suseai.wizard.labels.patched', 'Patched')"
          :title="t('suseai.wizard.hints.patched', 'This component has a kustomize patch applied.')"
        />
        <BadgeState
          v-if="editedDependsOn[comp.chartName]?.length"
          color="bg-info"
          :label="t('suseai.wizard.labels.ordered', 'Ordered')"
          :title="t('suseai.wizard.hints.ordered', 'This component has a deploy-order dependency.')"
        />
        <i :class="['icon', expandedPanels.has(idx) ? 'icon-chevron-up' : 'icon-chevron-down']" />
      </div>

      <Banner
        v-if="mode === 'install' && editedEnabled[comp.chartName] && existingReleases[comp.chartName]?.length"
        color="warning"
        class="existing-release-banner"
      >
        <div>
          {{ t('suseai.wizard.warnings.alreadyInstalled', 'Already installed on {clusters}. Consider excluding this component.', { clusters: existingClusterLabels(comp.chartName) }) }}
          <ul class="mt-5 mb-0">
            <li v-for="(rel, i) in existingReleases[comp.chartName]" :key="i">
              {{ releaseLine(rel) }}
            </li>
          </ul>
        </div>
      </Banner>

      <div v-if="expandedPanels.has(idx)" class="accordion-body">
        <Banner v-if="!editedEnabled[comp.chartName] && runningNames.has(comp.chartName)" color="warning" class="mb-20">
          {{ t('suseai.wizard.warnings.excludingRunningComponent', 'This component is currently deployed — excluding it will remove its resources on the next sync.') }}
        </Banner>
        <p v-if="!editedEnabled[comp.chartName]" class="text-muted">
          {{ t('suseai.wizard.labels.componentExcluded', 'Excluded — this component will not be deployed.') }}
        </p>
        <Tabbed
          v-else
          default-tab="values"
          :use-hash="false"
          :show-extension-tabs="false"
        >
          <Tab
            name="values"
            :label="t('suseai.wizard.labels.valuesTab', 'Values')"
            :weight="2"
          >
            <p
              v-for="(src, i) in describeValuesFrom(comp.valuesFrom)"
              :key="i"
              class="text-muted mb-10"
            >
              <template v-if="src.namespace">
                {{ t('suseai.wizard.hints.valuesFromNote', 'Some values are read at deploy time from {kind} {namespace}/{name}.', src) }}
              </template>
              <template v-else>
                {{ t('suseai.wizard.hints.valuesFromNoteReleaseNs', 'Some values are read at deploy time from {kind} {name} (release namespace).', src) }}
              </template>
            </p>
            <p
              v-for="(hint, i) in mode === 'install' ? findSentinels(blueprintDefaults[comp.chartName] || {}) : []"
              :key="i"
              class="text-muted mb-10"
            >
              {{ t(hint.key, SENTINEL_HINT_FALLBACKS[hint.key], { label: hint.label, path: hint.path }) }}
            </p>
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
              @load-defaults="onValuesUpdate(comp.chartName, JSON.parse(JSON.stringify(blueprintDefaults[comp.chartName] || {})))"
            />
          </Tab>

          <Tab
            name="advanced"
            :label="t('suseai.wizard.labels.advancedTab', 'Advanced')"
            :weight="1"
          >
            <Banner v-if="!advancedEditable" color="info">
              {{ t('suseai.wizard.hints.advancedReadOnly', 'Only users who can update Blueprints can change the deploy order or Kustomize overlays. The current settings are shown.') }}
            </Banner>
            <h3>{{ t('suseai.wizard.labels.deployOrder', 'Deploy Order') }}</h3>
            <Banner v-if="staleDeps[comp.chartName]" color="warning">
              {{ t('suseai.wizard.labels.staleDependsOn', 'These dependencies no longer exist in this Blueprint version and will be removed when you save: {names}', { names: staleDeps[comp.chartName].join(', ') }) }}
            </Banner>
            <div class="row mb-30">
              <div class="col span-6">
                <LabeledSelect
                  v-if="siblingNames(comp.chartName).length"
                  :value="editedDependsOn[comp.chartName] || []"
                  :options="siblingNames(comp.chartName)"
                  :multiple="true"
                  :clearable="true"
                  :close-on-select="false"
                  :label="t('suseai.wizard.labels.dependsOn', 'Deploys after')"
                  :tooltip="t('suseai.wizard.labels.dependsOnTooltip', 'This application is deployed only after the selected applications are Ready.')"
                  :mode="advancedEditable ? _EDIT : _VIEW"
                  @update:value="onDependsOnUpdate(comp.chartName, $event)"
                />
                <p v-else class="text-muted">
                  {{ t('suseai.wizard.labels.dependsOnSingleApp', 'This blueprint has a single application, so there is nothing to order it after.') }}
                </p>
              </div>
            </div>

            <ValuesFromEditor
              v-if="comp.valuesFrom?.length"
              :value="comp.valuesFrom"
              :name="comp.chartName"
              :mode="_VIEW"
            />

            <h3>{{ t('suseai.wizard.labels.kustomizeOverlay', 'Kustomize overlay') }}</h3>
            <template v-if="comp.kustomize">
              <h4>{{ t('suseai.wizard.labels.blueprintOverlay', 'Blueprint overlay (read-only)') }}</h4>
              <KustomizeOverlayEditor
                :value="comp.kustomize"
                :name="`${comp.chartName}-blueprint`"
                :git-repo-options="gitRepoNames"
                mode="view"
                class="mb-20"
              />
            </template>
            <h4>{{ t('suseai.wizard.labels.installOverlay', 'Overlay for this deployment') }}</h4>
            <p class="text-muted mb-10">
              {{ t('suseai.wizard.labels.installOverlayHint', 'Applied after the blueprint overlay.') }}
            </p>
            <p v-if="!advancedEditable && !editedKustomize[comp.chartName]" class="text-muted">
              {{ t('suseai.wizard.labels.kustomizeNone', 'None') }}
            </p>
            <KustomizeOverlayEditor
              v-else
              :key="`${comp.chartName}-${advancedEditable}`"
              :value="editedKustomize[comp.chartName]"
              :name="comp.chartName"
              :git-repo-options="gitRepoNames"
              :mode="advancedEditable ? _EDIT : _VIEW"
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
import { Banner } from '@components/Banner';
import { Checkbox } from '@components/Form/Checkbox';
import { BadgeState } from '@components/BadgeState';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import Tabbed from '@shell/components/Tabbed';
import Tab from '@shell/components/Tabbed/Tab';
import { _EDIT, _VIEW } from '@shell/config/query-params';
import ValuesStep from './ValuesStep.vue';
import KustomizeOverlayEditor from './KustomizeOverlayEditor.vue';
import ValuesFromEditor from './ValuesFromEditor.vue';
import { listGitClusterRepoNames } from '../../../services/rancher-apps';
import { validateDependsOn } from '../../../validators/blueprint';
import {
  seedComponentValues, seedComponentEnabled, seedComponentDependsOn, staleDependsOn, diffComponentOverrides,
  customizedComponentNames, resolveInitialFormState, lockedPaths, sentinelHint,
} from '../../../utils/blueprint-customize';
import { describeValuesFrom } from '../../../utils/values-from';
import type { ExistingRelease } from '../../../utils/blueprint-preflight';
import type { BlueprintComponent, ComponentKustomize } from '../../../types/blueprint-types';
import type { ComponentValueOverride, AIWorkloadStatus } from '../../../types/aiworkload-types';

interface Props {
  components:      BlueprintComponent[];
  existingValues?: ComponentValueOverride[];
  modelValue?:     ComponentValueOverride[];
  // Chart names with a status entry on the existing AIWorkload (manage mode
  // only) — i.e. currently deployed. Used only to decide whether to show the
  // "excluding this will remove it" warning; empty in install mode, where
  // nothing has been deployed yet.
  runningComponents?: string[];
  // Forwarded to the nested ValuesStep, which normalizes 'install' → Rancher
  // Questions' 'create' mode and anything else → 'edit'. Defaults to
  // 'install' so Task 6 (install-mode wiring) needs no changes here; Task 8
  // passes 'manage' explicitly when this step is used from the manage flow,
  // so the Questions/YAML editor gets correct edit-vs-create semantics
  // instead of always behaving as if this were a fresh install.
  mode?: 'install' | 'manage';
  // AIWorkload status for manage mode (contains resolvedValues for locked paths).
  status?: AIWorkloadStatus;
  // Install mode only: releases of the same charts already on the target clusters, by chartName.
  existingReleases?: Record<string, ExistingRelease[]>;
  clusterNames?:     Record<string, string>; // cluster ID → display name
  // False (the default, fail closed) shows the Advanced tab read-only; the
  // edited state is kept as is.
  advancedEditable?: boolean;
}
interface Emits {
  (e: 'update:modelValue', v: ComponentValueOverride[]): void;
  // valid is false when every component has been excluded, or the edited
  // deploy order is invalid — the wizard uses this to block advancing to
  // Review, mirroring the API's own rejection of those specs.
  (e: 'update:valid', v: boolean): void;
}

const props = withDefaults(defineProps<Props>(), {
  mode: 'install', runningComponents: () => [], existingReleases: () => ({}), clusterNames: () => ({}), advancedEditable: false,
});
const emit  = defineEmits<Emits>();
const vm    = getCurrentInstance()!.proxy as any;
const store = vm.$store;

const t = useT();

const runningNames = computed(() => new Set(props.runningComponents));

const clusterLabel = (clusterId: string) => props.clusterNames[clusterId] || clusterId;
const existingClusterLabels = (chartName: string) => Array.from(new Set(props.existingReleases[chartName].map((r) => clusterLabel(r.clusterId)))).join(', ');
const releaseLine = (rel: ExistingRelease) => t('suseai.wizard.warnings.alreadyInstalledRelease', '{cluster}: {release} ({chart}) in namespace {namespace}', {
  cluster: clusterLabel(rel.clusterId), release: rel.release, chart: [rel.chart, rel.version].filter(Boolean).join(' '), namespace: rel.namespace,
});

const expandedPanels      = ref(new Set<number>([0]));
const loadingMap          = ref<Record<string, boolean>>({});
const questionsLoadingMap = ref<Record<string, boolean>>({});
const versionInfoMap      = ref<Record<string, any>>({});

// blueprintDefaults / blueprintEnabled represent the immutable blueprint definition
// to diff against. AIWorkload.spec.componentValues holds the deltas from this baseline.
const blueprintDefaults = computed(() => seedComponentValues(props.components, []));
const blueprintEnabled  = computed(() => seedComponentEnabled(props.components, []));

// Resolve starting form state from any in-progress modelValue overrides or initial existing overrides.
const initialFormState = resolveInitialFormState(
  props.components,
  props.existingValues || [],
  props.modelValue,
);
const editedValues = ref<Record<string, Record<string, any>>>(
  Object.fromEntries(Object.entries(initialFormState.values).map(([k, v]) => [k, JSON.parse(JSON.stringify(v))])),
);
const editedEnabled = ref<Record<string, boolean>>({ ...initialFormState.enabled });
// Advanced tab: deploy order (replaces the blueprint's per component when it
// differs) and install-time overlays (layered after the blueprint's).
const blueprintDependsOn = computed(() => seedComponentDependsOn(props.components, []));
const editedDependsOn = ref<Record<string, string[]>>({ ...initialFormState.dependsOn });
const editedKustomize = ref<Record<string, ComponentKustomize | undefined>>({ ...initialFormState.kustomize });
// Dependencies an existing override names that this Blueprint version no longer
// has (after an upgrade): shown, and dropped from the overrides emitted below.
const staleDeps = staleDependsOn(props.components, props.modelValue !== undefined ? props.modelValue : props.existingValues || []);
const advancedEdits = computed(() => ({
  blueprintDependsOn: blueprintDependsOn.value, editedDependsOn: editedDependsOn.value, editedKustomize: editedKustomize.value,
}));

// Custom git ClusterRepo names: the sources a Git overlay can be fetched from (null while loading).
const gitRepoNames = ref<string[] | null>(null);
listGitClusterRepoNames(store).then((names) => {
  gitRepoNames.value = names;
}).catch(() => {
  // Empty: the ClusterRepo select falls back to free-typing (taggable).
  gitRepoNames.value = [];
});

// customizedNames surfaces the "Customized" badge only on enabled components whose
// values differ from the blueprint definition. Excluded components are not customized.
const customizedNames = computed(() =>
  customizedComponentNames(
    blueprintDefaults.value,
    editedValues.value,
    editedEnabled.value,
    advancedEdits.value,
  ),
);
const allDisabled = computed(() => props.components.length > 0 && props.components.every((c) => !editedEnabled.value[c.chartName]));

// Advanced-tab error: an invalid edited deploy order (the API rejects it too).
const advancedError = computed(() => validateDependsOn(props.components.map((c) => ({ chartName: c.chartName, dependsOn: editedDependsOn.value[c.chartName] }))));

const locked = computed(() => props.mode === 'manage' && props.status ? lockedPaths(props.status) : {});

watch([allDisabled, advancedError], ([disabled, err]) => emit('update:valid', !disabled && err === null), { immediate: true });

if (props.components.length > 0) {
  const first = props.components[0];
  loadChartInfo(first.chartName, first.chartRepo, first.chartVersion);
}

const SENTINEL_HINT_FALLBACKS: Record<string, string> = {
  'suseai.wizard.hints.autoDomainHint':       'Automatic: {label}.<node-ip>.sslip.io — set {path} to use your own domain.',
  'suseai.wizard.hints.autoIngressClassHint': "Automatic: the target cluster's default ingress class — set {path} to choose one.",
};

function findSentinels(vals: unknown): Array<{ key: string; label: string; path: string }> {
  const hints: Array<{ key: string; label: string; path: string }> = [];
  function walk(v: unknown, path: string) {
    if (typeof v === 'object' && v !== null && !Array.isArray(v)) {
      Object.entries(v).forEach(([k, val]) => walk(val, path ? `${path}.${k}` : k));
      return;
    }
    const key = sentinelHint(v);
    if (key && !hints.some((h) => h.key === key && h.path === path)) {
      hints.push({ key, label: String(v).replace(/^aif:auto-domain:/, ''), path });
    }
  }
  walk(vals, '');
  return hints;
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

function emitOverrides() {
  emit('update:modelValue', diffComponentOverrides(blueprintDefaults.value, editedValues.value, blueprintEnabled.value, editedEnabled.value, advancedEdits.value));
}

function onValuesUpdate(chartName: string, newValues: Record<string, any>) {
  editedValues.value = { ...editedValues.value, [chartName]: newValues };
  emitOverrides();
}

function onEnabledUpdate(chartName: string, value: boolean) {
  editedEnabled.value = { ...editedEnabled.value, [chartName]: value };
  emitOverrides();
}

function siblingNames(self: string) {
  return props.components.map((c) => c.chartName).filter((n) => n !== self);
}

function onDependsOnUpdate(chartName: string, deps: string[]) {
  editedDependsOn.value = { ...editedDependsOn.value, [chartName]: deps };
  emitOverrides();
}

function onKustomizeUpdate(chartName: string, kustomize: ComponentKustomize | undefined) {
  editedKustomize.value = { ...editedKustomize.value, [chartName]: kustomize };
  emitOverrides();
}

// Persist the stale-dependency cleanup even if nothing else is edited:
// otherwise saving would resend the removed names and the API would reject it.
if (Object.keys(staleDeps).length) {
  emitOverrides();
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
.panel-checkbox { margin: 0; cursor: default; }
.accordion-panel.is-disabled .panel-title,
.accordion-panel.is-disabled .panel-meta { opacity: 0.6; }
.panel-title { font-weight: 600; font-size: 14px; flex: 1; }
.panel-meta  { font-size: 12px; color: var(--muted); }
.accordion-body { padding: 16px; }
.mb-10 { margin-bottom: 10px; }
.mb-30 { margin-bottom: 30px; }
.existing-release-banner { margin: 12px 16px; }
</style>
