<script lang="ts" setup>
import { computed, getCurrentInstance, ref } from 'vue';
import { BadgeState } from '@components/BadgeState';
import { Banner } from '@components/Banner';
import { LabeledInput } from '@components/Form/LabeledInput';
import RcButton from '@components/RcButton/RcButton.vue';
import Drawer from '@shell/components/Drawer/Chrome.vue';
import DrawerCard from '@shell/components/Drawer/DrawerCard.vue';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import SortableTable from '@shell/components/SortableTable';
import StateDot from '@shell/components/StateDot/index.vue';
import Tabbed from '@shell/components/Tabbed/index.vue';
import Tab from '@shell/components/Tabbed/Tab.vue';
import YamlEditor, { EDITOR_MODES } from '@shell/components/YamlEditor';
import { _VIEW } from '@shell/config/query-params';
import { useT } from '../composables/useT';
import { phaseBadgeColor, phaseBadgeIcon, phaseColor, workloadStatusMessage } from '../utils/workload-status';
import type { AIWorkload } from '../types/aiworkload-types';
import type { Blueprint } from '../types/blueprint-types';
import type { ClusterInfo } from '../types/rancher-types';

// Laid out like the shell's ResourceDetailDrawer ("Show Configuration"): a Config
// tab holding the view-mode form, a YAML tab, and a Close + primary action footer.
// AIWorkloads come from the operator API rather than a Steve model, so the
// drawer's building blocks are reused here instead of the drawer itself.

const props = defineProps<{
  workload:  AIWorkload;
  blueprint: Blueprint | null;
  clusters:  ClusterInfo[];
}>();

// The page passes onManage through the slide-in props, so the footer action runs
// the same flow as the row's Manage button.
const emit = defineEmits(['manage']);

const t = useT();

// The panel is rendered by the shell's SlideInPanelManager, so closing it is a
// store commit rather than an emit. This matches Rancher's Fleet detail drawer.
const store = (getCurrentInstance()!.proxy as any)?.$store;

function close() {
  store?.commit('slideInPanel/close');
}

const w           = computed(() => props.workload);
const isBlueprint = computed(() => w.value.spec.source.sourceType === 'Blueprint');
const displayName = computed(() => w.value.spec.displayName || w.value.metadata.name);
const phase       = computed(() => w.value.status?.phase || 'Pending');

const title = computed(() => store?.getters['i18n/t']?.('suseai.pages.workloads.detail.title', { name: displayName.value }) ||
  `${ displayName.value } (AI Workload) - Details`);

const appSource   = computed(() => w.value.spec.source.app || null);
const sourceName  = computed(() => (isBlueprint.value ? w.value.spec.source.blueprint?.name : appSource.value?.chartName) || '');
const version     = computed(() => (isBlueprint.value ? w.value.spec.source.blueprint?.version : appSource.value?.chartVersion) || '');
const description = computed(() => (isBlueprint.value ? props.blueprint?.spec.description?.trim() || '' : ''));

const targetClusters       = computed(() => w.value.spec.targetClusters || []);
const targetClusterOptions = computed(() => targetClusters.value.map((id) => ({ label: clusterName(id), value: id })));

const fleetBundles  = computed(() => w.value.spec.fleetBundleNames || []);
const helmRelease   = computed(() => (isBlueprint.value ? '' : appSource.value?.release || ''));
const hasUnderlying = computed(() => fleetBundles.value.length > 0 || !!helmRelease.value);

// Blueprint components switched off via componentValues[].enabled=false. The
// operator doesn't deploy them, so they're listed but flagged as excluded.
const excludedNames = computed(() => new Set(
  (w.value.spec.componentValues || []).filter((override) => override.enabled === false).map((override) => override.componentName),
));

const objectRows = computed(() => {
  const components = props.blueprint?.spec.components || [];
  const objects = isBlueprint.value ? components : (appSource.value ? [appSource.value] : []);

  return objects.map((object, index) => ({
    ...object,
    excluded: isBlueprint.value && excludedNames.value.has(object.chartName),
    _key:     `${ object.chartName }/${ object.chartVersion }/${ index }`,
  }));
});

