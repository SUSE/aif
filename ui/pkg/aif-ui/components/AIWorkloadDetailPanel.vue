<script lang="ts" setup>
import { computed, ref, shallowRef, watchEffect } from 'vue';
import { useStore } from 'vuex';
import { BadgeState } from '@components/BadgeState';
import { Banner } from '@components/Banner';
import { LabeledInput } from '@components/Form/LabeledInput';
import { RcButton } from '@components/RcButton';
import Drawer from '@shell/components/Drawer/Chrome';
import DrawerCard from '@shell/components/Drawer/DrawerCard';
import SortableTable from '@shell/components/SortableTable';
import StateDot from '@shell/components/StateDot';
import Tabbed from '@shell/components/Tabbed';
import Tab from '@shell/components/Tabbed/Tab';
import YamlEditor, { EDITOR_MODES } from '@shell/components/YamlEditor';
import { _VIEW } from '@shell/config/query-params';
import { useT } from '../composables/useT';
import ClusterChips from '../formatters/ClusterChips.vue';
import { componentOverride, deepMergeValues, isComponentEnabled } from '../utils/blueprint-customize';
import { fleetHelmOpLink, fleetWorkspaces } from '../utils/rancher-links';
import { phaseBadgeColor, phaseBadgeIcon, phaseColor, workloadStatusMessage } from '../utils/workload-status';
import type { AIWorkload } from '../types/aiworkload-types';
import type { Blueprint } from '../types/blueprint-types';
import type { ClusterInfo } from '../types/rancher-types';

// Laid out like the shell's ResourceDetailDrawer ("Show Configuration"): a Config
// tab holding the view-mode form, a YAML tab, and a Close + primary action footer.
// AIWorkloads come from the operator API rather than a Steve model, so the
// drawer's building blocks are reused here instead of the drawer itself.

// Getters rather than values: the slide-in keeps the props it was opened with,
// while the page keeps polling, so reading through these keeps the drawer live.
const props = defineProps<{
  workload:  () => AIWorkload | undefined;
  blueprint: () => Blueprint | null;
  clusters:  () => ClusterInfo[];
}>();

// The page passes onManage through the slide-in props, so the footer action runs
// the same flow as the row's Manage button.
const emit = defineEmits(['manage']);

const t = useT();

// The panel is rendered by the shell's SlideInPanelManager, so closing it is a
// store commit rather than an emit. This matches Rancher's Fleet detail drawer.
const store = useStore();

function close() {
  store.commit('slideInPanel/close');
}

// The last known workload, so the body still renders while the panel slides
// shut after the workload disappears (deleted elsewhere).
const w = shallowRef(props.workload() as AIWorkload);

watchEffect(() => {
  const live = props.workload();

  if (live) {
    w.value = live;
  } else {
    close();
  }
});

// The Blueprint at spec.source.blueprint.version, i.e. the requested version.
// Like the rest of the Config tab it describes the desired state, so while an
// upgrade is in flight (or after one failed) the Applications and Values tabs
// show the target version; versionDrift below flags that case.
const blueprint = computed(() => props.blueprint());
const clusters  = computed(() => props.clusters());

const isBlueprint = computed(() => w.value.spec.source.sourceType === 'Blueprint');
const displayName = computed(() => w.value.spec.displayName || w.value.metadata.name);
const phase       = computed(() => w.value.status?.phase || 'Pending');

const title = computed(() => t('suseai.pages.workloads.detail.title', '{name} (AI Workload) - Details', { name: displayName.value }));

const appSource   = computed(() => w.value.spec.source.app || null);
const sourceName  = computed(() => (isBlueprint.value ? w.value.spec.source.blueprint?.name : appSource.value?.chartName) || '');
const version     = computed(() => (isBlueprint.value ? w.value.spec.source.blueprint?.version : appSource.value?.chartVersion) || '');
const description = computed(() => (isBlueprint.value ? blueprint.value?.spec.description?.trim() || '' : ''));

// The Blueprint version actually rendered, when it differs from the requested one.
const deployedVersion = computed(() => (isBlueprint.value ? w.value.status?.deployedSource?.version || '' : ''));
const versionDrift    = computed(() => !!deployedVersion.value && deployedVersion.value !== version.value);

