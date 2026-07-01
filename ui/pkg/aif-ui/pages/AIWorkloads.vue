<script lang="ts" setup>
import { ref, computed, onMounted, onUnmounted, reactive, getCurrentInstance } from 'vue';
import { Banner } from '@components/Banner';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import AppModal from '@shell/components/AppModal';
import { BadgeState } from '@components/BadgeState';
import { listAIWorkloads, deleteAIWorkload, updateAIWorkload } from '../utils/operator-api';
import { listBlueprints, groupBlueprintsByFamily } from '../utils/blueprint-api';
import { checkOperatorConnection, getConnectionError } from '../utils/operator-config';
import OperatorErrorBanner from '../components/OperatorErrorBanner.vue';
import type { AIWorkload, AIWorkloadPhase } from '../types/aiworkload-types';
import type { Blueprint } from '../types/blueprint-types';
import { PRODUCT } from '../config/suseai';
import ClusterChips from '../formatters/ClusterChips.vue';
import { getClusters } from '../services/cluster-service';
import type { ClusterInfo } from '../types/rancher-types';

const vm      = getCurrentInstance()!.proxy as any;
const router  = vm.$router;
const route   = vm.$route;
const cluster = (route?.params?.cluster as string) || '_';

const loading       = ref(true);
const error         = ref<string | null>(null);
const operatorError = ref<string | null>(null);

const search     = ref('');
const sortBy     = ref('name-asc');
const workloads  = ref<AIWorkload[]>([]);
const blueprints = ref<Blueprint[]>([]);
const clusters   = ref<ClusterInfo[]>([]);

// ── Delete modal ───────────────────────────────────────────────────────────────
const deleteModal = reactive({
  show:      false,
  name:      '',
  namespace: '',
  display:   '',
  deleting:  false,
});

// ── Blueprint upgrade modal ────────────────────────────────────────────────────
const upgradeModal = reactive({
  show:          false,
  workload:      null as AIWorkload | null,
  selectedVersion: '',
  upgrading:     false,
});

const upgradeVersionOptions = computed(() => {
  if (!upgradeModal.workload) return [];
  const family = upgradeModal.workload.spec.source.blueprint?.name || '';
  const families = groupBlueprintsByFamily(blueprints.value);
  const versions = families.get(family) || [];
  return versions.map(bp => bp.spec.version);
});

const upgradeVersionSelectOptions = computed(() =>
  upgradeVersionOptions.value.map(v => ({
    label: `v${ v }${ v === upgradeModal.workload?.spec.source.blueprint?.version ? ' (current)' : '' }`,
    value: v,
  })),
);

// ── Filtering ──────────────────────────────────────────────────────────────────
const PHASE_ORDER: Record<string, number> = { Running: 0, Degraded: 1, Failed: 2, Pending: 3 };

const filteredWorkloads = computed(() => {
  const q = search.value.toLowerCase();
  const list = q
    ? workloads.value.filter(w =>
        w.metadata.name.toLowerCase().includes(q) ||
        w.spec.displayName?.toLowerCase().includes(q) ||
        w.metadata.namespace.toLowerCase().includes(q) ||
        w.spec.source.sourceType.toLowerCase().includes(q),
      )
    : [...workloads.value];
  const key = sortBy.value;
  return list.sort((a, b) => {
    switch (key) {
      case 'name-desc':
        return b.metadata.name.localeCompare(a.metadata.name);
      case 'status':
        return (PHASE_ORDER[a.status?.phase || 'Pending'] ?? 4) - (PHASE_ORDER[b.status?.phase || 'Pending'] ?? 4)
          || a.metadata.name.localeCompare(b.metadata.name);
      case 'source':
        return a.spec.source.sourceType.localeCompare(b.spec.source.sourceType)
          || a.metadata.name.localeCompare(b.metadata.name);
      default:
        return a.metadata.name.localeCompare(b.metadata.name);
    }
  });
});

