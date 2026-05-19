<template>
  <div class="apps-page">
    <!-- Header -->
    <div class="apps-page__header">
      <h1>{{ t('aif.pages.apps.title') }}</h1>
      <div class="apps-page__counts">
        <span class="apps-page__pill">{{ t('aif.pages.apps.header.total', { count: apps.length }) }}</span>
        <span class="apps-page__pill apps-page__pill--nvidia">{{ t('aif.pages.apps.header.nvidia', { count: nvidiaCount }) }}</span>
        <span class="apps-page__pill apps-page__pill--suse">{{ t('aif.pages.apps.header.suse', { count: suseCount }) }}</span>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="apps-page__toolbar">
      <input
        v-model="search"
        type="search"
        :placeholder="t('aif.pages.apps.toolbar.search')"
        class="apps-page__search"
      />

      <select v-model="sourceFilter" class="apps-page__select" @change="loadApps">
        <option value="all">{{ t('aif.pages.apps.toolbar.sourceAll') }}</option>
        <option value="nvidia">{{ t('aif.pages.apps.toolbar.sourceNvidia') }}</option>
        <option value="suse">{{ t('aif.pages.apps.toolbar.sourceSuse') }}</option>
      </select>

      <select v-model="categoryFilter" class="apps-page__select" @change="loadApps">
        <option value="">{{ t('aif.pages.apps.toolbar.categoryAll') }}</option>
        <option v-for="cat in categories" :key="cat" :value="cat">{{ cat }}</option>
      </select>

      <label class="apps-page__toggle">
        <input v-model="includeRefBlueprints" type="checkbox" @change="onToggleRefBlueprints" />
        {{ t('aif.pages.apps.toolbar.includeRefBlueprints') }}
      </label>

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
          @add-to-bundle="onAddToBundle"
        />
      </div>

      <!-- List view -->
      <div v-if="viewMode === 'list' && filteredApps.length" class="apps-page__list-view">
        <table class="sortable-table">
          <thead>
            <tr>
              <th></th>
              <th>{{ t('aif.pages.apps.list.name') }}</th>
              <th>{{ t('aif.pages.apps.list.publisher') }}</th>
              <th>{{ t('aif.pages.apps.list.category') }}</th>
              <th>{{ t('aif.pages.apps.list.version') }}</th>
              <th>{{ t('aif.pages.apps.list.updated') }}</th>
              <th class="text-right">{{ t('aif.pages.apps.list.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="app in filteredApps" :key="app.id" class="main-row">
              <td class="col-logo">
                <img :src="app.logoURL || fallbackLogo" :alt="app.name" class="table-logo" @error="onImgError" />
              </td>
              <td class="col-name">
                <span class="app-name">{{ app.displayName || app.name }}</span>
                <span v-if="app.referenceBlueprint" class="ref-badge">{{ t('aif.pages.apps.badge.referenceBlueprint') }}</span>
              </td>
              <td>
                <span :class="['publisher-badge', `publisher-badge--${app.source}`]">{{ app.publisher }}</span>
              </td>
              <td>{{ (app.categories || []).join(', ') || '—' }}</td>
              <td>{{ app.version }}</td>
              <td>{{ formatDate(app.lastUpdatedAt) }}</td>
              <td class="text-right col-actions">
                <button class="btn btn-sm role-primary" disabled :title="t('aif.pages.apps.card.installDisabled')" @click.stop="onInstall(app)">
                  {{ t('aif.pages.apps.card.install') }}
                </button>
                <button class="btn btn-sm role-secondary" @click.stop="$event.currentTarget.blur(); onAddToBundle(app)">
                  {{ t('aif.pages.apps.card.addToBundle') }}
                </button>
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

    <!-- Add to Bundle Dialog -->
    <AddToBundleDialog
      v-if="showAddToBundleDialog && dialogApp"
      :app="dialogApp"
      @added="onBundleAdded"
      @cancel="showAddToBundleDialog = false"
    />
  </div>
</template>

<script>
import { defineComponent, ref, computed, onMounted, getCurrentInstance } from 'vue';
import AppCard from '../components/apps/AppCard.vue';
import AddToBundleDialog from '../components/apps/AddToBundleDialog.vue';
import { listApps, listCategories } from '../utils/operator-api';
import { formatDate } from '../utils/date';
import { FALLBACK_LOGO } from '../config/constants';
import { Banner } from '@components/Banner';

const STORAGE_KEY = 'aif-include-reference-blueprints';

export default defineComponent({
  name: 'AppsPage',

  components: { AppCard, AddToBundleDialog, Banner },

  setup() {
    const instance = getCurrentInstance();
    const store = instance?.proxy?.$store;
    const t = instance?.proxy?.t?.bind(instance.proxy) || ((key) => key);

    const loading = ref(true);
    const error = ref('');
    const apps = ref([]);
    const categories = ref([]);
    const search = ref('');
    const sourceFilter = ref('all');
    const categoryFilter = ref('');
    const viewMode = ref('tiles');
    const includeRefBlueprints = ref(false);
    const showAddToBundleDialog = ref(false);
    const dialogApp = ref(null);
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

    const nvidiaCount = computed(() => apps.value.filter((a) => a.source === 'nvidia').length);
    const suseCount = computed(() => apps.value.filter((a) => a.source === 'suse').length);

    const loadApps = async () => {
      loading.value = true;
      error.value = '';

      try {
        apps.value = await listApps({
          source:                     sourceFilter.value,
          category:                   categoryFilter.value || undefined,
          includeReferenceBlueprints: includeRefBlueprints.value
        });
      } catch (err) {
        error.value = err.message || t('aif.pages.apps.empty.error');
        apps.value = [];
      } finally {
        loading.value = false;
      }
    };

    const loadCategories = async () => {
      try {
        categories.value = await listCategories();
      } catch (err) {
        console.error('AppsPage: failed to load categories', err); // eslint-disable-line no-console
        categories.value = [];
      }
    };

    const refresh = async () => {
      await Promise.all([loadApps(), loadCategories()]);
    };

    const onToggleRefBlueprints = () => {
      localStorage.setItem(STORAGE_KEY, String(includeRefBlueprints.value));
      loadApps();
    };

    const onInstall = (_app) => {
      // P6-8 stub: Deploy Wizard not yet available
    };

    const onAddToBundle = (app) => {
      dialogApp.value = app;
      showAddToBundleDialog.value = true;
    };

    const onBundleAdded = (result) => {
      const appName = dialogApp.value?.displayName || dialogApp.value?.name || '';

      showAddToBundleDialog.value = false;
      dialogApp.value = null;

      const title = result.mode === 'new'
        ? t('aif.pages.apps.dialog.successNew')
        : t('aif.pages.apps.dialog.successExisting');

      instance?.proxy?.$store?.dispatch('growl/success', {
        title,
        message: t('aif.pages.apps.dialog.successMessage', { app: appName, bundle: result.bundle })
      });
    };

    const onImgError = (event) => {
      event.target.src = FALLBACK_LOGO;
    };

    onMounted(() => {
      const stored = localStorage.getItem(STORAGE_KEY);

      if (stored === 'true') {
        includeRefBlueprints.value = true;
      }
      refresh();
    });

    return {
      loading,
      error,
      apps,
      categories,
      search,
      sourceFilter,
      categoryFilter,
      viewMode,
      includeRefBlueprints,
      showAddToBundleDialog,
      dialogApp,
      filteredApps,
      nvidiaCount,
      suseCount,
      fallbackLogo,
      loadApps,
      refresh,
      onToggleRefBlueprints,
      onInstall,
      onAddToBundle,
      onBundleAdded,
      formatDate,
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

.apps-page__counts {
  display: flex;
  gap: 8px;
}

.apps-page__pill {
  padding: 2px 10px;
  border-radius: 12px;
  font-size: 12px;
  font-weight: 600;

  &--nvidia {
    background: var(--success-banner-bg, #dcfce7);
    color: var(--success, #166534);
  }

  &--suse {
    background: var(--info-banner-bg, #dbeafe);
    color: var(--info, #1d4ed8);
  }
}

.apps-page__toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 20px;
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
  min-width: 140px;
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

.apps-page__toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--body-text);
  cursor: pointer;
  margin-left: 8px;
  width: auto;
  white-space: nowrap;

  input[type="checkbox"] {
    width: auto;
    display: inline-block;
  }
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

    .main-row:hover {
      background: var(--sortable-table-accent-bg);
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

  .ref-badge {
    margin-left: 6px;
    background: var(--warning-banner-bg, #fff3e0);
    color: var(--warning, #e65100);
    padding: 1px 6px;
    border-radius: 8px;
    font-size: 10px;
    font-weight: 600;
  }

  .col-actions {
    white-space: nowrap;

    .btn + .btn {
      margin-left: 4px;
    }
  }
}

.publisher-badge {
  padding: 2px 8px;
  border-radius: 8px;
  font-size: 11px;
  font-weight: 600;

  &--nvidia {
    background: var(--success-banner-bg, #dcfce7);
    color: var(--success, #166534);
  }

  &--suse {
    background: var(--info-banner-bg, #dbeafe);
    color: var(--info, #1d4ed8);
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
