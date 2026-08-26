<script lang="ts" setup>
import { computed, ref } from 'vue';
import yaml from 'js-yaml';
import AppModal from '@shell/components/AppModal';
import { BadgeState } from '@components/BadgeState';
import ClusterChips from '../formatters/ClusterChips.vue';
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

const emit = defineEmits<{ (e: 'close'): void }>();

const t = useT();

const w           = computed(() => props.workload);
const isBlueprint = computed(() => w.value.spec.source.sourceType === 'Blueprint');
const displayName = computed(() => w.value.spec.displayName || w.value.metadata.name);
const phase       = computed(() => w.value.status?.phase || 'Pending');

const sourceName = computed(() => {
  const s = w.value.spec.source;
  return s.sourceType === 'App' ? (s.app?.chartName || '—') : (s.blueprint?.name || '—');
});

const version = computed(() => {
  const s = w.value.spec.source;
  return s.sourceType === 'App' ? (s.app?.chartVersion || '—') : (s.blueprint?.version || '—');
});

const description     = computed(() => (isBlueprint.value ? props.blueprint?.spec.description || '' : ''));
const components      = computed(() => props.blueprint?.spec.components || []);
const appSource       = computed(() => w.value.spec.source.app || null);
const clusterStatuses = computed(() => w.value.status?.clusterStatuses || []);
const readyMessage    = computed(() => workloadStatusMessage(w.value));

// Only overrides that actually set values are worth showing.
const overrides = computed(() =>
  (w.value.spec.componentValues || []).filter((o) => o.values && Object.keys(o.values).length > 0),
);
const fleetBundles = computed(() => w.value.spec.fleetBundleNames || []);
const helmRelease  = computed(() => (w.value.spec.source.sourceType === 'App' ? appSource.value?.release || '' : ''));

const hasUnderlying = computed(() => fleetBundles.value.length > 0 || !!helmRelease.value);