const deployStrategy = computed(() => w.value.spec.deployStrategy || 'Helm');
const targetClusters = computed(() => w.value.spec.targetClusters || []);

// spec.fleetBundleNames are the names of the HelmOps the operator owns. They're
// recorded for every strategy, but HelmOps only exist for the Fleet-based ones.
// A HelmOp lives in each workspace its targets map to, so a mixed local +
// downstream target gets one link per workspace.
const fleetHelmOps = computed(() => {
  if (deployStrategy.value === 'Helm') {
    return [];
  }
  const workspaces = fleetWorkspaces(targetClusters.value);

  return (w.value.spec.fleetBundleNames || []).flatMap((name) => workspaces.map((workspace) => ({
    key:   `${ workspace }/${ name }`,
    label: workspaces.length > 1 ? `${ name } (${ workspace })` : name,
    url:   fleetHelmOpLink(workspace, name),
  })));
});

// Blueprint components switched off via componentValues[].enabled=false. The
// operator doesn't deploy them, so they're listed but flagged as excluded.
function isExcluded(chartName: string): boolean {
  return !isComponentEnabled(w.value.spec.componentValues, chartName);
}

// The release each component actually installed, as reported by the operator.
const installedReleases = computed(() => new Map(
  (w.value.status?.componentStatuses || []).filter((status) => status.releaseName).map((status) => [status.componentName, status.releaseName]),
));

const componentRows = computed(() => (blueprint.value?.spec.components || []).map((component, index) => ({
  ...component,
  excluded:        isExcluded(component.chartName),
  releaseName:     installedReleases.value.get(component.chartName) || component.releaseName || '',
  targetNamespace: component.targetNamespace || w.value.spec.targetNamespace,
  _key:            `${ component.chartName }/${ component.chartVersion }/${ index }`,
})));

const componentHeaders = computed(() => [
  {
    name:  'chartName',
    label: t('suseai.wizard.labels.chart', 'Chart'),
    value: 'chartName',
    sort:  'chartName',
  },
  {
    name:        'chartVersion',
    label:       t('suseai.common.labels.version', 'Version'),
    value:       'chartVersion',
    sort:        'chartVersion',
    dashIfEmpty: true,
  },
  {
    name:        'releaseName',
    label:       t('suseai.pages.workloads.detail.releaseName', 'Release Name'),
    value:       'releaseName',
    sort:        'releaseName',
    dashIfEmpty: true,
  },
  {
    name:        'targetNamespace',
    label:       t('suseai.common.labels.namespace', 'Namespace'),
    value:       'targetNamespace',
    sort:        'targetNamespace',
    dashIfEmpty: true,
  },
  {
    name:        'chartRepo',
    label:       t('suseai.wizard.labels.repository', 'Repository'),
    value:       'chartRepo',
    sort:        'chartRepo',
    dashIfEmpty: true,
  },
]);

function hasKeys(values: Record<string, any> | undefined): values is Record<string, any> {
  return !!values && Object.keys(values).length > 0;
}

// Whether the Values tab can show what each component deploys with: that needs
// the Blueprint's defaults to merge the overrides onto.
const showsMergedValues = computed(() => isBlueprint.value && !!blueprint.value);

// One editor per deployed component, holding the values the operator renders:
// the Blueprint defaults with the workload's override deep-merged on top, the
// same way resolveComponentValues does. Without the Blueprint (an App workload,
// or a Blueprint that isn't loaded) only the stored overrides can be shown.
// Excluded components never deploy, so they're left out.
const valueSections = computed(() => {
  const overrides = w.value.spec.componentValues;

  if (!showsMergedValues.value) {
    const names = [...new Set((overrides || []).map((override) => override.componentName))];

    return names
      .filter((name) => isComponentEnabled(overrides, name))
      .map((name) => ({ name, values: componentOverride(overrides, name), customized: true }))
      .filter((section): section is { name: string; values: Record<string, any>; customized: boolean } => hasKeys(section.values));
  }

  return (blueprint.value?.spec.components || [])
    .filter((component) => !isExcluded(component.chartName))
    .map((component) => {
      const override = componentOverride(overrides, component.chartName);

      return {
        name:       component.chartName,
        values:     deepMergeValues(component.values || {}, override || {}),
        customized: hasKeys(override),
      };
    })
    .filter((section) => hasKeys(section.values));
});

