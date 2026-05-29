<template>
  <div class="aif-workloads">
    <div class="aif-workloads__header">
      <h1>{{ t('aif.pages.workloads.title') }}</h1>
      <button class="btn btn-sm role-secondary" @click="refresh">
        {{ t('aif.pages.workloads.toolbar.refresh') }}
      </button>
    </div>

    <Banner v-if="loadError" color="error" :label="t('aif.pages.workloads.empty.error')" />
    <Banner v-if="actionError" color="error" :label="actionError.message || t('aif.pages.workloads.actionFailedGeneric')" />

    <div v-if="loading" class="aif-workloads__loading">
      <Loading />
    </div>

    <!-- loadError gates the empty/data branch so a failed initial load
         doesn't render an empty table; an action failure leaves the
         previously-loaded data intact and surfaces actionError above. -->
    <div v-else-if="!loadError && workloads.length === 0" class="aif-workloads__empty">
      <p>{{ t('aif.pages.workloads.empty.none') }}</p>
    </div>

    <template v-else-if="!loadError">
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
              <button
                v-else
                class="btn btn-sm role-secondary"
                :disabled="wl.status?.phase !== 'Running' || !hasUpgradeCandidates(wl)"
                :title="hasUpgradeCandidates(wl) ? '' : t('aif.pages.workloads.actions.noUpgradeAvailable')"
                @click="confirmUpgrade(wl)"
              >
                {{ t('aif.pages.workloads.actions.upgrade') }}
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

    <!-- Upgrade version modal -->
    <div v-if="upgradeTarget" class="aif-workloads__modal-backdrop" @click.self="upgradeTarget = null">
      <div class="aif-workloads__modal">
        <h3>{{ t('aif.pages.workloads.upgradeModal.title') }}</h3>
        <p>{{ t('aif.pages.workloads.upgradeModal.body', { name: upgradeTarget.metadata.name }) }}</p>
        <label>
          {{ t('aif.pages.workloads.upgradeModal.selectVersion') }}
          <select v-model="upgradeSelectedVersion" class="select">
            <option v-for="v in availableVersions" :key="v.version" :value="v.version">{{ optionLabel(v) }}</option>
          </select>
        </label>
        <div class="aif-workloads__modal-actions">
          <button class="btn role-secondary" @click="upgradeTarget = null">
            {{ t('aif.pages.workloads.upgradeModal.cancel') }}
          </button>
          <button class="btn role-primary" :disabled="upgrading" @click="doUpgrade">
            {{ t('aif.pages.workloads.upgradeModal.confirm') }}
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
import { listWorkloads, deleteWorkload, upgradeWorkload } from '../utils/operator-api';
import { compareVersions } from '../utils/blueprint';
import { PRODUCT_NAME, MANAGEMENT_CLUSTER, CRD_TYPES } from '../config/types';

export default defineComponent({
  name: 'WorkloadsPage',

  components: { Loading, Banner },

  async fetch() {
    await this.loadWorkloads();
  },

  data() {
    return {
      workloads:              [],
      blueprints:             [],
      loading:                false,
      // loadError is set by loadWorkloads only — its banner reads
      // "Failed to load workloads". actionError is set by per-row
      // actions (delete/upgrade) and carries the upstream message.
      // Keeping them separate prevents an upgrade 400 from rendering
      // a misleading "Failed to load workloads" toast.
      loadError:              null,
      actionError:            null,
      deleteTarget:           null,
      deleting:               false,
      upgradeTarget:          null,
      upgradeSelectedVersion: '',
      availableVersions:      [],
      upgrading:              false,
      search:                 '',
      _timer:                 null,
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
      this.loadError = null;
      try {
        this.workloads = await listWorkloads();
        // Blueprints power the lineage→versions picker in the upgrade modal.
        // Fetch via Steve so we share the management-cluster cache.
        this.blueprints = await this.$store.dispatch('management/findAll', { type: CRD_TYPES.BLUEPRINT });
      } catch (e) {
        this.loadError = e;
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
      this.actionError = null;
      this.deleteTarget = wl;
    },

    async doDelete() {
      if (!this.deleteTarget) {
        return;
      }
      this.actionError = null;
      this.deleting = true;
      try {
        await deleteWorkload(this.deleteTarget.metadata.namespace, this.deleteTarget.metadata.name);
        this.deleteTarget = null;
        await this.loadWorkloads();
      } catch (e) {
        this.actionError = e;
      } finally {
        this.deleting = false;
      }
    },

    // Returns versions of `lineageName` strictly greater than `currentVersion`,
    // sorted ascending (so the picker lists the lowest valid upgrade first).
    // Mirrors pkg/workload/upgrader.go: it rejects Withdrawn and same-or-lower
    // versions but accepts Deprecated targets — deprecation is "discouraged,
    // not forbidden". The picker reflects the same policy and renders the
    // phase suffix on Deprecated options so the user can choose informedly.
    blueprintUpgradeCandidates(lineageName, currentVersion) {
      return this.blueprints
        .filter((b) => b.spec?.blueprintName === lineageName)
        .filter((b) => b.status?.phase !== 'Withdrawn')
        .map((b) => ({ version: b.spec.version, phase: b.status?.phase || 'Active' }))
        .filter((c) => compareVersions(c.version, currentVersion) > 0)
        .sort((a, b) => compareVersions(a.version, b.version));
    },

    confirmUpgrade(wl) {
      const lineage = wl.spec?.source?.blueprint?.name || '';
      const current = wl.spec?.source?.blueprint?.version || '';
      const candidates = this.blueprintUpgradeCandidates(lineage, current);

      // The row's Upgrade button is gated on candidates.length > 0
      // (hasUpgradeCandidates), so this is defence-in-depth: if a click
      // somehow races a refresh that emptied the list, do nothing.
      if (candidates.length === 0) {
        return;
      }
      this.actionError            = null;
      this.upgradeTarget          = wl;
      this.availableVersions      = candidates;
      this.upgradeSelectedVersion = candidates[0].version;
    },

    hasUpgradeCandidates(wl) {
      if (wl.spec?.source?.kind !== 'Blueprint') {
        return false;
      }
      const lineage = wl.spec?.source?.blueprint?.name || '';
      const current = wl.spec?.source?.blueprint?.version || '';

      return this.blueprintUpgradeCandidates(lineage, current).length > 0;
    },

    async doUpgrade() {
      if (!this.upgradeTarget || !this.upgradeSelectedVersion) {
        return;
      }
      this.actionError = null;
      this.upgrading = true;
      try {
        await upgradeWorkload(
          this.upgradeTarget.metadata.namespace,
          this.upgradeTarget.metadata.name,
          this.upgradeSelectedVersion,
        );
        this.upgradeTarget = null;
        await this.loadWorkloads();
      } catch (e) {
        this.actionError = e;
      } finally {
        this.upgrading = false;
      }
    },

    optionLabel(v) {
      if (v.phase === 'Active') {
        return v.version;
      }
      return this.t('aif.pages.workloads.upgradeModal.candidateLabel', {
        version: v.version,
        phase:   this.t(`aif.pages.blueprints.phase.${ v.phase.toLowerCase() }`),
      });
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