// Collapsible per-component override blocks.
const openOverrides = ref<Set<string>>(new Set());
function toggleOverride(name: string) {
  const next = new Set(openOverrides.value);
  if (next.has(name)) next.delete(name); else next.add(name);
  openOverrides.value = next;
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
  <AppModal :click-to-close="true" :width="640" @close="emit('close')">
    <div class="wl-detail">
      <!-- Header -->
      <header class="wl-detail-header">
        <div class="wl-detail-title-row">
          <h3 class="wl-detail-title">{{ displayName }}</h3>
          <BadgeState
            :color="phaseBadgeColor(phase)"
            :icon="phaseBadgeIcon(phase)"
            :label="phase"
          />
        </div>
        <div class="wl-detail-subrow">
          <span class="mono-chip">{{ w.metadata.name }}</span>
          <span class="mono-chip">{{ w.metadata.namespace }}</span>
        </div>
        <button
          class="wl-detail-close"
          type="button"
          :aria-label="t('suseai.pages.workloads.detail.close', 'Close')"
          @click="emit('close')"
        >
          <i class="icon icon-close" />
        </button>
      </header>

      <div class="wl-detail-body">
        <!-- Overview -->
        <section class="wl-section">
          <h4 class="wl-section-heading">{{ t('suseai.pages.workloads.detail.overview', 'Overview') }}</h4>
          <dl class="wl-dl">
            <dt>{{ t('suseai.pages.workloads.detail.source', 'Source') }}</dt>
            <dd>
              <span
                class="source-type-badge"
                :class="isBlueprint ? 'source-blueprint' : 'source-app'"
              >{{ w.spec.source.sourceType }}</span>
              <span class="wl-dd-text">{{ sourceName }}</span>
            </dd>

            <dt>{{ t('suseai.common.labels.version', 'Version') }}</dt>
            <dd class="mono">{{ version }}</dd>

            <dt>{{ t('suseai.pages.workloads.detail.deployStrategy', 'Deploy strategy') }}</dt>
            <dd><span class="deploy-badge">{{ w.spec.deployStrategy || 'Helm' }}</span></dd>

            <dt>{{ t('suseai.pages.workloads.detail.targetNamespace', 'Target namespace') }}</dt>
            <dd><span class="mono-chip">{{ w.spec.targetNamespace }}</span></dd>

            <dt>{{ t('suseai.pages.workloads.detail.targetClusters', 'Target clusters') }}</dt>
            <dd>
              <ClusterChips
                :clusters="w.spec.targetClusters || []"
                :cluster-info="clusters"
                :show-label="false"
                :clickable="false"
              />
            </dd>
          </dl>
        </section>

        <!-- Blueprint description -->
        <section v-if="description" class="wl-section">
          <h4 class="wl-section-heading">
            {{ t('suseai.pages.workloads.detail.blueprintDescription', 'Blueprint description') }}
          </h4>
          <p class="wl-description">{{ description }}</p>
        </section>

        <!-- Objects being created -->
        <section class="wl-section">
          <h4 class="wl-section-heading">{{ t('suseai.pages.workloads.detail.objects', 'Objects being created') }}</h4>
          <p class="wl-section-hint">{{ t('suseai.pages.workloads.detail.objectsHint', 'Charts this workload deploys to the target cluster(s).') }}</p>

          <table v-if="isBlueprint && components.length" class="wl-table">
            <thead>
              <tr>
                <th>{{ t('suseai.wizard.labels.chart', 'Chart') }}</th>
                <th>{{ t('suseai.common.labels.version', 'Version') }}</th>
                <th>{{ t('suseai.wizard.labels.repository', 'Repository') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="comp in components" :key="comp.chartName">
                <td>{{ comp.chartName }}</td>
                <td class="mono">{{ comp.chartVersion }}</td>
                <td class="mono">{{ comp.chartRepo }}</td>
              </tr>
            </tbody>
          </table>

          <table v-else-if="!isBlueprint && appSource" class="wl-table">
            <thead>
              <tr>
                <th>{{ t('suseai.wizard.labels.chart', 'Chart') }}</th>
                <th>{{ t('suseai.common.labels.version', 'Version') }}</th>
                <th>{{ t('suseai.wizard.labels.repository', 'Repository') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>{{ appSource.chartName }}</td>
                <td class="mono">{{ appSource.chartVersion }}</td>
                <td class="mono">{{ appSource.chartRepo }}</td>
              </tr>
            </tbody>
          </table>

          <p v-else class="wl-muted-note">
            {{ t('suseai.pages.workloads.detail.noBlueprint', 'Blueprint details are not available for this workload.') }}
          </p>
        </section>

        <!-- Per-cluster status -->
        <section v-if="clusterStatuses.length || readyMessage" class="wl-section">
          <h4 class="wl-section-heading">{{ t('suseai.pages.workloads.detail.perClusterStatus', 'Per-cluster status') }}</h4>

          <div v-if="readyMessage" class="wl-ready-message">{{ readyMessage }}</div>

          <table v-if="clusterStatuses.length" class="wl-table">
            <thead>
              <tr>
                <th>{{ t('suseai.common.labels.cluster', 'Cluster') }}</th>
                <th>{{ t('suseai.common.labels.status', 'Status') }}</th>
                <th>{{ t('suseai.pages.workloads.detail.message', 'Message') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="cs in clusterStatuses" :key="cs.clusterId">
                <td class="mono">{{ cs.clusterId }}</td>
                <td>
                  <BadgeState
                    :color="phaseBadgeColor(cs.phase)"
                    :icon="phaseBadgeIcon(cs.phase)"
                    :label="cs.phase"
                  />
                </td>
                <td class="wl-msg-cell">{{ cs.message || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </section>

        <!-- Configuration overrides -->
        <section v-if="overrides.length" class="wl-section">
          <h4 class="wl-section-heading">{{ t('suseai.pages.workloads.detail.componentValues', 'Configuration overrides') }}</h4>
          <div v-for="o in overrides" :key="o.componentName" class="wl-override">
            <button class="wl-override-heading" type="button" @click="toggleOverride(o.componentName)">
              <i
                class="icon icon-chevron-down wl-override-chevron"
                :class="{ 'is-open': openOverrides.has(o.componentName) }"
              />
              <span class="mono">{{ o.componentName }}</span>
            </button>
            <pre v-if="openOverrides.has(o.componentName)" class="wl-yaml">{{ dumpYaml(o.values) }}</pre>
          </div>
        </section>

        <!-- Underlying resources -->
        <section v-if="hasUnderlying" class="wl-section">
          <h4 class="wl-section-heading">{{ t('suseai.pages.workloads.detail.underlyingResources', 'Underlying resources') }}</h4>
          <dl class="wl-dl">
            <template v-if="fleetBundles.length">
              <dt>{{ t('suseai.pages.workloads.detail.fleetBundles', 'Fleet bundles') }}</dt>
              <dd>
                <span v-for="b in fleetBundles" :key="b" class="mono-chip wl-chip-gap">{{ b }}</span>
              </dd>
            </template>
            <template v-if="helmRelease">
              <dt>{{ t('suseai.pages.workloads.detail.helmRelease', 'Helm release') }}</dt>
              <dd><span class="mono-chip">{{ helmRelease }}</span></dd>
            </template>
          </dl>
        </section>
      </div>
    </div>
  </AppModal>
</template>

<style lang="scss" scoped>
.wl-detail {
  display: flex;
  flex-direction: column;
  max-height: 80vh;
}

.wl-detail-header {
  position: relative;
  padding: 20px 24px 14px;
  border-bottom: 1px solid var(--border);

  .wl-detail-title-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .wl-detail-title {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .wl-detail-subrow {
    display: flex;
    gap: 8px;
    margin-top: 8px;
  }

  .wl-detail-close {
    position: absolute;
    top: 14px;
    right: 16px;
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--body-text);
    &:hover { color: var(--primary); }
  }
}

.wl-detail-body {
  padding: 8px 24px 24px;
  overflow-y: auto;
}

.wl-section {
  padding: 16px 0;
  & + .wl-section { border-top: 1px solid var(--border); }
}

.wl-section-heading {
  margin: 0 0 8px;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--muted);
}

.wl-section-hint {
  margin: 0 0 10px;
  font-size: 12px;
  color: var(--muted);
}

.wl-dl {
  display: grid;
  grid-template-columns: 160px 1fr;
  gap: 8px 12px;
  margin: 0;

  dt { color: var(--muted); font-size: 13px; }
  dd { margin: 0; font-size: 13px; color: var(--body-text); display: flex; align-items: center; flex-wrap: wrap; gap: 6px; }
}

.wl-description {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: var(--body-text);
  white-space: pre-wrap;
}

.wl-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;

  th {
    text-align: left;
    padding: 6px 8px;
    border-bottom: 1px solid var(--border);
    color: var(--muted);
    font-weight: 500;
  }

  td {
    padding: 6px 8px;
    border-bottom: 1px solid var(--border);
    color: var(--body-text);
    vertical-align: top;
  }

  tr:last-child td { border-bottom: none; }
}

.wl-msg-cell { color: var(--muted); }

.wl-ready-message {
  font-size: 13px;
  padding: 8px 12px;
  margin-bottom: 10px;
  background: var(--warning-banner-bg);
  border-left: 3px solid var(--warning);
  border-radius: 4px;
  color: var(--body-text);
}

.wl-override + .wl-override { border-top: 1px solid var(--border); }

.wl-override-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 0;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  color: var(--body-text);
}

.wl-override-chevron {
  font-size: 12px;
  color: var(--muted);
  transform: rotate(-90deg);
  transition: transform 0.2s ease;
  &.is-open { transform: rotate(0deg); }
}

.wl-yaml {
  margin: 0 0 8px 26px;
  padding: 10px 12px;
  background: var(--body-bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-size: 12px;
  overflow-x: auto;
  white-space: pre;
}

.wl-muted-note {
  margin: 0;
  font-size: 13px;
  font-style: italic;
  color: var(--muted);
}

.wl-chip-gap { margin-right: 4px; }

// Reused chips/badges (scoped copies matching AIWorkloads.vue).
.mono { font-family: monospace; }

.mono-chip {
  font-family: monospace;
  background: var(--accent-btn);
  padding: 2px 6px;
  border-radius: 3px;
  font-size: 12px;
  border: 1px solid var(--border);
}

.source-type-badge {
  display: inline-block;
  padding: 2px 7px;
  border-radius: 10px;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;

  &.source-app       { background: var(--info-banner-bg); color: var(--info); }
  &.source-blueprint { background: var(--accent-btn); color: var(--body-text); border: 1px solid var(--border); }
}

.deploy-badge {
  display: inline-block;
  padding: 2px 7px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 500;
  background: var(--accent-btn);
  border: 1px solid var(--border);
  color: var(--body-text);
}
</style>