const clusterStatuses   = computed(() => w.value.status?.clusterStatuses || []);
const readyMessage      = computed(() => workloadStatusMessage(w.value));
const statusBannerColor = computed(() => (phase.value === 'Failed' ? 'error' : 'warning'));

const componentStatuses = computed(() => (w.value.status?.componentStatuses || []).map((status) => ({
  ...status,
  _key: `${ status.componentName }/${ status.clusterId }`,
})));

// A finished operation is history; only in-flight or failed ones need attention.
const activeOperation      = computed(() => {
  const operation = w.value.status?.activeOperation;

  return operation && operation.state !== 'Succeeded' ? operation : null;
});
const operationBannerColor = computed(() => {
  switch (activeOperation.value?.state) {
  case 'Failed': return 'error';
  case 'Superseded': return 'warning';
  default: return 'info';
  }
});

const hasStatus = computed(() => !!readyMessage.value || !!activeOperation.value || componentStatuses.value.length > 0 || clusterStatuses.value.length > 0);

const componentStatusHeaders = computed(() => [
  {
    name:  'componentName',
    label: t('suseai.pages.workloads.detail.component', 'Component'),
    value: 'componentName',
    sort:  ['componentName', 'clusterId'],
  },
  {
    name:        'releaseName',
    label:       t('suseai.pages.workloads.detail.releaseName', 'Release Name'),
    value:       'releaseName',
    sort:        'releaseName',
    dashIfEmpty: true,
  },
  {
    name:  'clusterId',
    label: t('suseai.common.labels.cluster', 'Cluster'),
    value: 'clusterId',
    sort:  'clusterId',
  },
  {
    name:  'phase',
    label: t('suseai.common.labels.status', 'Status'),
    value: 'phase',
    sort:  'phase',
    width: 150,
  },
  {
    name:        'installedVersion',
    label:       t('suseai.common.labels.version', 'Version'),
    value:       'installedVersion',
    sort:        'installedVersion',
    dashIfEmpty: true,
  },
  {
    name:        'message',
    label:       t('suseai.pages.workloads.detail.message', 'Message'),
    value:       'message',
    sort:        'message',
    dashIfEmpty: true,
  },
]);

const statusHeaders = computed(() => [
  {
    name:  'clusterId',
    label: t('suseai.common.labels.cluster', 'Cluster'),
    value: 'clusterId',
    sort:  'clusterId',
  },
  {
    name:  'phase',
    label: t('suseai.common.labels.status', 'Status'),
    value: 'phase',
    sort:  'phase',
    width: 150,
  },
  {
    name:        'message',
    label:       t('suseai.pages.workloads.detail.message', 'Message'),
    value:       'message',
    sort:        'message',
    dashIfEmpty: true,
  },
]);

// Manage is the equivalent of Rancher's "Edit Config". Mirrors the row's Manage
// button: an App workload must be Running, and a Blueprint workload can't have an
// operation in flight. Like Rancher, the button is hidden rather than disabled.
const canManage = computed(() => (isBlueprint.value ? w.value.status?.activeOperation?.state !== 'InProgress' : phase.value === 'Running'));

function manage() {
  emit('manage');
  close();
}

// CodeMirror measures nothing while its tab is hidden, so refresh on show, as
// the shell's YamlTab does.
type Refreshable = { refresh: () => void };

const yamlEditor    = ref<Refreshable | null>(null);
const valuesEditors = ref<Refreshable[]>([]);

function refreshYamlEditor() {
  yamlEditor.value?.refresh();
}

function refreshValuesEditors() {
  valuesEditors.value.forEach((editor) => editor?.refresh());
}

function clusterName(clusterId: string): string {
  return clusters.value.find((cluster) => cluster.id === clusterId)?.name || clusterId;
}
</script>

