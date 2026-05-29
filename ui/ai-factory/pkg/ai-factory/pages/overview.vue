<template>
  <div class="aif-overview">
    <div class="aif-overview__header">
      <h1>{{ t('aif.pages.overview.title') }}</h1>
      <button class="btn btn-sm role-secondary" @click="refresh">
        {{ t('aif.pages.overview.refresh') }}
      </button>
    </div>

    <Banner v-if="error" color="error" :label="t('aif.pages.overview.error')" />

    <!-- Summary cards -->
    <div class="aif-overview__cards">
      <button
        type="button"
        class="aif-overview__card"
        :aria-label="t('aif.pages.overview.cards.totalWorkloads')"
        @click="goTo('workloads')"
      >
        <div class="aif-overview__card-value">{{ counts.total }}</div>
        <div class="aif-overview__card-label">{{ t('aif.pages.overview.cards.totalWorkloads') }}</div>
      </button>
      <button
        type="button"
        class="aif-overview__card aif-overview__card--success"
        :aria-label="t('aif.pages.overview.cards.running')"
        @click="goTo('workloads')"
      >
        <div class="aif-overview__card-value">{{ counts.running }}</div>
        <div class="aif-overview__card-label">{{ t('aif.pages.overview.cards.running') }}</div>
      </button>
      <button
        type="button"
        class="aif-overview__card aif-overview__card--warning"
        :aria-label="t('aif.pages.overview.cards.withIssues')"
        @click="goTo('workloads')"
      >
        <div class="aif-overview__card-value">{{ counts.withIssues }}</div>
        <div class="aif-overview__card-label">{{ t('aif.pages.overview.cards.withIssues') }}</div>
      </button>
      <button
        type="button"
        class="aif-overview__card"
        :aria-label="t('aif.pages.overview.cards.activeBlueprints')"
        @click="goTo('blueprints')"
      >
        <div class="aif-overview__card-value">{{ counts.activeBlueprints }}</div>
        <div class="aif-overview__card-label">{{ t('aif.pages.overview.cards.activeBlueprints') }}</div>
      </button>
    </div>

    <!-- Recent Workloads + Active Blueprints panels -->
    <div class="aif-overview__panels">
      <div class="aif-overview__panel">
        <div class="aif-overview__panel-header">
          <h3>{{ t('aif.pages.overview.recentWorkloads.title') }}</h3>
          <a href="#" @click.prevent="goTo('workloads')">{{ t('aif.pages.overview.recentWorkloads.viewAll') }}</a>
        </div>
        <p v-if="recentWorkloads.length === 0" class="aif-overview__empty">
          {{ t('aif.pages.overview.recentWorkloads.empty') }}
        </p>
        <table v-else class="aif-overview__mini-table">
          <tbody>
            <tr v-for="wl in recentWorkloads" :key="`${ wl.metadata?.namespace }/${ wl.metadata?.name }`">
              <td><span :class="`badge badge--${ phaseBadge(wl) }`">{{ phaseLabel(wl) }}</span></td>
              <td>{{ wl.metadata?.name }}</td>
              <td>{{ sourceLabel(wl) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="aif-overview__panel">
        <div class="aif-overview__panel-header">
          <h3>{{ t('aif.pages.overview.activeBlueprints.title') }}</h3>
          <a href="#" @click.prevent="goTo('blueprints')">{{ t('aif.pages.overview.activeBlueprints.viewAll') }}</a>
        </div>
        <p v-if="activeBlueprints.length === 0" class="aif-overview__empty">
          {{ t('aif.pages.overview.activeBlueprints.empty') }}
        </p>
        <ul v-else class="aif-overview__bp-list">
          <li v-for="bp in activeBlueprints" :key="bp.lineage">
            {{ bp.lineage }}
            <span class="badge badge--primary">{{ bp.version }}</span>
          </li>
        </ul>
      </div>
    </div>

    <!-- Quick Actions -->
    <div class="aif-overview__quick-actions">
      <h3>{{ t('aif.pages.overview.quickActions.title') }}</h3>
      <div class="aif-overview__actions-grid">
        <button
          type="button"
          class="aif-overview__action-card"
          :aria-label="t('aif.pages.overview.quickActions.browseApps')"
          @click="goTo('apps')"
        >
          <strong>{{ t('aif.pages.overview.quickActions.browseApps') }}</strong>
          <p>{{ t('aif.pages.overview.quickActions.browseAppsDesc') }}</p>
        </button>
        <button
          type="button"
          class="aif-overview__action-card"
          :aria-label="t('aif.pages.overview.quickActions.manageBlueprints')"
          @click="goTo('blueprints')"
        >
          <strong>{{ t('aif.pages.overview.quickActions.manageBlueprints') }}</strong>
          <p>{{ t('aif.pages.overview.quickActions.manageBlueprintsDesc') }}</p>
        </button>
        <button
          type="button"
          class="aif-overview__action-card"
          :aria-label="t('aif.pages.overview.quickActions.viewWorkloads')"
          @click="goTo('workloads')"
        >
          <strong>{{ t('aif.pages.overview.quickActions.viewWorkloads') }}</strong>
          <p>{{ t('aif.pages.overview.quickActions.viewWorkloadsDesc') }}</p>
        </button>
      </div>
    </div>
  </div>
</template>

<script>
import { defineComponent } from 'vue';
import { Banner } from '@components/Banner';
import { CRD_TYPES, PRODUCT_NAME, MANAGEMENT_CLUSTER } from '../config/types';
import { listWorkloads } from '../utils/operator-api';
import { groupByLineage } from '../utils/blueprint';

export default defineComponent({
  name: 'OverviewPage',

  components: { Banner },

  async fetch() {
    await this.loadData();
  },

  data() {
    return {
      workloads:        [],
      blueprints:       [],
      error:            null,
      _timer:           null,
    };
  },

  computed: {
    counts() {
      const total      = this.workloads.length;
      const running    = this.workloads.filter((w) => w.status?.phase === 'Running').length;
      const withIssues = this.workloads.filter((w) => ['Degraded', 'Failed'].includes(w.status?.phase)).length;
      // Count distinct lineages that have at least one Active version (matches the
      // reference's "active blueprint families" — not raw CR count).
      const activeBlueprints = groupByLineage(this.blueprints).filter((l) => l.latestActive).length;
      return { total, running, withIssues, activeBlueprints };
    },

    recentWorkloads() {
      return [...this.workloads]
        .sort((a, b) => new Date(b.metadata?.creationTimestamp || 0) - new Date(a.metadata?.creationTimestamp || 0))
        .slice(0, 5);
    },

    // Group by lineage and show the latest Active version of each (max 5),
    // mirroring the reference's activeBlueprintList.
    activeBlueprints() {
      return groupByLineage(this.blueprints)
        .filter((l) => l.latestActive)
        .map((l) => ({ lineage: l.lineage, version: l.latestActive.version }))
        .slice(0, 5);
    },
  },

  mounted() {
    // Background poll is silent (see silentRefresh) — no spinner, no error flash.
    // Mirrors the workloads.vue setInterval form so the only two auto-polling
    // pages share a single shape; the .bind(this) is redundant under Vue 3
    // Options API but matches the existing precedent. Follow-up: drop the
    // redundant .bind from both call sites in one pass.
    this._timer = setInterval(this.silentRefresh.bind(this), 10 * 1000);
  },

  beforeUnmount() {
    clearInterval(this._timer);
  },

  methods: {
    async fetchData() {
      const [workloads, blueprints] = await Promise.all([
        listWorkloads(),
        this.$store.dispatch('management/findAll', { type: CRD_TYPES.BLUEPRINT }),
      ]);
      this.workloads  = workloads;
      this.blueprints = blueprints;
    },

    async loadData() {
      this.error = null;
      try {
        await this.fetchData();
      } catch (e) {
        this.error = e;
      }
    },

    // 10s background poll: refresh data but keep the last good state on a
    // transient failure — never surface the error banner mid-poll.
    async silentRefresh() {
      try {
        await this.fetchData();
      } catch (e) {
        /* swallow — keep last good data */
      }
    },

    async refresh() {
      await this.loadData();
    },

    goTo(page) {
      this.$router.push({ name: `${ PRODUCT_NAME }-c-cluster-${ page }`, params: { cluster: MANAGEMENT_CLUSTER } });
    },

    sourceLabel(wl) {
      const src = wl.spec?.source;
      if (src?.app) {
        return src.app.chart || 'App';
      }
      if (src?.blueprint) {
        return `${ src.blueprint.name } v${ src.blueprint.version }`;
      }
      // WorkloadSource.Kind enum is App|Blueprint (BundleTest was removed in
      // P4-3 — see CLAUDE.md follow-up note on `WorkloadSource.Kind`). Both
      // kinds are handled above, so this branch only fires for a missing or
      // malformed source — render the em-dash placeholder.
      return '—';
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

    // Reuses the workloads page l10n namespace so the two pages render the
    // same translated label for a given phase. Falls back to the raw CRD
    // value if no translation is registered.
    phaseLabel(wl) {
      const phase = wl.status?.phase || 'Unknown';
      const key   = `aif.pages.workloads.phase.${ phase.toLowerCase() }`;
      return this.t(key, undefined, true) || phase;
    },
  },
});
</script>

<style scoped>
.aif-overview__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 24px;
}

.aif-overview__cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 24px;
}

.aif-overview__card {
  background: var(--body-bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 20px;
  cursor: pointer;
  text-align: center;
  /* Reset native <button> defaults so the element renders like the old <div>
     card while keeping focus, keyboard activation, and role=button. */
  font:    inherit;
  color:   inherit;
  width:   100%;
}

.aif-overview__card-value {
  font-size: 2rem;
  font-weight: 700;
}

.aif-overview__panels {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-bottom: 24px;
}

.aif-overview__panel {
  background: var(--body-bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 16px;
}

.aif-overview__panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.aif-overview__mini-table {
  width: 100%;
  border-collapse: collapse;
}

.aif-overview__mini-table td {
  padding: 4px 8px;
}

.aif-overview__actions-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  margin-top: 12px;
}

.aif-overview__action-card {
  background: var(--body-bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 16px;
  cursor: pointer;
  /* Reset native <button> defaults so the element renders like the old <div>
     card while keeping focus, keyboard activation, and role=button. */
  font:       inherit;
  color:      inherit;
  text-align: left;
  width:      100%;
}

/* Drop default browser bullets on the Active Blueprints list so the panel
   matches the Recent Workloads table — both panels render flush-left rows. */
.aif-overview__bp-list {
  list-style: none;
  padding:    0;
  margin:     0;
}

/* Empty-state copy in both panels (Recent Workloads, Active Blueprints).
   Mirrors the bp-page__empty rule on the Blueprints page. */
.aif-overview__empty {
  text-align: center;
  color:      var(--muted);
  padding:    12px;
}
</style>
