<script lang="ts" setup>
import { ref, computed, onMounted, onUnmounted, getCurrentInstance } from 'vue';
import { Banner }    from '@components/Banner';
import { BadgeState } from '@components/BadgeState';
import CountBox      from '@shell/components/CountBox';
import Loading       from '@shell/components/Loading';
import { checkOperatorConnection, getConnectionError } from '../utils/operator-config';
import OperatorErrorBanner from '../components/OperatorErrorBanner.vue';
import { listAIWorkloads }           from '../utils/operator-api';
import { listBlueprints, groupBlueprintsByFamily, latestVersion } from '../utils/blueprint-api';
import type { AIWorkload, AIWorkloadPhase } from '../types/aiworkload-types';
import type { Blueprint }                   from '../types/blueprint-types';
import { PRODUCT, PAGE_TYPES }              from '../config/suseai';
import ClusterChips from '../formatters/ClusterChips.vue';
import { getClusters } from '../services/cluster-service';
import type { ClusterInfo } from '../types/rancher-types';

const vm      = getCurrentInstance()!.proxy as any;
const router  = vm.$router;
const route   = vm.$route;
const cluster = (route?.params?.cluster as string) || '_';

const loading         = ref(true);
const error           = ref<string | null>(null);
const operatorError   = ref<string | null>(null);
const workloads  = ref<AIWorkload[]>([]);
const blueprints = ref<Blueprint[]>([]);
const clusters   = ref<ClusterInfo[]>([]);

// ── Computed stats ─────────────────────────────────────────────────────────────
const totalWorkloads   = computed(() => workloads.value.length);
const runningWorkloads = computed(() => workloads.value.filter(w => w.status?.phase === 'Running').length);
const degradedWorkloads = computed(() => workloads.value.filter(w => w.status?.phase === 'Degraded').length);
const failedWorkloads  = computed(() => workloads.value.filter(w => w.status?.phase === 'Failed').length);
const issueWorkloads   = computed(() => degradedWorkloads.value + failedWorkloads.value);

const activeBlueprintFamilies = computed(() => {
  const families = groupBlueprintsByFamily(blueprints.value);
  let count = 0;
  for (const versions of families.values()) {
    if (versions.some(bp => !bp.spec.deprecated)) count++;
  }
  return count;
});

// Most recent 5 workloads for the activity feed
const recentWorkloads = computed(() => workloads.value.slice(0, 5));

// Active (non-deprecated) blueprint families with their latest version
const activeBlueprintList = computed(() => {
  const families = groupBlueprintsByFamily(blueprints.value);
  const result: { family: string; latest: Blueprint }[] = [];
  for (const [family, versions] of families.entries()) {
    const active = versions.filter(bp => !bp.spec.deprecated);
    if (active.length > 0) {
      result.push({ family, latest: latestVersion(active) });
    }
  }
  return result.slice(0, 5);
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

function workloadSourceLabel(w: AIWorkload): string {
  if (w.spec.source.sourceType === 'App') return w.spec.source.app?.chartName || '—';
  return `${ w.spec.source.blueprint?.name || '—' } v${ w.spec.source.blueprint?.version || '' }`;
}

// ── Navigation ─────────────────────────────────────────────────────────────────
function goTo(pageType: string) {
  router.push({ name: `c-cluster-${ PRODUCT }-${ pageType }`, params: { cluster } });
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
    error.value = e?.message || 'Failed to load overview data';
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
  } catch { /* ignore */ }
}

let pollTimer: ReturnType<typeof setInterval> | null = null;

onMounted(() => {
  refresh();
  pollTimer = setInterval(silentRefresh, 10_000);
});

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer);
});
</script>