// ── Helpers ────────────────────────────────────────────────────────────────────
function phaseBadgeColor(phase: AIWorkloadPhase | undefined): string {
  switch (phase) {
    case 'Running':  return 'bg-success';
    case 'Degraded': return 'bg-warning';
    case 'Failed':   return 'bg-error';
    default:         return 'bg-info';
  }
}

function phaseBadgeIcon(phase: AIWorkloadPhase | undefined): string {
  switch (phase) {
    case 'Running':  return 'icon-checkmark';
    case 'Degraded': return 'icon-warning';
    case 'Failed':   return 'icon-x';
    default:         return 'icon-info';
  }
}

function workloadVersion(w: AIWorkload): string {
  if (w.spec.source.sourceType === 'App') return w.spec.source.app?.chartVersion || '—';
  return w.spec.source.blueprint?.version || '—';
}

function workloadSource(w: AIWorkload): string {
  if (w.spec.source.sourceType === 'App') {
    return w.spec.source.app?.chartName || '—';
  }
  return w.spec.source.blueprint?.name || '—';
}

// ── Data loading ───────────────────────────────────────────────────────────────
async function refresh() {
  loading.value = true;
  error.value   = null;
  await checkOperatorConnection();
  operatorError.value = getConnectionError();
  if (operatorError.value) {
    loading.value = false;
    return;
  }
  try {
    const [wlResult, bpResult, clResult] = await Promise.all([
      listAIWorkloads(),
      listBlueprints().catch(() => ({ items: [] as Blueprint[] })),
      getClusters(vm.$store).catch(() => [] as ClusterInfo[]),
    ]);
    workloads.value  = wlResult.items || [];
    blueprints.value = bpResult.items || [];
    clusters.value   = clResult;
  } catch (e: any) {
    error.value = e?.message || 'Failed to load workloads';
  } finally {
    loading.value = false;
  }
}

async function retryConnection() {
  loading.value = true;
  await checkOperatorConnection(true);
  operatorError.value = getConnectionError();
  if (!operatorError.value) await refresh();
  else loading.value = false;
}

async function silentRefresh() {
  if (loading.value) return;
  try {
    const wlResult = await listAIWorkloads();
    workloads.value = wlResult.items || [];
  } catch {
    // silently ignore — user can use the Refresh button if needed
  }
}

let pollTimer: ReturnType<typeof setInterval> | null = null;

onMounted(() => {
  refresh();
  pollTimer = setInterval(silentRefresh, 10_000);
});

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer);
});

// ── Delete ─────────────────────────────────────────────────────────────────────
function openDeleteModal(w: AIWorkload) {
  deleteModal.name      = w.metadata.name;
  deleteModal.namespace = w.metadata.namespace;
  deleteModal.display   = w.spec.displayName || w.metadata.name;
  deleteModal.show      = true;
}

async function executeDelete() {
  deleteModal.deleting = true;
  try {
    await deleteAIWorkload(deleteModal.namespace, deleteModal.name);
    workloads.value = workloads.value.filter(
      w => !(w.metadata.name === deleteModal.name && w.metadata.namespace === deleteModal.namespace),
    );
    deleteModal.show = false;
  } catch (e: any) {
    error.value = e?.message || 'Failed to delete workload';
    deleteModal.show = false;
  } finally {
    deleteModal.deleting = false;
  }
}

// ── Manage (App) ───────────────────────────────────────────────────────────────
function onManage(w: AIWorkload) {
  const slug = w.spec.source.app?.chartName || '';
  router.push({
    name:   `c-cluster-${ PRODUCT }-manage`,
    params: { cluster, slug },
    query:  {
      instanceName:      w.metadata.name,
      instanceNamespace: w.metadata.namespace,
      instanceCluster:   w.spec.targetClusters?.[0] || 'local',
      deployStrategy:    w.spec.deployStrategy || 'Helm',
    },
  });
}

// ── Upgrade (Blueprint) ────────────────────────────────────────────────────────
function openUpgradeModal(w: AIWorkload) {
  upgradeModal.workload         = w;
  upgradeModal.selectedVersion  = w.spec.source.blueprint?.version || '';
  upgradeModal.upgrading        = false;
  upgradeModal.show             = true;
}

