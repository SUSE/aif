<script lang="ts" setup>
import { computed, getCurrentInstance } from 'vue';
import yaml from 'js-yaml';
import { Accordion } from '@components/Accordion';
import { BadgeState } from '@components/BadgeState';
import { Banner } from '@components/Banner';
import RcTag from '@components/Pill/RcTag/RcTag.vue';
import Drawer from '@shell/components/Drawer/Chrome.vue';
import DrawerCard from '@shell/components/Drawer/DrawerCard.vue';
import SortableTable from '@shell/components/SortableTable';
import Tabbed from '@shell/components/Tabbed/index.vue';
import Tab from '@shell/components/Tabbed/Tab.vue';
import { useT } from '../composables/useT';
import { phaseBadgeColor, phaseBadgeIcon, workloadStatusMessage } from '../utils/workload-status';
import type { AIWorkload } from '../types/aiworkload-types';
import type { Blueprint } from '../types/blueprint-types';
import type { ClusterInfo } from '../types/rancher-types';

const props = defineProps<{
  workload:  AIWorkload;
  blueprint: Blueprint | null;
  clusters:  ClusterInfo[];
}>();

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

const sourceName = computed(() => {
  const source = w.value.spec.source;

  return source.sourceType === 'App' ? (source.app?.chartName || '—') : (source.blueprint?.name || '—');
});

const version = computed(() => {
  const source = w.value.spec.source;

  return source.sourceType === 'App' ? (source.app?.chartVersion || '—') : (source.blueprint?.version || '—');
});

const description       = computed(() => (isBlueprint.value ? props.blueprint?.spec.description || '' : ''));
const components        = computed(() => props.blueprint?.spec.components || []);
const appSource         = computed(() => w.value.spec.source.app || null);
const clusterStatuses   = computed(() => w.value.status?.clusterStatuses || []);
const readyMessage      = computed(() => workloadStatusMessage(w.value));
const hasStatus         = computed(() => clusterStatuses.value.length > 0 || !!readyMessage.value);
const statusBannerColor = computed(() => phase.value === 'Failed' ? 'error' : 'warning');

// Only overrides that actually set values are worth showing.
const overrides = computed(() =>
  (w.value.spec.componentValues || []).filter((override) => override.values && Object.keys(override.values).length > 0),
);
const fleetBundles  = computed(() => w.value.spec.fleetBundleNames || []);
const helmRelease   = computed(() => (w.value.spec.source.sourceType === 'App' ? appSource.value?.release || '' : ''));
const hasUnderlying = computed(() => fleetBundles.value.length > 0 || !!helmRelease.value);