<template>
  <main class="main-layout">
    <div class="outlet">
      <header class="page-header">
        <h1>Overview</h1>
        <button
          class="btn role-secondary"
          :disabled="loading"
          type="button"
          @click="refresh"
        >
          <i v-if="loading" class="icon icon-spinner icon-spin" />
          <i v-else class="icon icon-refresh" />
          Refresh
        </button>
      </header>

      <OperatorErrorBanner v-if="operatorError" :operator-error="operatorError" @retry="retryConnection" />

      <Banner v-if="error" color="error" class="mb-20">{{ error }}</Banner>

      <Loading v-if="loading" />

      <template v-else-if="!operatorError">
        <!-- ── Summary cards ─────────────────────────────────────────────── -->
        <section class="summary-grid">
          <CountBox
            name="Total Workloads"
            :count="totalWorkloads"
            primary-color-var="--sizzle-info"
            :clickable="true"
            @click="goTo(PAGE_TYPES.WORKLOADS)"
          />
          <CountBox
            name="Running"
            :count="runningWorkloads"
            primary-color-var="--sizzle-success"
            :clickable="true"
            @click="goTo(PAGE_TYPES.WORKLOADS)"
          />
          <CountBox
            name="With Issues"
            :count="issueWorkloads"
            primary-color-var="--sizzle-error"
            :clickable="true"
            @click="goTo(PAGE_TYPES.WORKLOADS)"
          />
          <CountBox
            name="Active Blueprints"
            :count="activeBlueprintFamilies"
            primary-color-var="--sizzle-3"
            :clickable="true"
            @click="goTo(PAGE_TYPES.BLUEPRINTS)"
          />
        </section>

        <!-- ── Two-column lower section ──────────────────────────────────── -->
        <div class="lower-grid">
          <!-- Recent Workloads -->
          <section class="panel">
            <div class="panel-header">
              <h3>Recent Workloads</h3>
              <button class="btn-link" type="button" @click="goTo(PAGE_TYPES.WORKLOADS)">
                View all <i class="icon icon-chevron-right" />
              </button>
            </div>

            <div v-if="!recentWorkloads.length" class="panel-empty">
              <i class="icon icon-folder-open" />
              No workloads deployed yet.
            </div>

            <table v-else class="overview-table">
              <thead>
                <tr>
                  <th>State</th>
                  <th>Name</th>
                  <th>Source</th>
                  <th>Cluster</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="w in recentWorkloads"
                  :key="`${ w.metadata.namespace }/${ w.metadata.name }`"
                >
                  <td>
                    <BadgeState
                      :color="phaseBadgeColor(w.status?.phase)"
                      :icon="phaseBadgeIcon(w.status?.phase)"
                      :label="w.status?.phase || 'Pending'"
                    />
                  </td>
                  <td class="col-name">{{ w.spec.displayName || w.metadata.name }}</td>
                  <td class="col-source">{{ workloadSourceLabel(w) }}</td>
                  <td class="col-cluster">
                    <ClusterChips
                      :clusters="w.spec.targetClusters || []"
                      :cluster-info="clusters"
                      :show-label="false"
                      :clickable="false"
                    />
                  </td>
                </tr>
              </tbody>
            </table>
          </section>

          <!-- Active Blueprints -->
          <section class="panel">
            <div class="panel-header">
              <h3>Active Blueprints</h3>
              <button class="btn-link" type="button" @click="goTo(PAGE_TYPES.BLUEPRINTS)">
                View all <i class="icon icon-chevron-right" />
              </button>
            </div>

            <div v-if="!activeBlueprintList.length" class="panel-empty">
              <i class="icon icon-document" />
              No blueprints defined yet.
            </div>

            <table v-else class="overview-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Latest Version</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="item in activeBlueprintList"
                  :key="item.family"
                >
                  <td class="col-name">{{ item.latest.spec.displayName }}</td>
                  <td>
                    <span class="version-chip">v{{ item.latest.spec.version }}</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </section>
        </div>

        <!-- ── Quick actions ──────────────────────────────────────────────── -->
        <section class="quick-actions">
          <h3>Quick Actions</h3>
          <div class="action-cards">
            <button
              class="action-card"
              type="button"
              @click="goTo(PAGE_TYPES.APPS)"
            >
              <i class="icon icon-apps icon-2x" />
              <span>Browse Apps</span>
            </button>
            <button
              class="action-card"
              type="button"
              @click="goTo(PAGE_TYPES.BLUEPRINTS)"
            >
              <i class="icon icon-document icon-2x" />
              <span>Manage Blueprints</span>
            </button>
            <button
              class="action-card"
              type="button"
              @click="goTo(PAGE_TYPES.WORKLOADS)"
            >
              <i class="icon icon-list-flat icon-2x" />
              <span>View Workloads</span>
            </button>
          </div>
        </section>
      </template>
    </div>
  </main>
</template>

<style lang="scss" scoped>
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 24px;

  h1 { margin: 0; }
}

// ── Summary cards ──────────────────────────────────────────────────────────────
.summary-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 24px;

  @media (max-width: 900px) {
    grid-template-columns: repeat(2, 1fr);
  }
}

// ── Lower two-column section ──────────────────────────────────────────────────
.lower-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-bottom: 24px;

  @media (max-width: 800px) {
    grid-template-columns: 1fr;
  }
}

.panel {
  background: var(--body-bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 16px;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;

  h3 { margin: 0; font-size: 15px; font-weight: 600; }
}

.panel-empty {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 14px;
  padding: 20px 0;

  .icon { font-size: 20px; opacity: 0.5; }
}

.btn-link {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  background: none;
  border: none;
  color: var(--primary);
  font-size: 13px;
  cursor: pointer;
  padding: 0;

  &:hover { text-decoration: underline; }
}

// ── Overview tables ────────────────────────────────────────────────────────────
.overview-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;

  th {
    text-align: left;
    padding: 6px 8px;
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
    border-bottom: 1px solid var(--border);
  }

  td {
    padding: 8px;
    border-bottom: 1px solid var(--border);
    vertical-align: middle;
  }

  tr:last-child td { border-bottom: none; }

  tr:hover td { background: var(--sortable-table-accent-bg); }

  .col-name   { font-weight: 500; color: var(--body-text); }
  .col-source { color: var(--muted); font-size: 12px; font-family: monospace; }
}

.version-chip {
  display: inline-block;
  padding: 2px 7px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 500;
  background: var(--accent-btn);
  border: 1px solid var(--border);
  color: var(--body-text);
  font-family: monospace;
}

// ── Quick actions ──────────────────────────────────────────────────────────────
.quick-actions {
  h3 { margin: 0 0 12px; font-size: 15px; font-weight: 600; }
}

.action-cards {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.action-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 20px 32px;
  background: var(--body-bg);
  border: 1px solid var(--border);
  border-radius: 8px;
  cursor: pointer;
  color: var(--body-text);
  font-size: 13px;
  font-weight: 500;
  transition: border-color 0.15s, background 0.15s;

  .icon { opacity: 0.7; }

  &:hover {
    border-color: var(--primary);
    background: var(--sortable-table-accent-bg);

    .icon { opacity: 1; color: var(--primary); }
  }
}

// ── Shared ─────────────────────────────────────────────────────────────────────
.mb-20 { margin-bottom: 20px; }
.ml-10 { margin-left: 10px; }

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

  &.role-secondary {
    background: var(--body-bg);
    border-color: var(--border);
    color: var(--body-text);

    &:disabled { opacity: 0.6; cursor: not-allowed; }
  }

  .icon-spin { animation: spin 1s linear infinite; }
}

@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
</style>