async function executeUpgrade() {
  if (!upgradeModal.workload) return;
  const w = upgradeModal.workload;
  upgradeModal.upgrading = true;
  error.value = null;
  try {
    const newSpec = {
      ...w.spec,
      source: {
        ...w.spec.source,
        blueprint: {
          ...w.spec.source.blueprint!,
          version: upgradeModal.selectedVersion,
        },
      },
    };
    await updateAIWorkload(w.metadata.namespace, w.metadata.name, newSpec);
    upgradeModal.show = false;
    await refresh();
  } catch (e: any) {
    error.value = e?.message || 'Failed to upgrade workload';
    upgradeModal.show = false;
  } finally {
    upgradeModal.upgrading = false;
  }
}
</script>

<template>
  <main class="main-layout">
    <div class="outlet">
      <header class="fixed-header">
        <h1>Workloads</h1>
        <div class="actions-container">
          <div class="search-box">
            <input
              v-model="search"
              type="search"
              placeholder="Search workloads"
              class="input-sm"
            />
          </div>
          <select v-model="sortBy" class="sort-select form-control-sm" aria-label="Sort workloads">
            <option value="name-asc">Name (A → Z)</option>
            <option value="name-desc">Name (Z → A)</option>
            <option value="status">Status (healthy first)</option>
            <option value="source">Source (App, Blueprint)</option>
          </select>
          <button class="btn role-secondary ml-auto" @click="refresh" :disabled="loading" type="button">
            <i v-if="loading" class="icon icon-spinner icon-spin" />
            <i v-else class="icon icon-refresh" />
            Refresh
          </button>
        </div>
      </header>

      <OperatorErrorBanner v-if="operatorError" :operator-error="operatorError" @retry="retryConnection" />

      <Banner v-if="error" color="error" class="mb-20">{{ error }}</Banner>

      <div class="main-content">
        <!-- Loading state -->
        <div v-if="loading" class="loading-row">
          <i class="icon icon-spinner icon-spin" /> Loading workloads...
        </div>

        <!-- Empty state -->
        <div v-else-if="!filteredWorkloads.length && !error && !operatorError" class="empty-state-content">
          <i class="icon icon-folder-open icon-4x text-muted" />
          <h3>No workloads found</h3>
          <p class="text-muted">Deploy an App or install a Blueprint to see workloads here.</p>
        </div>

        <!-- Workloads table -->
        <div v-else class="workloads-table">
          <table class="table" role="table" aria-label="AI Workloads">
            <thead>
              <tr>
                <th>State</th>
                <th>Name</th>
                <th>Namespace</th>
                <th>Cluster</th>
                <th>Source</th>
                <th>Deploy</th>
                <th>Version</th>
                <th class="text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="w in filteredWorkloads"
                :key="`${ w.metadata.namespace }/${ w.metadata.name }`"
                class="workload-row"
              >
                <!-- State -->
                <td class="col-state">
                  <BadgeState
                    :color="phaseBadgeColor(w.status?.phase)"
                    :icon="phaseBadgeIcon(w.status?.phase)"
                    :label="w.status?.phase || 'Pending'"
                  />
                </td>

                <!-- Name -->
                <td class="col-name">
                  <div class="name-primary">{{ w.metadata.name }}</div>
                </td>

                <!-- Namespace -->
                <td class="col-namespace">
                  <span class="mono-chip">{{ w.metadata.namespace }}</span>
                </td>

                <!-- Cluster -->
                <td class="col-cluster">
                  <ClusterChips
                    :clusters="w.spec.targetClusters || []"
                    :cluster-info="clusters"
                    :show-label="false"
                    :clickable="false"
                  />
                </td>

                <!-- Source -->
                <td class="col-source">
                  <span class="source-type-badge" :class="w.spec.source.sourceType === 'App' ? 'source-app' : 'source-blueprint'">
                    {{ w.spec.source.sourceType }}
                  </span>
                  <div class="source-name">{{ workloadSource(w) }}{{ workloadVersion(w) !== '—' ? '-' + workloadVersion(w) : '' }}</div>
                </td>

                <!-- Deploy strategy -->
                <td class="col-deploy">
                  <span class="deploy-badge">{{ w.spec.deployStrategy || 'Helm' }}</span>
                </td>

                <!-- Version -->
                <td class="col-version">
                  {{ workloadVersion(w) }}
                </td>

                <!-- Actions -->
                <td class="col-actions text-right">
                  <div class="btn-group">
                    <!-- App workload: Manage -->
                    <button
                      v-if="w.spec.source.sourceType === 'App'"
                      class="btn btn-sm role-secondary"
                      :disabled="w.status?.phase !== 'Running'"
                      @click="onManage(w)"
                      type="button"
                    >
                      <i class="icon icon-edit" />
                      Manage
                    </button>

                    <!-- Blueprint workload: Upgrade -->
                    <button
                      v-else
                      class="btn btn-sm role-secondary"
                      :disabled="w.status?.phase !== 'Running'"
                      @click="openUpgradeModal(w)"
                      type="button"
                    >
                      <i class="icon icon-upload" />
                      Upgrade
                    </button>

                    <!-- Delete (both types) -->
                    <button
                      class="btn btn-sm role-secondary text-error"
                      @click="openDeleteModal(w)"
                      type="button"
                    >
                      <i class="icon icon-delete" />
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Delete confirmation modal -->
    <AppModal v-if="deleteModal.show" :click-to-close="true" :width="480" @close="deleteModal.show = false">
      <div class="modal-body">
        <h3>Delete Workload</h3>
        <p>
          Delete <strong>{{ deleteModal.display }}</strong> from namespace
          <code>{{ deleteModal.namespace }}</code>?
        </p>
        <p class="text-muted modal-warning">
          All associated resources on the target clusters — Fleet bundles, HelmOps,
          and Helm releases — will be removed. This action cannot be undone.
        </p>
        <div class="modal-buttons">
          <button class="btn role-secondary" @click="deleteModal.show = false" type="button">Cancel</button>
          <button
            class="btn role-primary btn-danger"
            @click="executeDelete"
            :disabled="deleteModal.deleting"
            type="button"
          >
            <i v-if="deleteModal.deleting" class="icon icon-spinner icon-spin" />
            Delete
          </button>
        </div>
      </div>
    </AppModal>

    <!-- Blueprint upgrade modal -->
    <AppModal v-if="upgradeModal.show" :click-to-close="true" :width="480" @close="upgradeModal.show = false">
      <div class="modal-body">
        <h3>Upgrade Blueprint Workload</h3>
        <p>
          <strong>{{ upgradeModal.workload?.spec.displayName }}</strong>
        </p>

        <div class="field-row">
          <label class="field-label">Current version</label>
          <span class="field-value">v{{ upgradeModal.workload?.spec.source.blueprint?.version }}</span>
        </div>

        <div class="field-row">
          <LabeledSelect
            v-model:value="upgradeModal.selectedVersion"
            label="Target version"
            :options="upgradeVersionSelectOptions"
            :clearable="false"
          />
        </div>

        <div class="modal-buttons">
          <button class="btn role-secondary" @click="upgradeModal.show = false" type="button">Cancel</button>
          <button
            class="btn role-primary"
            @click="executeUpgrade"
            :disabled="upgradeModal.upgrading || upgradeModal.selectedVersion === upgradeModal.workload?.spec.source.blueprint?.version"
            type="button"
          >
            <i v-if="upgradeModal.upgrading" class="icon icon-spinner icon-spin" />
            Upgrade
          </button>
        </div>
      </div>
    </AppModal>
  </main>
