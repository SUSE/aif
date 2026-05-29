<template>
  <div class="aif-workloads">
    <div class="aif-workloads__header">
      <h1>{{ t('aif.pages.workloads.title') }}</h1>
      <button class="btn btn-sm role-secondary" @click="refresh">
        {{ t('aif.pages.workloads.toolbar.refresh') }}
      </button>
    </div>

    <Banner v-if="error" color="error" :label="t('aif.pages.workloads.empty.error')" />

    <div v-else-if="loading" class="aif-workloads__loading">
      <Loading />
    </div>

    <div v-else-if="workloads.length === 0" class="aif-workloads__empty">
      <p>{{ t('aif.pages.workloads.empty.none') }}</p>
    </div>

    <template v-else>
      <input
        v-model="search"
        type="search"
        class="input"
        :placeholder="t('aif.pages.workloads.toolbar.search')"
      />

      <table class="aif-workloads__table">
        <thead>
          <tr>
            <th>{{ t('aif.pages.workloads.columns.state') }}</th>
            <th>{{ t('aif.pages.workloads.columns.name') }}</th>
            <th>{{ t('aif.pages.workloads.columns.namespace') }}</th>
            <th>{{ t('aif.pages.workloads.columns.source') }}</th>
            <th>{{ t('aif.pages.workloads.columns.deploy') }}</th>
            <th>{{ t('aif.pages.workloads.columns.version') }}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="wl in filteredWorkloads" :key="`${ wl.metadata.namespace }/${ wl.metadata.name }`">
            <td>
              <span :class="`badge badge--${ phaseBadge(wl) }`">{{ phaseLabel(wl) }}</span>
            </td>
            <td>{{ wl.metadata.name }}</td>
            <td><span class="aif-workloads__mono-chip">{{ wl.metadata.namespace }}</span></td>
            <td>
              <span class="badge badge--primary">{{ sourceKind(wl) }}</span>
              {{ sourceName(wl) }}
            </td>
            <td><span class="badge badge--secondary">{{ deployStrategyLabel(wl) }}</span></td>
            <td>{{ sourceVersion(wl) }}</td>
            <td class="aif-workloads__actions">
              <button
                v-if="wl.spec?.source?.kind === 'App'"
                class="btn btn-sm role-secondary"
                :disabled="wl.status?.phase !== 'Running'"
                @click="navigateManage(wl)"
              >
                {{ t('aif.pages.workloads.actions.manage') }}
              </button>
              <button class="btn btn-sm role-danger" @click="confirmDelete(wl)">
                {{ t('aif.pages.workloads.actions.delete') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </template>

    <!-- Delete confirmation modal -->
    <div v-if="deleteTarget" class="aif-workloads__modal-backdrop" @click.self="deleteTarget = null">
      <div class="aif-workloads__modal">
        <h3>{{ t('aif.pages.workloads.deleteModal.title') }}</h3>
        <p>{{ t('aif.pages.workloads.deleteModal.body', { name: deleteTarget.metadata.name }) }}</p>
        <div class="aif-workloads__modal-actions">
          <button class="btn role-secondary" @click="deleteTarget = null">
            {{ t('aif.pages.workloads.deleteModal.cancel') }}
          </button>
          <button class="btn role-danger" :disabled="deleting" @click="doDelete">
            {{ t('aif.pages.workloads.deleteModal.confirm') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import { defineComponent } from 'vue';
import Loading from '@shell/components/Loading';
import { Banner } from '@components/Banner';
import { listWorkloads, deleteWorkload } from '../utils/operator-api';
import { PRODUCT_NAME, MANAGEMENT_CLUSTER } from '../config/types';

export default defineComponent({
  name: 'WorkloadsPage',

  components: { Loading, Banner },

  async fetch() {
    await this.loadWorkloads();
  },

  data() {
    return {
      workloads:    [],
      loading:      false,
      error:        null,
      deleteTarget: null,
      deleting:     false,
      search:       '',
      _timer:       null,
    };
  },

  computed: {
    filteredWorkloads() {
      const q = (this.search || '').toLowerCase();
      if (!q) return this.workloads;
      return this.workloads.filter((wl) => (
        wl.metadata.name.toLowerCase().includes(q) ||
        wl.metadata.namespace.toLowerCase().includes(q) ||
        (wl.spec?.source?.kind || '').toLowerCase().includes(q)
      ));
    },
  },

  mounted() {
    // Background poll is silent (see silentRefresh) — no spinner, no error flash,
    // matching the reference AIWorkloads.vue silentRefresh.
    this._timer = setInterval(this.silentRefresh.bind(this), 10 * 1000);
  },

  beforeUnmount() {
    clearInterval(this._timer);
  },

  methods: {
    async loadWorkloads() {
      this.loading = this.workloads.length === 0;
      this.error = null;
      try {
        this.workloads = await listWorkloads();
      } catch (e) {
        this.error = e;
      } finally {
        this.loading = false;
      }
    },

    // 10s background poll: refresh rows but keep the last good data on a
    // transient failure — never surface the error banner or a spinner mid-poll.
    async silentRefresh() {
      if (this.loading) {
        return;
      }
      try {
        this.workloads = await listWorkloads();
      } catch (e) {
        /* swallow — user can hit Refresh if needed */
      }
    },

    async refresh() {
      await this.loadWorkloads();
    },

    confirmDelete(wl) {
      this.deleteTarget = wl;
    },

    async doDelete() {
      if (!this.deleteTarget) {
        return;
      }
      this.deleting = true;
      try {
        await deleteWorkload(this.deleteTarget.metadata.namespace, this.deleteTarget.metadata.name);
        this.deleteTarget = null;
        await this.loadWorkloads();
      } catch (e) {
        this.error = e;
      } finally {
        this.deleting = false;
      }
    },

    phaseBadge(wl) {
      // Matches the reference phaseBadgeColor mapping.
      switch (wl.status?.phase) {
        case 'Running':  return 'success';
        case 'Degraded': return 'warning';
        case 'Failed':   return 'error';
        default:         return 'info';
      }
    },

    phaseLabel(wl) {
      const phase = wl.status?.phase || 'Unknown';
      const key   = `aif.pages.workloads.phase.${ phase.toLowerCase() }`;
      return this.t(key, undefined, true) || phase;
    },

    sourceKind(wl) {
      return wl.spec?.source?.kind || 'Unknown';
    },

    sourceName(wl) {
      const src = wl.spec?.source;
      if (!src) {
        return '';
      }
      if (src.app) {
        return src.app.chart;
      }
      if (src.blueprint) {
        return src.blueprint.name;
      }
      return '';
    },

    deployStrategyLabel(wl) {
      const strategy = wl.spec?.deployStrategy || 'helm';
      const key     = `aif.pages.workloads.deployStrategy.${ strategy }`;
      return this.t(key, undefined, true) || strategy;
    },

    sourceVersion(wl) {
      const src = wl.spec?.source;
      if (!src) {
        return '';
      }
      if (src.app) {
        return src.app.version;
      }
      if (src.blueprint) {
        return src.blueprint.version;
      }
      return '';
    },

    navigateManage(wl) {
      this.$router.push({
        name:   `${ PRODUCT_NAME }-c-cluster-workload-manage`,
        params: { cluster: MANAGEMENT_CLUSTER, ns: wl.metadata.namespace, name: wl.metadata.name },
      });
    },
  },
});
</script>

<style scoped>
.aif-workloads__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.aif-workloads__table {
  width: 100%;
  border-collapse: collapse;
}

.aif-workloads__table th,
.aif-workloads__table td {
  padding: 8px 12px;
  text-align: left;
  border-bottom: 1px solid var(--border);
}

.aif-workloads__modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.aif-workloads__modal {
  background: var(--body-bg);
  border-radius: 4px;
  padding: 24px;
  max-width: 480px;
  width: 100%;
}

.aif-workloads__modal-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 16px;
}

.aif-workloads__actions {
  display: flex;
  gap: 6px;
}

.aif-workloads__mono-chip {
  font-family: monospace;
  background: var(--accent-btn);
  padding: 2px 6px;
  border-radius: 3px;
  font-size: 12px;
  border: 1px solid var(--border);
}
</style>
