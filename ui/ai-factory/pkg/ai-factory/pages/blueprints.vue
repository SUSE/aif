<template>
  <div class="bp-page">
    <header class="bp-page__header">
      <h1>{{ t('aif.pages.blueprints.title') }}</h1>
      <div class="bp-page__counts">
        <span class="bp-page__pill">
          {{ t('aif.pages.blueprints.header.lineages', { count: visibleLineages.length }) }}
        </span>
        <span class="bp-page__pill">
          {{ t('aif.pages.blueprints.header.versions', { count: totalVersions }) }}
        </span>
        <button
          class="btn role-primary"
          @click="$router.push({ name: `${ PRODUCT_NAME }-c-cluster-blueprint-create`, params: { cluster: MANAGEMENT_CLUSTER } })"
        >
          {{ t('aif.pages.blueprints.newBlueprint') }}
        </button>
      </div>
    </header>

    <Banner
      v-if="unreachable"
      color="warning"
      :label="t('aif.pages.blueprints.empty.unreachable')"
    />

    <Banner
      v-if="loadError"
      color="error"
      :label="t('aif.pages.blueprints.empty.loadError', { message: loadError })"
    />

    <Loading v-if="!loaded" />

    <template v-else>
      <div class="bp-page__toolbar">
        <input
          v-model="search"
          type="search"
          :placeholder="t('aif.pages.blueprints.toolbar.search')"
          class="bp-page__search"
        />
        <select v-model="useCaseFilter" class="bp-page__select">
          <option value="">{{ t('aif.pages.blueprints.toolbar.useCaseAll') }}</option>
          <option v-for="uc in useCases" :key="uc" :value="uc">{{ uc }}</option>
        </select>
        <div class="bp-page__toggle">
          <Checkbox v-model:value="showWithdrawn" :label="t('aif.pages.blueprints.toolbar.showWithdrawn')" />
        </div>
      </div>

      <div v-if="visibleLineages.length" class="bp-page__gallery">
        <BlueprintCard
          v-for="l in visibleLineages"
          :key="l.lineage"
          :lineage="l"
          :is-publisher="isPublisher"
          :show-withdrawn="showWithdrawn"
          @view-versions="onViewVersions"
        />
      </div>

      <div v-else-if="hasAnyLineage" class="bp-page__empty">
        <p>{{ t('aif.pages.blueprints.empty.noResults') }}</p>
      </div>

      <div v-else-if="!unreachable" class="bp-page__empty">
        <p>{{ t('aif.pages.blueprints.empty.none') }}</p>
      </div>
    </template>

    <BlueprintVersionsPanel
      v-if="panelLineage"
      :lineage="panelLineage"
      @close="panelLineage = null"
    />
  </div>
</template>

<script>
import { defineComponent, ref, computed, getCurrentInstance } from 'vue';
import Loading from '@shell/components/Loading';
import { Banner } from '@components/Banner';
import { Checkbox } from '@components/Form/Checkbox';
import BlueprintCard from '../components/blueprints/BlueprintCard.vue';
import BlueprintVersionsPanel from '../components/blueprints/BlueprintVersionsPanel.vue';
import { groupByLineage, readUnreachable, readPublisherOverride } from '../utils/blueprint';
import { CRD_TYPES, PRODUCT_NAME, MANAGEMENT_CLUSTER } from '../config/types';

export default defineComponent({
  name: 'BlueprintsPage',

  components: {
    Loading, Banner, Checkbox, BlueprintCard, BlueprintVersionsPanel
  },

  async fetch() {
    try {
      await Promise.all([
        this.$store.dispatch('management/findAll', { type: CRD_TYPES.BLUEPRINT }),
        this.$store.dispatch('management/findAll', { type: CRD_TYPES.SETTINGS })
      ]);
      this.loaded = true;
    } catch (e) {
      this.loadError = e?.message || String(e);
      this.loaded    = true;
    }
  },

  data() {
    return {
      loaded:        false,
      loadError:     '',
      search:        '',
      useCaseFilter: '',
      showWithdrawn: false,
      panelLineage:  null,
      PRODUCT_NAME,
      MANAGEMENT_CLUSTER
    };
  },

  computed: {
    lineages() {
      const blueprints = this.$store.getters['management/all'](CRD_TYPES.BLUEPRINT) || [];

      return groupByLineage(blueprints);
    },

    unreachable() {
      // Settings is a singleton CR; settings[0] may be undefined when the cluster
      // has none yet. readUnreachable handles undefined safely (returns false) —
      // see tests/unit/blueprint.spec.js covering the null/undefined input case.
      const settings = this.$store.getters['management/all'](CRD_TYPES.SETTINGS) || [];

      return readUnreachable(settings[0]);
    },

    isPublisher() {
      return readPublisherOverride().value;
    },

    useCases() {
      const set = new Set();

      for (const l of this.lineages) {
        for (const v of l.versions) {
          if (v.useCase) set.add(v.useCase);
        }
      }

      return [...set].sort();
    },

    hasAnyLineage() {
      return this.lineages.length > 0;
    },

    visibleLineages() {
      const q = this.search.trim().toLowerCase();

      return this.lineages.filter((l) => {
        if (!this.showWithdrawn && l.versions.every((v) => v.phase === 'Withdrawn')) {
          return false;
        }
        if (this.useCaseFilter && !l.versions.some((v) => v.useCase === this.useCaseFilter)) {
          return false;
        }
        if (!q) return true;

        const haystack = `${ l.lineage } ${ l.versions.map((v) => `${ v.description } ${ v.useCase }`).join(' ') }`.toLowerCase();

        return haystack.includes(q);
      });
    },

    totalVersions() {
      let total = 0;

      for (const l of this.visibleLineages) {
        total += this.showWithdrawn
          ? l.versions.length
          : l.versions.filter((v) => v.phase !== 'Withdrawn').length;
      }

      return total;
    }
  },

  methods: {
    onViewVersions(lineage) {
      this.panelLineage = lineage;
    }
  }
});
</script>

<style lang="scss" scoped>
.bp-page {
  padding: 20px;

  &__header {
    display:         flex;
    align-items:     baseline;
    justify-content: space-between;
    margin-bottom:   15px;

    h1 { margin: 0; }
  }
  &__counts {
    display: flex;
    gap:     8px;
  }
  &__pill {
    background: var(--accent-btn);
    color:      var(--body-text);
    padding:    2px 10px;
    border-radius: 12px;
    font-size:  0.9em;
    border:     1px solid var(--border);
  }
  &__toolbar {
    display:        flex;
    flex-wrap:      wrap;
    gap:            10px;
    margin-bottom:  15px;
    align-items:    center;
  }
  &__search {
    flex:       1 1 160px;
    min-width:  120px;
    width:      auto;
    padding:    6px 10px;
    height:     36px;
    box-sizing: border-box;
  }
  &__select {
    flex:       0 1 auto;
    width:      auto;
    min-width:  120px;
    padding:    6px 10px;
    height:     36px;
    box-sizing: border-box;
  }
  &__toggle {
    flex:        0 1 auto;
    display:     inline-flex;
    align-items: center;
    height:      36px;
    width:       auto;

    :deep(.checkbox-outer-container) {
      width:        auto;
      margin-bottom: 0;
    }
  }
  &__gallery {
    display:               grid;
    grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
    gap:                   15px;
  }
  &__empty {
    text-align: center;
    padding:    40px 20px;
    color:      var(--muted);
  }
}
</style>