</template>

<style lang="scss" scoped>
.fixed-header {
  margin-bottom: 30px;

  h1 { margin: 0 0 16px; }

  .actions-container {
    display: flex;
    align-items: center;
    gap: 12px;

    .search-box .input-sm {
      width: 240px;
      height: 32px;
      padding: 0 12px;
      border: 1px solid var(--border);
      border-radius: var(--border-radius);
      background: var(--input-bg);
      color: var(--body-text);
      font-size: 14px;
    }

    .sort-select {
      height: 30px;
      padding: 0 6px 0 8px;
      border: 1px solid var(--border);
      border-radius: var(--border-radius);
      background: var(--input-bg);
      color: var(--body-text);
      font-size: 13px;
      width: auto;
    }
    .ml-auto { margin-left: auto; }
  }
}

.loading-row {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  padding: 40px 0;
  font-size: 14px;
}

.workloads-table {
  .table {
    width: 100%;
    border-collapse: collapse;
    background: var(--body-bg);
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;

    th {
      background: var(--sortable-table-header-bg);
      color: var(--body-text);
      padding: 12px;
      text-align: left;
      font-weight: 600;
      font-size: 13px;
      border-bottom: 1px solid var(--border);

      &.text-right { text-align: right; }
    }

    td {
      padding: 12px;
      border-bottom: 1px solid var(--border);
      vertical-align: middle;

      &.text-right { text-align: right; }
    }

    tr:last-child td { border-bottom: none; }

    .workload-row {
      transition: background-color 0.15s ease;
      &:hover { background: var(--sortable-table-accent-bg); }
    }
  }
}


