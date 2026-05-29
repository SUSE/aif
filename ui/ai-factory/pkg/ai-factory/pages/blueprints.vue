<template>
  <div class="bp-page">
    <header class="bp-page__header">
      <h1>{{ t('aif.pages.blueprints.title') }}</h1>
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

    <Banner
      v-if="crudError"
      color="error"
      :label="crudError"
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
        <div class="bp-page__toggle">
          <Checkbox v-model:value="showDeprecated" :label="t('aif.pages.blueprints.toolbar.showDeprecated')" />
        </div>
        <button class="btn role-primary" @click="navigateCreate">
          {{ t('aif.pages.blueprints.toolbar.create') }}
        </button>
        <button class="btn role-secondary" @click="$fetch()">
          {{ t('aif.pages.blueprints.toolbar.refresh') }}
        </button>
      </div>

      <div v-if="visibleLineages.length" class="bp-page__gallery">
        <BlueprintCard
          v-for="l in visibleLineages"
          :key="l.lineage"
          :lineage="l"
          :is-admin="isAdmin"
          :show-deprecated="showDeprecated"
          @deploy="onCardDeploy"
          @copy="onCardCopy"
          @edit="onCardEdit"
          @deprecate="onCardDeprecate"
          @delete="onCardDelete"
        />
      </div>

      <div v-else-if="hasAnyLineage" class="bp-page__empty">
        <p>{{ t('aif.pages.blueprints.empty.noResults') }}</p>
      </div>

      <div v-else-if="!unreachable" class="bp-page__empty">
        <p>{{ t('aif.pages.blueprints.empty.none') }}</p>
      </div>
    </template>

    <!-- Deprecate / Undeprecate -->
    <div v-if="deprecateTarget" class="aif-modal-backdrop" @click.self="deprecateTarget = null">
      <div class="aif-modal">
        <h3>{{ deprecateTarget.currentlyDeprecated
          ? t('aif.pages.blueprints.undeprecateModal.title')
          : t('aif.pages.blueprints.deprecateModal.title') }}</h3>
        <p>{{ deprecateTarget.currentlyDeprecated
          ? t('aif.pages.blueprints.undeprecateModal.body', { name: deprecateTarget.lineage, version: deprecateTarget.version })
          : t('aif.pages.blueprints.deprecateModal.body', { name: deprecateTarget.lineage, version: deprecateTarget.version }) }}</p>
        <Banner
          v-if="!deprecateTarget.currentlyDeprecated && deprecateTarget.activeWorkloads.length"
          color="warning"
          :label="t('aif.pages.blueprints.activeWorkloadsWarning', { count: deprecateTarget.activeWorkloads.length })"
        />
        <div class="aif-modal__actions">
          <button class="btn role-secondary" @click="deprecateTarget = null">{{ t('aif.pages.blueprints.deprecateModal.cancel') }}</button>
          <button class="btn role-primary" @click="doDeprecate">
            {{ deprecateTarget.currentlyDeprecated
              ? t('aif.pages.blueprints.undeprecateModal.confirm')
              : t('aif.pages.blueprints.deprecateModal.confirm') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Delete -->
    <div v-if="deleteTarget" class="aif-modal-backdrop" @click.self="deleteTarget = null">
      <div class="aif-modal">
        <h3>{{ t('aif.pages.blueprints.deleteModal.title') }}</h3>
        <p>{{ t('aif.pages.blueprints.deleteModal.body', { name: deleteTarget.lineage, version: deleteTarget.version }) }}</p>
        <Banner
          v-if="deleteTarget.activeWorkloads.length"
          color="warning"
          :label="t('aif.pages.blueprints.activeWorkloadsWarning', { count: deleteTarget.activeWorkloads.length })"
        />
        <div class="aif-modal__actions">
          <button class="btn role-secondary" @click="deleteTarget = null">{{ t('aif.pages.blueprints.deleteModal.cancel') }}</button>
          <button class="btn role-danger" @click="doDelete">{{ t('aif.pages.blueprints.deleteModal.confirm') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import { defineComponent } from 'vue';
import Loading from '@shell/components/Loading';
import { Banner } from '@components/Banner';
import { Checkbox } from '@components/Form/Checkbox';
import BlueprintCard from '../components/blueprints/BlueprintCard.vue';
import { groupByLineage, readUnreachable } from '../utils/blueprint';
import { deprecateBlueprint, deleteBlueprint, listWorkloads } from '../utils/operator-api';
import { CRD_TYPES, PRODUCT_NAME, MANAGEMENT_CLUSTER } from '../config/types';

export default defineComponent({
  name: 'BlueprintsPage',

  components: {
    Loading, Banner, Checkbox, BlueprintCard
  },

  async fetch() {
    try {
      await Promise.all([
        this.$store.dispatch('management/findAll', { type: CRD_TYPES.BLUEPRINT }),
        this.$store.dispatch('management/findAll', { type: CRD_TYPES.SETTINGS })
      ]);
      await this.checkAdminRole();
      this.loaded = true;
    } catch (e) {
      this.loadError = e?.message || String(e);
      this.loaded    = true;
    }
  },

  data() {
    return {
      loaded:          false,
      loadError:       '',
      search:          '',
      showDeprecated:  false,
      isAdmin:         false,
      deprecateTarget: null,
      deleteTarget:    null,
      crudError:       null,
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

    hasAnyLineage() {
      return this.lineages.length > 0;
    },

    visibleLineages() {
      const q = this.search.trim().toLowerCase();

      return this.lineages.filter((l) => {
        if (!this.showDeprecated && l.versions.every((v) => v.phase !== 'Active')) {
          return false;
        }
        if (!q) return true;

        const haystack = `${ l.lineage } ${ l.versions.map((v) => `${ v.description } ${ v.useCase }`).join(' ') }`.toLowerCase();

        return haystack.includes(q);
      });
    }
  },

  methods: {
    async checkAdminRole() {
      try {
        const grbs   = await this.$store.dispatch('management/findAll', { type: 'management.cattle.io.globalrolebinding' });
        const userId = this.$store.getters['auth/user']?.id;
        this.isAdmin = !!(userId && grbs.some((g) => g.userName === userId && g.globalRoleName === 'admin'));
      } catch (e) {
        this.isAdmin = false;
      }
    },

    async fetchActiveWorkloads(lineage, version) {
      try {
        const res = await listWorkloads();
        return (res.items || res || []).filter((wl) => {
          const bp = wl.spec?.source?.blueprint;
          return bp?.name === lineage && bp?.version === version;
        });
      } catch {
        return [];
      }
    },

    navigateCreate() {
      this.$router.push({ name: `${ PRODUCT_NAME }-c-cluster-blueprint-create`, params: { cluster: MANAGEMENT_CLUSTER } });
    },

    onCardDeploy(v) {
      this.$router.push({
        name:   `${ PRODUCT_NAME }-c-cluster-blueprint-install`,
        params: { cluster: MANAGEMENT_CLUSTER },
        query:  { bpName: v.lineage, bpVersion: v.version },
      });
    },

    onCardCopy(v) {
      this.$router.push({
        name:   `${ PRODUCT_NAME }-c-cluster-blueprint-create`,
        params: { cluster: MANAGEMENT_CLUSTER },
        query:  { copyFrom: v.lineage, copyVersion: v.version },
      });
    },

    onCardEdit(v) {
      this.$router.push({
        name:   `${ PRODUCT_NAME }-c-cluster-blueprint-create`,
        params: { cluster: MANAGEMENT_CLUSTER },
        query:  { editFrom: v.lineage, editVersion: v.version },
      });
    },

    async onCardDeprecate(v) {
      this.crudError = null;
      const lineage = v.lineage;
      const version = v.version;
      const currentlyDeprecated = v.phase !== 'Active';
      this.deprecateTarget = {
        lineage,
        version,
        currentlyDeprecated,
        activeWorkloads: currentlyDeprecated ? [] : await this.fetchActiveWorkloads(lineage, version),
      };
    },

    async doDeprecate() {
      this.crudError = null;
      const { lineage, version, currentlyDeprecated } = this.deprecateTarget;
      try {
        await deprecateBlueprint(lineage, version, !currentlyDeprecated);
        this.deprecateTarget = null;
        await this.$fetch();
      } catch (e) {
        this.crudError = e?.message || String(e);
        this.deprecateTarget = null;
      }
    },

    async onCardDelete(v) {
      this.crudError = null;
      const lineage = v.lineage;
      const version = v.version;
      this.deleteTarget = { lineage, version, activeWorkloads: await this.fetchActiveWorkloads(lineage, version) };
    },

    async doDelete() {
      this.crudError = null;
      const { lineage, version } = this.deleteTarget;
      try {
        await deleteBlueprint(lineage, version);
        this.deleteTarget = null;
        await this.$fetch();
      } catch (e) {
        this.crudError = e?.message || String(e);
        this.deleteTarget = null;
      }
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
.aif-modal-backdrop {
  position:        fixed;
  inset:           0;
  background:      rgba(0, 0, 0, 0.4);
  display:         flex;
  align-items:     center;
  justify-content: center;
  z-index:         100;
}
.aif-modal {
  background:    var(--body-bg);
  border:        1px solid var(--border);
  border-radius: var(--border-radius);
  padding:       20px;
  min-width:     360px;
  max-width:     520px;
  display:       flex;
  flex-direction: column;
  gap:           12px;

  &__actions {
    display:         flex;
    justify-content: flex-end;
    gap:             8px;
    margin-top:      8px;
  }
}
</style>