const objectHeaders = computed(() => [
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
    name:        'chartRepo',
    label:       t('suseai.wizard.labels.repository', 'Repository'),
    value:       'chartRepo',
    sort:        'chartRepo',
    dashIfEmpty: true,
  },
]);

// Only overrides that actually set values are worth showing, and an excluded
// component's values never deploy, so they're left out too.
const overrides = computed(() => (w.value.spec.componentValues || []).filter((override) => override.enabled !== false &&
  override.values && Object.keys(override.values).length > 0,
));

const clusterStatuses   = computed(() => w.value.status?.clusterStatuses || []);
const readyMessage      = computed(() => workloadStatusMessage(w.value));
const hasStatus         = computed(() => clusterStatuses.value.length > 0 || !!readyMessage.value);
const statusBannerColor = computed(() => (phase.value === 'Failed' ? 'error' : 'warning'));

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

// Items from a typed list come back without apiVersion/kind; fill them in so
// the YAML tab reads like `kubectl get -o yaml`.
const workloadObject = computed(() => {
  const { apiVersion, kind, ...rest } = w.value;

  return {
    apiVersion: apiVersion || 'ai-factory.suse.com/v1alpha1',
    kind:       kind || 'AIWorkload',
    ...rest,
  };
});

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
  return props.clusters.find((cluster) => cluster.id === clusterId)?.name || clusterId;
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
                      :value="w.spec.deployStrategy || 'Helm'"
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
                  <div class="col span-12">
                    <LabeledSelect
                      :value="targetClusters"
                      :options="targetClusterOptions"
                      :multiple="true"
                      :mode="_VIEW"
                      :label="t('suseai.pages.workloads.detail.targetClusters', 'Target Clusters')"
                    />
                  </div>
                </div>

                <template v-if="hasUnderlying">
                  <h3>{{ t('suseai.pages.workloads.detail.underlyingResources', 'Underlying Resources') }}</h3>
                  <div class="row">
                    <div
                      v-if="fleetBundles.length"
                      class="col span-8"
                    >
                      <LabeledSelect
                        :value="fleetBundles"
                        :options="fleetBundles"
                        :multiple="true"
                        :mode="_VIEW"
                        :label="t('suseai.pages.workloads.detail.fleetBundles', 'Fleet Bundles')"
                      />
                    </div>
                    <div
                      v-if="helmRelease"
                      class="col span-4"
                    >
                      <LabeledInput
                        :value="helmRelease"
                        :mode="_VIEW"
                        :label="t('suseai.pages.workloads.detail.helmRelease', 'Helm Release')"
                      />
                    </div>
                  </div>
                </template>
              </Tab>

              <Tab
                name="applications"
                :label="t('suseai.wizard.labels.applications', 'Applications')"
                :weight="2"
              >
                <SortableTable
                  v-if="objectRows.length"
                  :headers="objectHeaders"
                  :rows="objectRows"
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
                v-if="overrides.length"
                name="values"
                :label="t('suseai.pages.workloads.detail.tabs.values', 'Values')"
                :weight="1"
                @active="refreshValuesEditors"
              >
                <div
                  v-for="(override, index) in overrides"
                  :key="override.componentName"
                  :class="{ 'mt-20': index > 0 }"
                >
                  <h3>{{ override.componentName }}</h3>
                  <YamlEditor
                    ref="valuesEditors"
                    :value="override.values"
                    :as-object="true"
                    :editor-mode="EDITOR_MODES.VIEW_CODE"
                    :mode="_VIEW"
                  />
                </div>
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
            <SortableTable
              v-if="clusterStatuses.length"
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
          </DrawerCard>
        </Tab>

        <Tab
          name="yaml-tab"
          class="yaml-tab"
          :label="t('suseai.pages.workloads.detail.tabs.yaml', 'YAML')"
          :weight="1"
          @active="refreshYamlEditor"
        >
          <YamlEditor
            ref="yamlEditor"
            :value="workloadObject"
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
.yaml-tab {
  :deep() .codemirror-container {
    background-color: var(--body-bg);
    border-radius: var(--border-radius-md);
    padding: 16px;

    .CodeMirror, .CodeMirror-gutter {
      background-color: var(--body-bg);
    }
  }
}
</style>