// Name column
.col-name {
  .name-primary { font-weight: 600; color: var(--body-text); }
}

// Source column
.col-source {
  .source-type-badge {
    display: inline-block;
    padding: 2px 7px;
    border-radius: 10px;
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    margin-bottom: 3px;

    &.source-app       { background: var(--info-banner-bg);    color: var(--info);    }
    &.source-blueprint { background: var(--accent-btn);        color: var(--body-text); border: 1px solid var(--border); }
  }

  .source-name { font-size: 12px; color: var(--muted); font-family: monospace; }
}

// Mono chip (namespace)
.mono-chip {
  font-family: monospace;
  background: var(--accent-btn);
  padding: 2px 6px;
  border-radius: 3px;
  font-size: 12px;
  border: 1px solid var(--border);
}

// Deploy badge
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

// Actions
.btn-group {
  display: flex;
  gap: 4px;
  justify-content: flex-end;
}

// Modal
.modal-body {
  padding: 24px;

  h3 { margin: 0 0 16px; font-size: 18px; font-weight: 600; }

  p { margin: 0 0 12px; }

  .modal-warning {
    font-size: 13px;
    padding: 10px 12px;
    background: var(--warning-banner-bg);
    border-radius: 4px;
    border-left: 3px solid var(--warning);
  }

  .modal-buttons {
    display: flex;
    gap: 12px;
    justify-content: flex-end;
    margin-top: 20px;
  }
}

.field-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;

  .field-label { font-weight: 500; font-size: 14px; min-width: 120px; }
  .field-value { font-family: monospace; font-size: 14px; color: var(--muted); }
}


// Empty state
.empty-state-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 60px 20px;

  .icon-4x { font-size: 64px; opacity: 0.5; margin-bottom: 20px; }
  h3 { margin: 0 0 12px; font-size: 20px; }
  p  { color: var(--muted); }
}

.mb-20 { margin-bottom: 20px; }
.ml-5  { margin-left: 5px; }
.text-muted { color: var(--muted); }

.text-right { text-align: right; }

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0 14px;
  height: 32px;
  border-radius: 6px;
  font-weight: 500;
  font-size: 13px;
  cursor: pointer;
  border: 1px solid;
  transition: all 0.15s ease;

  &.btn-sm { height: 28px; padding: 0 12px; font-size: 12px; }

  &.role-primary {
    background: var(--primary);
    border-color: var(--primary);
    color: var(--primary-text);

    &.btn-danger { background: var(--error); border-color: var(--error); }
    &:disabled   { opacity: 0.6; cursor: not-allowed; }
  }

  &.role-secondary {
    background: var(--body-bg);
    border-color: var(--border);
    color: var(--body-text);

    &.text-error { color: var(--error); }
    &:disabled   { opacity: 0.6; cursor: not-allowed; }
  }

  .icon-spin { animation: spin 1s linear infinite; }
}

@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
</style>