<template>
  <Drawer
    :aria-target="displayName"
    @close="close"
  >
    <template #title>
      <StateDot
        :color="phaseColor(phase)"
        class="mmr-3"
      />
      {{ title }}
    </template>

    <template #body>
      <Tabbed
        default-tab="config-tab"
        :use-hash="false"
        :show-extension-tabs="false"
        :remove-borders="true"
      >
        <Tab
          name="config-tab"
          :label="t('suseai.pages.workloads.detail.tabs.config', 'Config')"
          :weight="3"
        >
          <DrawerCard>
            <div class="row mb-20">
              <div class="col span-4">
                <LabeledInput
                  :value="w.metadata.namespace"
                  :mode="_VIEW"
                  :label="t('suseai.common.labels.namespace', 'Namespace')"
                />
              </div>
              <div class="col span-4">
                <LabeledInput
                  :value="w.metadata.name"
                  :mode="_VIEW"
                  :label="t('suseai.common.labels.name', 'Name')"
                />
              </div>
              <div class="col span-4">
                <LabeledInput
                  :value="w.spec.displayName"
                  :mode="_VIEW"
                  :label="t('suseai.pages.workloads.detail.displayName', 'Display Name')"
                />
              </div>
            </div>

            <Banner
              v-if="versionDrift"
              class="mt-0"
              color="warning"
              data-testid="version-drift"
            >
              {{ t('suseai.pages.workloads.detail.versionDrift', 'Version {deployed} is deployed. This configuration describes the requested version {requested}.', { deployed: deployedVersion, requested: version }) }}
            </Banner>

            <Tabbed
              :side-tabs="true"
              :use-hash="false"
              :show-extension-tabs="false"
            >
              <Tab
                name="general"
                :label="t('suseai.pages.workloads.detail.tabs.general', 'General')"
                :weight="3"
              >
                <h3>{{ t('suseai.pages.workloads.detail.source', 'Source') }}</h3>
                <div class="row mb-20">
                  <div class="col span-4">
                    <LabeledInput
                      :value="w.spec.source.sourceType"
                      :mode="_VIEW"
                      :label="t('suseai.pages.workloads.detail.sourceType', 'Source Type')"
                    />
                  </div>
                  <div class="col span-4">
                    <LabeledInput
                      :value="sourceName"
                      :mode="_VIEW"
                      :label="isBlueprint ? t('suseai.wizard.labels.blueprint', 'Blueprint') : t('suseai.wizard.labels.chart', 'Chart')"
                    />
                  </div>
                  <div class="col span-4">
                    <LabeledInput
                      :value="version"
                      :mode="_VIEW"
                      :label="t('suseai.common.labels.version', 'Version')"
                    />
                  </div>
                </div>
                <div
                  v-if="appSource"
                  class="row mb-20"
                >
                  <div class="col span-8">
                    <LabeledInput
                      :value="appSource.chartRepo"
                      :mode="_VIEW"
                      :label="t('suseai.wizard.labels.repository', 'Repository')"
                    />
                  </div>
                  <div class="col span-4">
                    <LabeledInput
                      :value="appSource.release"
                      :mode="_VIEW"
                      :label="t('suseai.pages.workloads.detail.releaseName', 'Release Name')"
                    />
                  </div>
                </div>
                <div
                  v-if="description"
                  class="row mb-20"
                >
                  <div class="col span-12">
                    <LabeledInput
                      :value="description"
                      type="multiline"
                      :mode="_VIEW"
                      :label="t('suseai.common.labels.description', 'Description')"
                    />
                  </div>
                </div>

                <h3>{{ t('suseai.pages.workloads.detail.target', 'Target') }}</h3>
                <div class="row mb-20">
                  <div class="col span-4">
                    <LabeledInput
                      :value="deployStrategy"
                      :mode="_VIEW"
                      :label="t('suseai.pages.workloads.detail.deployStrategy', 'Deploy Strategy')"
                    />
                  </div>
                  <div class="col span-4">
                    <LabeledInput
                      :value="w.spec.targetNamespace"
                      :mode="_VIEW"
                      :label="t('suseai.pages.workloads.detail.targetNamespace', 'Target Namespace')"
                    />
                  </div>
                </div>
                <div class="row mb-20">
                  <div
                    class="col span-12"
                    data-testid="target-clusters"
                  >
                    <label class="field-label">{{ t('suseai.pages.workloads.detail.targetClusters', 'Target Clusters') }}</label>
                    <ClusterChips
                      v-if="targetClusters.length"
                      :clusters="targetClusters"
                      :cluster-info="clusters"
                      :show-label="false"
                    />
                    <span
                      v-else
                      class="text-muted"
                    >&mdash;</span>
                  </div>
                </div>

                <template v-if="fleetHelmOps.length">
                  <h3>{{ t('suseai.pages.workloads.detail.underlyingResources', 'Underlying Resources') }}</h3>
                  <div class="row">
                    <div
                      class="col span-12"
                      data-testid="fleet-helmops"
                    >
                      <label class="field-label">{{ t('suseai.pages.workloads.detail.fleetHelmOps', 'Fleet HelmOps') }}</label>
                      <ul class="resource-links">
                        <li
                          v-for="helmOp in fleetHelmOps"
                          :key="helmOp.key"
                        >
                          <router-link :to="helmOp.url">
                            {{ helmOp.label }}
                          </router-link>
                        </li>
                      </ul>
                    </div>
                  </div>
                </template>
              </Tab>

              <!-- An App workload is a single chart, already shown under General. -->
              <Tab
                v-if="isBlueprint"
                name="applications"
                :label="t('suseai.wizard.labels.applications', 'Applications')"
                :weight="2"
              >
                <SortableTable
                  v-if="componentRows.length"
                  :headers="componentHeaders"
                  :rows="componentRows"
                  key-field="_key"
                  default-sort-by="chartName"
                  :table-actions="false"
                  :row-actions="false"
                  :search="false"
                >
                  <template #cell:chartName="{ row }">
                    {{ row.chartName }}
                    <BadgeState
                      v-if="row.excluded"
                      class="ml-5"
                      color="badge-disabled"
                      :label="t('suseai.wizard.labels.excluded', 'Excluded')"
                    />
                  </template>
                </SortableTable>
                <p
                  v-else
                  class="text-muted"
                >
                  {{ t('suseai.pages.workloads.detail.noBlueprint', 'Blueprint details are not available for this workload.') }}
                </p>
              </Tab>

              <Tab
                name="values"
                :label="t('suseai.pages.workloads.detail.tabs.values', 'Values')"
                :weight="1"
                @active="refreshValuesEditors"
              >
                <Banner
                  class="mt-0"
                  color="info"
                >
                  {{ t('suseai.pages.workloads.detail.plaintextNotice', 'Values are shown in plain text. Keep credentials in Kubernetes Secrets rather than in Helm values.') }}
                </Banner>
                <p
                  v-if="isBlueprint"
                  class="text-muted mb-20"
                  data-testid="values-caption"
                >
                  {{ showsMergedValues
                    ? t('suseai.pages.workloads.detail.valuesMerged', 'Each component shows the values it deploys with: the Blueprint defaults, with this workload\'s overrides merged on top.')
                    : t('suseai.pages.workloads.detail.valuesOverridesOnly', 'The Blueprint is not available, so only this workload\'s overrides are shown. They are merged onto the Blueprint defaults at deploy time.') }}
                </p>
                <div
                  v-for="(section, index) in valueSections"
                  :key="section.name"
                  :class="{ 'mt-20': index > 0 }"
                  data-testid="value-section"
                >
                  <h3>
                    {{ section.name }}
                    <BadgeState
                      v-if="isBlueprint"
                      class="ml-5"
                      :color="section.customized ? 'bg-info' : 'badge-disabled'"
                      :label="section.customized ? t('suseai.wizard.labels.customized', 'Customized') : t('suseai.pages.workloads.detail.blueprintDefaults', 'Blueprint defaults')"
                    />
                  </h3>
                  <YamlEditor
                    ref="valuesEditors"
                    class="code-card"
                    :value="section.values"
                    :as-object="true"
                    :editor-mode="EDITOR_MODES.VIEW_CODE"
                    :mode="_VIEW"
                  />
                </div>
                <p
                  v-if="!valueSections.length"
                  class="text-muted"
                >
                  {{ t('suseai.pages.workloads.detail.noValues', 'No custom values are set; the chart defaults are used.') }}
                </p>
              </Tab>
            </Tabbed>
          </DrawerCard>
        </Tab>

        <Tab
          v-if="hasStatus"
          name="status-tab"
          :label="t('suseai.common.labels.status', 'Status')"
          :weight="2"
        >
          <DrawerCard>
            <Banner
              v-if="readyMessage"
              class="mt-0"
              :color="statusBannerColor"
            >
              {{ readyMessage }}
            </Banner>
            <Banner
              v-if="activeOperation"
              class="mt-0"
              :color="operationBannerColor"
              data-testid="active-operation"
            >
              {{ t('suseai.pages.workloads.detail.operation', '{type}: {state}', { type: activeOperation.type, state: activeOperation.state }) }}<span v-if="activeOperation.reason"> ({{ activeOperation.reason }})</span>
            </Banner>

            <template v-if="componentStatuses.length">
              <h3>{{ t('suseai.pages.workloads.detail.components', 'Components') }}</h3>
              <SortableTable
                class="mb-20"
                :headers="componentStatusHeaders"
                :rows="componentStatuses"
                key-field="_key"
                default-sort-by="componentName"
                :table-actions="false"
                :row-actions="false"
                :search="false"
              >
                <template #cell:clusterId="{ row }">
                  {{ clusterName(row.clusterId) }}
                </template>
                <template #cell:phase="{ row }">
                  <BadgeState
                    :color="phaseBadgeColor(row.phase)"
                    :icon="phaseBadgeIcon(row.phase)"
                    :label="row.phase"
                  />
                </template>
              </SortableTable>
            </template>

            <template v-if="clusterStatuses.length">
              <h3>{{ t('suseai.pages.workloads.detail.clusters', 'Clusters') }}</h3>
              <SortableTable
                :headers="statusHeaders"
                :rows="clusterStatuses"
                key-field="clusterId"
                default-sort-by="clusterId"
                :table-actions="false"
                :row-actions="false"
                :search="false"
              >
                <template #cell:clusterId="{ row }">
                  <div>{{ clusterName(row.clusterId) }}</div>
                  <div
                    v-if="clusterName(row.clusterId) !== row.clusterId"
                    class="text-muted text-small"
                  >
                    {{ row.clusterId }}
                  </div>
                </template>
                <template #cell:phase="{ row }">
                  <BadgeState
                    :color="phaseBadgeColor(row.phase)"
                    :icon="phaseBadgeIcon(row.phase)"
                    :label="row.phase"
                  />
                </template>
              </SortableTable>
            </template>
          </DrawerCard>
        </Tab>

        <Tab
          name="yaml-tab"
          :label="t('suseai.pages.workloads.detail.tabs.yaml', 'YAML')"
          :weight="1"
          @active="refreshYamlEditor"
        >
          <Banner
            class="mt-0"
            color="info"
          >
            {{ t('suseai.pages.workloads.detail.plaintextNotice', 'Values are shown in plain text. Keep credentials in Kubernetes Secrets rather than in Helm values.') }}
          </Banner>
          <YamlEditor
            ref="yamlEditor"
            class="code-card"
            :value="w"
            :as-object="true"
            :editor-mode="EDITOR_MODES.VIEW_CODE"
            :mode="_VIEW"
          />
        </Tab>
      </Tabbed>
    </template>

    <template #additional-actions>
      <RcButton
        v-if="canManage"
        variant="primary"
        size="large"
        @click="manage"
      >
        {{ t('suseai.pages.workloads.detail.manage', 'Manage') }}
      </RcButton>
    </template>
  </Drawer>
</template>

<style lang="scss" scoped>
// Same card treatment the shell's ResourceDetailDrawer YamlTab gives its editor.
.code-card {
  :deep() .codemirror-container {
    background-color: var(--body-bg);
    border-radius: var(--border-radius-md);
    padding: 16px;

    .CodeMirror, .CodeMirror-gutter {
      background-color: var(--body-bg);
    }
  }
}

// Label for read-only fields that hold links/chips rather than a LabeledInput.
.field-label {
  display: block;
  margin-bottom: 6px;
  color: var(--input-label);
}

.resource-links {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 16px;
  margin: 0;
  padding: 0;
  list-style: none;
}
</style>
