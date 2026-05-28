<template>
  <div class="apps-page">
    <!-- Header -->
    <div class="apps-page__header">
      <h1>{{ t('aif.pages.apps.title') }}</h1>
    </div>

    <!-- Toolbar -->
    <div class="apps-page__toolbar">
      <input
        v-model="search"
        type="search"
        :placeholder="t('aif.pages.apps.toolbar.search')"
        class="apps-page__search"
      />

      <select v-model="registry" class="apps-page__select" @change="loadApps">
        <option value="suse">{{ t('aif.pages.apps.toolbar.registrySuseLibrary') }}</option>
        <option value="nvidia">{{ t('aif.pages.apps.toolbar.registryNvidia') }}</option>
      </select>

      <div class="apps-page__toolbar-right">
        <button class="btn role-primary btn-sm apps-page__refresh" :disabled="loading" @click="$event.currentTarget.blur(); refresh()">
          <i v-if="loading" class="icon icon-spinner icon-spin" />
          <i v-else class="icon icon-refresh" />
          {{ t('aif.pages.apps.toolbar.refresh') }}
        </button>

        <div class="apps-page__view-toggle">
          <button
            :class="['btn', 'btn-sm', viewMode === 'tiles' ? 'role-primary' : 'role-secondary']"
            :title="t('aif.pages.apps.toolbar.viewTile')"
            @click="viewMode = 'tiles'"
          >
            <i class="icon icon-apps" />
          </button>
          <button
            :class="['btn', 'btn-sm', viewMode === 'list' ? 'role-primary' : 'role-secondary']"
            :title="t('aif.pages.apps.toolbar.viewList')"
            @click="viewMode = 'list'"
          >
            <i class="icon icon-list-flat" />
          </button>
        </div>
      </div>
    </div>

    <!-- Results summary -->
    <div class="apps-page__summary">{{ t('aif.pages.apps.resultsSummary', { count: filteredApps.length }) }}</div>

    <!-- Error banner -->
    <Banner v-if="error" color="error" :label="error" class="apps-page__error" />

    <!-- Loading -->
    <div v-if="loading" class="apps-page__loading">
      <i class="icon icon-spinner icon-spin icon-3x" />
    </div>

    <!-- Content -->
    <template v-else-if="!error">
      <!-- Tile view -->
      <div v-if="viewMode === 'tiles' && filteredApps.length" class="apps-page__tiles-grid">
        <AppCard
          v-for="app in filteredApps"
          :key="app.id"
          :app="app"
          @install="onInstall"
        />
      </div>

      <!-- List view -->
      <div v-if="viewMode === 'list' && filteredApps.length" class="apps-page__list-view">
        <table class="sortable-table">
          <thead>
            <tr>
              <th></th>
              <th>{{ t('aif.pages.apps.list.name') }}</th>
              <th>{{ t('aif.pages.apps.list.description') }}</th>
              <th class="text-right">{{ t('aif.pages.apps.list.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="app in filteredApps" :key="app.id" class="main-row" tabindex="0" @click="onInstall(app)" @keydown.enter="onInstall(app)" @keydown.space.prevent="onInstall(app)">
              <td class="col-logo">
                <img :src="app.logoURL || fallbackLogo" :alt="app.name" class="table-logo" @error="onImgError" />
              </td>
              <td class="col-name">
                <span class="app-name">{{ app.displayName || app.name }}</span>
                <span class="packaging-badge">{{ app.assetType === 'chart' ? t('aif.pages.apps.packaging.helm') : t('aif.pages.apps.packaging.container') }}</span>
              </td>
              <td class="col-description">{{ app.description || '—' }}</td>
              <td class="text-right col-actions">
                <i class="icon icon-chevron-right" />
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Empty: no results after filtering -->
      <div v-if="!filteredApps.length && apps.length" class="apps-page__empty">
        <p>{{ t('aif.pages.apps.empty.noResults') }}</p>
      </div>

      <!-- Empty: no catalog at all -->
      <div v-if="!filteredApps.length && !apps.length && !loading" class="apps-page__empty">
        <p>{{ t('aif.pages.apps.empty.noCatalog') }}</p>
      </div>
    </template>

  </div>
</template>

<script>
import { defineComponent, ref, computed, onMounted, getCurrentInstance } from 'vue';
import AppCard from '../components/apps/AppCard.vue';
import { listApps } from '../utils/operator-api';
import { FALLBACK_LOGO } from '../config/constants';
import { PRODUCT_NAME, MANAGEMENT_CLUSTER } from '../config/types';
import { Banner } from '@components/Banner';

export default defineComponent({
  name: 'AppsPage',

  components: { AppCard, Banner },

  setup() {
    const instance = getCurrentInstance();
    const t = instance?.proxy?.t?.bind(instance.proxy) || ((key) => key);

    const loading = ref(true);
    const error = ref('');
    const apps = ref([]);
    const search = ref('');
    const registry = ref('suse');
    const viewMode = ref('tiles');
    const fallbackLogo = FALLBACK_LOGO;

    const filteredApps = computed(() => {
      if (!search.value) {
        return apps.value;
      }
      const q = search.value.toLowerCase();

      return apps.value.filter((app) => {
        return app.name.toLowerCase().includes(q) ||
               (app.displayName || '').toLowerCase().includes(q) ||
               (app.description || '').toLowerCase().includes(q);
      });
    });

    const loadApps = async () => {
      loading.value = true;
      error.value = '';

      try {
        apps.value = await listApps({ source: registry.value });
      } catch (err) {
        error.value = err.message || t('aif.pages.apps.empty.error');
        apps.value = [];
      } finally {
        loading.value = false;
      }
    };

    const refresh = async () => {
      await loadApps();
    };

    const onInstall = (app) => {
      instance?.proxy?.$router.push({
        name:   `${ PRODUCT_NAME }-c-cluster-app-install`,
        params: { cluster: MANAGEMENT_CLUSTER, id: app.id },
      });
    };

    const onImgError = (event) => {
      event.target.src = FALLBACK_LOGO;
    };

    onMounted(() => {
      refresh();
    });

    return {
      loading,
      error,
      apps,
      search,
      registry,
      viewMode,
      filteredApps,
      fallbackLogo,
      loadApps,
      refresh,
      onInstall,
      onImgError,
      t
    };
  }
});
</script>

<style lang="scss" scoped>
.apps-page {
  padding: 20px;
}

.apps-page__header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;

  h1 {
    margin: 0;
    font-size: 24px;
    font-weight: 600;
  }
}

.apps-page__toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
  flex-wrap: wrap;

  > * {
    flex-shrink: 0;
  }
}

.apps-page__search {
  width: 200px;
  max-width: 200px;
  height: 32px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: var(--border-radius);
  background: var(--input-bg);
  color: var(--body-text);
  font-size: 14px;
  display: inline-block;
}

.apps-page__select {
  height: 32px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: var(--border-radius);
  background: var(--input-bg);
  color: var(--body-text);
  font-size: 14px;
  width: auto;
  min-width: 180px;
  display: inline-block;
}

.apps-page__refresh {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.apps-page__toolbar-right {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-left: auto;
}

.apps-page__view-toggle {
  display: flex;
  border: 1px solid var(--border);
  border-radius: var(--border-radius);
  overflow: hidden;

  .btn {
    border: none;
    border-radius: 0;

    &:not(:last-child) {
      border-right: 1px solid var(--border);
    }
  }
}

.apps-page__summary {
  font-size: 13px;
  color: var(--muted);
  margin-bottom: 12px;
}

.apps-page__error {
  margin-bottom: 16px;
}

.apps-page__loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 80px 0;
  color: var(--muted);
}

.apps-page__tiles-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}