const objectRows = computed(() => {
  const objects = isBlueprint.value ? components.value : (appSource.value ? [appSource.value] : []);

  return objects.map((object, index) => ({
    ...object,
    _key: `${ object.chartName }/${ object.chartVersion }/${ index }`,
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
    name:  'chartVersion',
    label: t('suseai.common.labels.version', 'Version'),
    value: 'chartVersion',
    sort:  'chartVersion',
    width: 160,
  },
  {
    name:  'chartRepo',
    label: t('suseai.wizard.labels.repository', 'Repository'),
    value: 'chartRepo',
    sort:  'chartRepo',
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

function clusterName(clusterId: string): string {
  return props.clusters.find((cluster) => cluster.id === clusterId)?.name || clusterId;
}

function dumpYaml(values: Record<string, any> | undefined): string {
  try {
    return yaml.dump(values || {}, { indent: 2, lineWidth: -1 });
  } catch {
    return JSON.stringify(values ?? {}, null, 2);
  }
}
</script>

<template>
  <Drawer
    :aria-target="w.metadata.name"
    :remove-footer="true"
    @close="close"
  >
    <template #title>
      <div class="wl-title">
        <span class="wl-title__name">{{ displayName }}</span>
        <BadgeState
          :color="phaseBadgeColor(phase)"
          :icon="phaseBadgeIcon(phase)"
          :label="phase"
        />
      </div>
    </template>

    <template #body>
      <Tabbed
        default-tab="overview"
        :use-hash="false"
        :show-extension-tabs="false"
        :remove-borders="true"
      >
        <Tab
          name="overview"
          :label="t('suseai.pages.workloads.detail.overview', 'Overview')"
          :count="false"
          :weight="400"
        >
          <div class="wl-card-stack">
            <DrawerCard>
              <h3>{{ t('suseai.pages.workloads.detail.overview', 'Overview') }}</h3>

              <div class="wl-details-grid">
                <div class="wl-detail-field">
                  <div class="text-label">{{ t('suseai.common.labels.name', 'Name') }}</div>
                  <div class="wl-detail-value monospace">{{ w.metadata.name }}</div>
                </div>

                <div class="wl-detail-field">
                  <div class="text-label">{{ t('suseai.common.labels.namespace', 'Namespace') }}</div>
                  <div class="wl-detail-value monospace">{{ w.metadata.namespace }}</div>
                </div>

                <div class="wl-detail-field">
                  <div class="text-label">{{ t('suseai.common.labels.status', 'Status') }}</div>
                  <div class="wl-detail-value">
                    <BadgeState
                      :color="phaseBadgeColor(phase)"
                      :icon="phaseBadgeIcon(phase)"
                      :label="phase"
                    />
                  </div>
                </div>

                <div class="wl-detail-field">
                  <div class="text-label">{{ t('suseai.pages.workloads.detail.source', 'Source') }}</div>
                  <div class="wl-detail-value wl-inline-value">
                    <RcTag type="inactive">{{ w.spec.source.sourceType }}</RcTag>
                    <span>{{ sourceName }}</span>
                  </div>
                </div>

                <div class="wl-detail-field">
                  <div class="text-label">{{ t('suseai.common.labels.version', 'Version') }}</div>
                  <div class="wl-detail-value monospace">{{ version }}</div>
                </div>

                <div class="wl-detail-field">
                  <div class="text-label">{{ t('suseai.pages.workloads.detail.deployStrategy', 'Deploy strategy') }}</div>
                  <div class="wl-detail-value">{{ w.spec.deployStrategy || 'Helm' }}</div>
                </div>

                <div class="wl-detail-field">
                  <div class="text-label">{{ t('suseai.pages.workloads.detail.targetNamespace', 'Target namespace') }}</div>
                  <div class="wl-detail-value monospace">{{ w.spec.targetNamespace || '—' }}</div>
                </div>

                <div class="wl-detail-field wl-detail-field--wide">
                  <div class="text-label">{{ t('suseai.pages.workloads.detail.targetClusters', 'Target clusters') }}</div>
                  <div
                    v-if="w.spec.targetClusters?.length"
                    class="wl-detail-value wl-tag-list"
                  >
                    <RcTag
                      v-for="clusterId in w.spec.targetClusters"
                      :key="clusterId"
                      type="inactive"
                      :title="clusterId"
                    >
                      {{ clusterName(clusterId) }}
                    </RcTag>
                  </div>
                  <div v-else class="wl-detail-value text-muted">—</div>
                </div>
              </div>
            </DrawerCard>

            <DrawerCard v-if="description">
              <h3>{{ t('suseai.pages.workloads.detail.blueprintDescription', 'Blueprint description') }}</h3>
              <p class="wl-description">{{ description }}</p>
            </DrawerCard>

            <DrawerCard v-if="hasUnderlying">
              <h3>{{ t('suseai.pages.workloads.detail.underlyingResources', 'Underlying resources') }}</h3>

              <div class="wl-details-grid">
                <div v-if="fleetBundles.length" class="wl-detail-field">
                  <div class="text-label">{{ t('suseai.pages.workloads.detail.fleetBundles', 'Fleet bundles') }}</div>
                  <div class="wl-detail-value wl-tag-list">
                    <RcTag
                      v-for="bundle in fleetBundles"
                      :key="bundle"
                      type="inactive"
                    >
                      <span class="monospace">{{ bundle }}</span>
                    </RcTag>
                  </div>
                </div>

                <div v-if="helmRelease" class="wl-detail-field">
                  <div class="text-label">{{ t('suseai.pages.workloads.detail.helmRelease', 'Helm release') }}</div>
                  <div class="wl-detail-value monospace">{{ helmRelease }}</div>
                </div>
              </div>
            </DrawerCard>
          </div>
        </Tab>

        <Tab
          name="objects"
          :label="t('suseai.wizard.labels.applications', 'Applications')"
          :count="objectRows.length"
          :weight="300"
        >
          <DrawerCard>
            <h3>{{ t('suseai.pages.workloads.detail.objects', 'Objects being created') }}</h3>
            <p class="text-muted">
              {{ t('suseai.pages.workloads.detail.objectsHint', 'Charts this workload deploys to the target cluster(s).') }}
            </p>

            <SortableTable
              v-if="objectRows.length"
              class="wl-table"
              :headers="objectHeaders"
              :rows="objectRows"
              key-field="_key"
              default-sort-by="chartName"
              :table-actions="false"
              :row-actions="false"
              :search="false"
              :overflow-x="true"
            >
              <template #cell:chartVersion="{ row }">
                <span class="monospace">{{ row.chartVersion || '—' }}</span>
              </template>
              <template #cell:chartRepo="{ row }">
                <span class="monospace">{{ row.chartRepo || '—' }}</span>
              </template>
            </SortableTable>

            <p v-else class="text-muted wl-empty">
              {{ t('suseai.pages.workloads.detail.noBlueprint', 'Blueprint details are not available for this workload.') }}
            </p>
          </DrawerCard>
        </Tab>

        <Tab
          v-if="hasStatus"
          name="status"
          :label="t('suseai.common.labels.status', 'Status')"
          :count="clusterStatuses.length || false"
          :weight="200"
        >
          <DrawerCard>
            <h3>{{ t('suseai.pages.workloads.detail.perClusterStatus', 'Per-cluster status') }}</h3>

            <Banner
              v-if="readyMessage"
              :color="statusBannerColor"
              role="status"
            >
              {{ readyMessage }}
            </Banner>

            <SortableTable
              v-if="clusterStatuses.length"
              :class="{ 'wl-table': readyMessage }"
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
                  class="text-muted text-small monospace"
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
          v-if="overrides.length"
          name="configuration"
          :label="t('suseai.wizard.sections.configuration', 'Configuration')"
          :count="overrides.length"
          :weight="100"
        >
          <DrawerCard>
            <h3>{{ t('suseai.pages.workloads.detail.componentValues', 'Configuration overrides') }}</h3>

            <div class="wl-accordions">
              <Accordion
                v-for="override in overrides"
                :key="override.componentName"
                :title="override.componentName"
              >
                <template #header>
                  <span class="monospace">{{ override.componentName }}</span>
                </template>
                <pre class="wl-yaml"><code>{{ dumpYaml(override.values) }}</code></pre>
              </Accordion>
            </div>
          </DrawerCard>
        </Tab>
      </Tabbed>
    </template>
  </Drawer>
</template>

<style lang="scss" scoped>
.wl-title {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  width: 100%;

  &__name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.wl-card-stack {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.wl-details-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 24px 32px;
}

.wl-detail-field {
  min-width: 0;

  &--wide {
    grid-column: 1 / -1;
  }
}

.wl-detail-value {
  min-height: 24px;
  margin-top: 6px;
  line-height: 20px;
  overflow-wrap: anywhere;
}

.wl-inline-value,
.wl-tag-list {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.wl-description {
  line-height: 20px;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.wl-table,
.wl-empty {
  margin-top: 16px;
}

.wl-accordions {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.wl-yaml {
  margin: 0;
}
</style>