.apps-page__list-view {
  .sortable-table {
    width: 100%;
    border-collapse: collapse;
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;

    th {
      background: var(--sortable-table-header-bg);
      padding: 10px 12px;
      text-align: left;
      font-weight: 600;
      font-size: 13px;
      border-bottom: 1px solid var(--border);
    }

    td {
      padding: 10px 12px;
      border-bottom: 1px solid var(--border);
      vertical-align: middle;
    }

    .main-row {
      cursor: pointer;

      &:hover {
        background: var(--sortable-table-accent-bg);
      }
    }
  }

  .col-logo {
    width: 40px;
  }

  .table-logo {
    width: 28px;
    height: 28px;
    object-fit: contain;
    border-radius: 4px;
    background: var(--accent-btn);
  }

  .app-name {
    font-weight: 600;
  }

  .packaging-badge {
    margin-left: 8px;
    padding: 1px 6px;
    border-radius: 8px;
    font-size: 10px;
    font-weight: 600;
    background: var(--accent-btn, #f5f5f5);
    color: var(--muted);
  }

  .col-description {
    color: var(--body-text);
    font-size: 13px;
  }

  .col-actions {
    white-space: nowrap;
    color: var(--muted);
  }
}

.apps-page__empty {
  text-align: center;
  padding: 60px 20px;
  color: var(--muted);

  p {
    max-width: 400px;
    margin: 0 auto;
    line-height: 1.5;
  }
}
</style>
