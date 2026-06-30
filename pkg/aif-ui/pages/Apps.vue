<template>
  <main class="main-layout">
    <div class="outlet">
      <!-- Header -->
      <header class="fixed-header">
        <h1>Applications</h1>

        <!-- Toolbar with filters and actions -->
        <div class="actions-container" role="toolbar" aria-label="Application filters and actions">
          <div class="search-box">
            <label for="search-input" class="sr-only">Search applications</label>
            <input
              id="search-input"
              v-model="search"
              type="search"
              :placeholder="t('suseai.apps.search', 'Search applications')"
              class="input-sm"
              aria-label="Search applications"
              :aria-describedby="search ? 'search-results-count' : null"
            />
          </div>

          <div class="filter-group">
            <label for="repository-filter" class="sr-only">Filter by repository</label>
            <select
              id="repository-filter"
              v-model="selectedRepo"
              class="form-control"
              aria-label="Filter applications by repository"
            >
              <option v-for="option in repositoryOptions" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </div>

          <div class="view-controls" role="group" aria-label="View mode selection">
            <button
              :class="['btn', 'btn-sm', viewMode === 'tiles' ? 'role-primary' : 'role-secondary']"
              @click="viewMode = 'tiles'"
              :title="t('suseai.apps.tileView', 'Tile View')"
              :aria-label="t('suseai.apps.tileView', 'Tile View')"
              :aria-pressed="viewMode === 'tiles'"
              type="button"
            >
              <i class="icon icon-th view-icon-grid" aria-hidden="true" />
            </button>
            <button
              :class="['btn', 'btn-sm', viewMode === 'list' ? 'role-primary' : 'role-secondary']"
              @click="viewMode = 'list'"
              :title="t('suseai.apps.listView', 'List View')"
              :aria-label="t('suseai.apps.listView', 'List View')"
              :aria-pressed="viewMode === 'list'"
              type="button"
            >
              <i class="icon icon-th-list view-icon-list" aria-hidden="true" />
            </button>
          </div>

          <button
            class="btn role-primary"
            @click="refresh"
            :disabled="loading"
            :title="t('suseai.apps.refresh', 'Refresh')"
            :aria-label="loading ? 'Refreshing applications...' : 'Refresh applications'"
            type="button"
          >
            <i v-if="loading" class="icon icon-spinner icon-spin" aria-hidden="true" />
            <i v-else class="icon icon-refresh" aria-hidden="true" />
            {{ t('suseai.apps.refresh', 'Refresh') }}
          </button>
        </div>
      </header>

      <!-- Error state -->
      <div v-if="error" class="banner bg-error">
        <span>{{ error }}</span>
      </div>

      <!-- Search results count -->
      <div v-if="search && !loading" id="search-results-count" class="sr-only" aria-live="polite">
        {{ filteredApps.length }} {{ filteredApps.length === 1 ? 'application' : 'applications' }} found for "{{ search }}"
      </div>

      <!-- Main content area - always present to avoid layout jumps -->
      <div class="main-content">
        <!-- Results/Loading summary - fixed position to prevent jumps -->
        <div class="results-summary" aria-live="polite">
          <div v-if="filteredApps.length" class="results-text">
            Showing {{ filteredApps.length }} of {{ filteredApps.length }} applications
          </div>
          <div v-else-if="!loading && !error && items.length > 0" class="results-text">
            No applications found
          </div>
        </div>

        <!-- Tiles view -->
        <div v-if="viewMode === 'tiles'" class="tiles-grid" role="grid" aria-label="Applications grid">
          <div
            v-for="app in filteredApps"
            :key="app.slug_name"
            :class="['app-tile', 'clickable-tile']"
            @click="onTileClick(app)"
            :aria-label="`Install ${ app.name }`"
            role="button"
            tabindex="0"
            @keydown.enter="onTileClick(app)"
            @keydown.space.prevent="onTileClick(app)"
          >
            <div class="tile-header">
              <img :src="logoFor(app)" alt="" @error="onImgError($event)" class="tile-logo" />
              <div class="tile-info">
                <div class="tile-title-row">
                  <h3 class="tile-title">{{ app.name }}</h3>
                </div>
                <div class="tile-meta">
                  <span v-if="app.packaging_format" class="tile-meta-item">
                    {{ formatPackagingType(app.packaging_format) }}
                  </span>
                </div>
              </div>
              <div class="tile-actions">
                <a v-if="app.project_url" :href="app.project_url" target="_blank" rel="noopener noreferrer" class="action-link" title="Project page" @click.stop>
                  <i class="icon icon-external-link" />
                </a>
                <a v-if="app.documentation_url" :href="app.documentation_url" target="_blank" rel="noopener noreferrer" class="action-link" title="Documentation" @click.stop>
                  <i class="icon icon-document" />
                </a>
              </div>
            </div>

            <div class="tile-content">
              <p class="tile-description">{{ app.description || '—' }}</p>
            </div>
          </div>
          <div
              v-for="n in 5"
              :key="`filler-${n}`"
              class="app-tile app-tile-filler"
          ></div>
        </div>

      <!-- List view -->
      <div v-else class="list-view">
        <table class="table">
          <thead>
            <tr>
              <th>{{ t('suseai.apps.name', 'Name') }}</th>
              <th>{{ t('suseai.apps.description', 'Description') }}</th>
              <th class="text-right">{{ t('suseai.apps.actions', 'Actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!filteredApps.length && items.length > 0" class="empty-row">
              <td colspan="3" class="text-center text-muted">{{ t('suseai.apps.noApps', 'No applications found') }}</td>
            </tr>
            <tr
              v-else
              v-for="app in filteredApps"
              :key="app.slug_name"
              class="main-row clickable-row"
              @click="onTileClick(app)"
              :aria-label="`Install ${ app.name }`"
              role="button"
              tabindex="0"
              @keydown.enter="onTileClick(app)"
              @keydown.space.prevent="onTileClick(app)"
            >
              <!-- Name column with logo -->
              <td class="col-name">
                <div class="name-cell">
                  <img :src="logoFor(app)" alt="" @error="onImgError($event)" class="table-logo" />
                  <div class="name-info">
                    <div class="app-name">{{ app.name }}</div>
                    <div v-if="app.packaging_format" class="app-meta">
                      <span class="badge-state badge-sm" :class="getBadgeClass(app.packaging_format)">
                        {{ formatPackagingType(app.packaging_format) }}
                      </span>
                    </div>
                  </div>
                </div>
              </td>

              <!-- Description -->
              <td class="col-description">
                <span class="list-description">{{ app.description || '—' }}</span>
              </td>

              <!-- Actions -->
              <td class="col-actions text-right">
                <i class="icon icon-chevron-right" aria-hidden="true"></i>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

        <!-- Empty state: no credentials configured (Settings CR absent or has no secretRef pairs) -->
        <div v-if="!loading && !items.length && !hasRegistryConfigured && !error" class="empty-state-content">
          <i class="icon icon-folder-open icon-4x text-muted" />
          <h3>{{ t('suseai.apps.noRegistryTitle', 'No applications available') }}</h3>
          <p class="text-muted">
            {{ t('suseai.apps.noRegistryDescBefore', 'To browse applications, add your registry connection details on the') }}
            <a class="empty-state-link" role="button" tabindex="0" @click.prevent="goToSettings" @keydown.enter.prevent="goToSettings">{{ t('suseai.apps.settingsLink', 'Settings') }}</a>
            {{ t('suseai.apps.noRegistryDescAfter', 'page.') }}
          </p>
        </div>

        <!-- Empty state: credentials configured but no apps found yet -->
        <div v-else-if="!loading && !items.length && hasRegistryConfigured && !error" class="empty-state-content">
          <i class="icon icon-folder-open icon-4x text-muted" />
          <h3>{{ t('suseai.apps.noAppsYetTitle', 'No applications available yet') }}</h3>
          <p class="text-muted">
            {{ t('suseai.apps.noAppsYetDescBefore', 'Registry connections are configured but no applications were found. If you recently added your registry, the catalog may still be loading — this can take a few minutes.') }}
            <a
                class="empty-state-link"
                role="button"
                :tabindex="loading ? -1 : 0"
                :aria-disabled="loading || undefined"
                @click.prevent="!loading && refresh()"
                @keydown.enter.prevent="!loading && refresh()"
              >{{ t('suseai.apps.refresh', 'Refresh') }}</a> {{ t('suseai.apps.noAppsYetDescAfter', 'to check again.') }}
          </p>
        </div>

        <!-- Empty state: apps loaded but search/filter yields no results -->
        <div v-else-if="!loading && !filteredApps.length && items.length > 0 && !error" class="empty-state-content">
          <i class="icon icon-folder-open icon-4x text-muted" />
          <h3>{{ t('suseai.apps.noApps', 'No applications found') }}</h3>
          <p class="text-muted">{{ search ? t('suseai.apps.noAppsDesc', 'Try adjusting your search or filter.') : t('suseai.apps.noAppsLibrary', 'No applications are available in the selected library.') }}</p>
        </div>
      </div>
    </div>
  </main>
</template>

<script lang="ts">
import { defineComponent, computed, getCurrentInstance, onMounted, ref } from 'vue';
import type { RouteLocationRaw } from 'vue-router';
import { useT } from '../composables/useT';
import type { AppCollectionItem } from '../services/app-collection';
import { fetchSuseAiApps, fetchNvidiaApps, fetchSettingsOrNull, getClusterRepoNameFromUrl } from '../services/app-collection';

export default defineComponent({
  name: 'SuseAIApps',

  setup() {
    const vm = getCurrentInstance();
    const $router = (vm as any)?.proxy?.$router;
    const store = (vm as any)?.proxy?.$store;
    const route = (vm as any)?.proxy?.$route;
    const currentClusterId = (route?.params?.cluster as string) || 'local';

    // State
    const loading = ref(true);
    const error = ref<string | null>(null);
    const search = ref('');
    const selectedRepo = ref('suse-ai');
    const viewMode = ref('tiles');
    const items = ref<AppCollectionItem[]>([]);
    const settingsData = ref<Record<string, any> | null | undefined>(undefined); // undefined=not loaded, null=no Settings CR, object=settings

    const repositoryOptions = computed(() => [
      { label: 'SUSE AI Library', value: 'suse-ai' },
      { label: 'Nvidia Library', value: 'nvidia' },
    ]);

    const hasRegistryConfigured = computed(() => {
      const spec = settingsData.value?.spec;
      if (!spec) return false;
      return !!(
        (spec.applicationCollection?.userSecretRef && spec.applicationCollection?.tokenSecretRef) ||
        (spec.suseRegistry?.userSecretRef && spec.suseRegistry?.tokenSecretRef) ||
        (spec.nvidia?.userSecretRef && spec.nvidia?.tokenSecretRef)
      );
    });

    const filteredApps = computed(() => {
      let arr = items.value.slice();

      // Filter by the selected library ('suse-ai' | 'nvidia'). A falsy value
      // (e.g. a future "All libraries" option) intentionally shows everything.
      if (selectedRepo.value) {
        arr = arr.filter((app: AppCollectionItem) => app.library === selectedRepo.value);
      }

      if (search.value) {
        const searchLower = search.value.toLowerCase();
        arr = arr.filter((app: AppCollectionItem) =>
          app.name.toLowerCase().includes(searchLower) ||
          app.description?.toLowerCase().includes(searchLower) ||
          app.slug_name.toLowerCase().includes(searchLower)
        );
      }

      return arr;
    });

    // Methods
    const getBadgeClass = (format: string) => {
      return format === 'HELM_CHART' ? 'bg-success' : 'bg-info';
    };

    const formatPackagingType = (format: string) => {
      return format === 'HELM_CHART' ? 'Helm' : 'Container';
    };

    const logoFor = (item: AppCollectionItem): string => {
      return item.logo_url || require('../assets/generic-app.svg');
    };

    const onImgError = (event: Event) => {
      const img = event.target as HTMLImageElement;
      img.src = require('../assets/generic-app.svg');
    };

    const refresh = async () => {
      loading.value = true;
      error.value = null;
      try {
        await loadApps();
      } catch (err) {
        console.error('Failed to refresh:', err);
        error.value = 'Failed to refresh applications';
      } finally {
        loading.value = false;
      }
    };

    const loadApps = async () => {
      try {
        const settings = await fetchSettingsOrNull();
        settingsData.value = settings;
        const [suseApps, nvidiaApps] = await Promise.all([
          fetchSuseAiApps(store, settings),
          fetchNvidiaApps(store, settings),
        ]);
        items.value = [...suseApps, ...nvidiaApps];
      } catch (err) {
        console.error('Failed to load apps:', err);
        throw err;
      }
    };

    const goToSettings = () => {
      $router.push({ name: 'c-cluster-suseai-settings', params: { cluster: currentClusterId } })
        .catch((err: any) => {
          if (err?.name !== 'NavigationDuplicated') console.warn('Navigation failed:', err);
        });
    };

    const onTileClick = async (app: AppCollectionItem) => {
      const route: RouteLocationRaw = {
        name:   `c-cluster-suseai-install`,
        params: {
          cluster: currentClusterId,
          slug:    app.slug_name,
        },
        query: { n: app.name },
      };

      if (app.repository_url) {
        const repoName = await getClusterRepoNameFromUrl(store, app.repository_url);
        if (repoName) {
          route.query = { ...route.query, repo: repoName };
        }
      }

      await $router.push(route);
    };

    // Initialize
    onMounted(() => {
      refresh();
    });

    const t = useT();

    return {
      // State
      loading,
      error,
      search,
      selectedRepo,
      repositoryOptions,
      viewMode,
      items,
      filteredApps,
      settingsData,
      hasRegistryConfigured,

      // Methods
      refresh,
      onTileClick,
      getBadgeClass,
      formatPackagingType,
      logoFor,
      onImgError,
      goToSettings,
      t
    };
  }
});
</script>

<style lang="scss" scoped>
// Main container
.suse-ai-apps {
  background: #ffffff;
  min-height: 100vh;
  padding: 20px 24px;
}

// Keyframes for animations
@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

// Main layout with Rancher-style refinements
.fixed-header {
  margin-bottom: 30px;

  .page-header {
    margin-bottom: 20px;

    .primary-title {
      margin: 0;
      font-size: 28px;
      font-weight: 600;
      color: #374151;
      letter-spacing: -0.025em;
    }
  }

  .actions-container {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: nowrap;
    min-height: 40px;

    .search-box {
      .input-sm {
        width: 200px;
        height: 32px;
        padding: 0 12px;
        border: 1px solid var(--border);
        border-radius: var(--border-radius);
        background: var(--input-bg);
        color: var(--body-text);
        font-size: 14px;
        transition: all 0.15s ease;

        &::placeholder {
          color: var(--muted);
        }

        &:focus {
          outline: none;
          border-color: var(--outline);
          box-shadow: 0 0 0 2px var(--primary-keyboard-focus);
        }
      }
    }

    .filter-group {
      .form-control {
        min-width: 140px;
        width: 140px;
        height: 32px;
        padding: 0 12px;
        border: 1px solid var(--border);
        border-radius: var(--border-radius);
        background: var(--input-bg);
        color: var(--body-text);
        font-size: 14px;
        font-weight: 400;
        transition: all 0.15s ease;
        appearance: none;
        background-image: url("data:image/svg+xml;charset=US-ASCII,<svg xmlns='http://www.w3.org/2000/svg' width='4' height='5'><path fill='%23666' d='m0 1 2 2 2-2z'/></svg>");
        background-repeat: no-repeat;
        background-position: right 8px center;
        background-size: 12px;

        &:focus {
          outline: none;
          border-color: var(--outline);
          box-shadow: 0 0 0 2px var(--primary-keyboard-focus);
        }
      }
    }

    .view-controls {
      display: flex;
      border: 1px solid var(--border);
      border-radius: var(--border-radius);
      overflow: hidden;
      background: var(--input-bg);
      margin-left: auto;

      .btn {
        border: none;
        background: transparent;
        padding: 6px 10px;
        min-width: 32px;
        height: 32px;
        color: var(--muted);
        transition: all 0.15s ease;

        &.role-primary {
          background: var(--primary);
          color: var(--primary-text);
        }

        &.role-secondary {
          &:hover {
            background: var(--hover-bg);
            color: var(--body-text);
          }
        }

        &:not(:last-child) {
          border-right: 1px solid var(--border);
        }
      }
    }

    .btn.role-primary {
      background: var(--primary);
      color: var(--primary-text);
      border: 1px solid var(--primary);
      border-radius: var(--border-radius);
      padding: 0 16px;
      height: 32px;
      font-weight: 500;
      font-size: 13px;
      transition: all 0.15s ease;

      &:hover {
        background: var(--primary);
        border-color: var(--primary);
        filter: brightness(0.9);
        transform: translateY(-1px);
        box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
      }

      &:disabled {
        background: var(--disabled-bg);
        border-color: var(--disabled-bg);
        cursor: not-allowed;
      }
    }
  }
}

// Accessibility helpers
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

.results-summary {
  padding: 8px 0 16px;
  color: var(--muted);
  font-size: 14px;
  font-weight: 500;
}

// Inline loading (no layout shift)
.inline-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-weight: 500;

  .icon-spinner {
    font-size: 16px;
  }
}

.repo-loading {
  margin: 20px 0;

  .loading-banner {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 16px;
    background: var(--accent-btn);
    border: 1px solid var(--border);
    border-radius: var(--border-radius);
    color: var(--body-text);
    font-size: 14px;
  }
}

.loading-text {
  margin-left: 12px;
  color: var(--muted);
  font-size: 13px;
  font-style: italic;

  .small-spinner {
    font-size: 12px;
    margin-right: 4px;
  }
}

.banner {
  margin-bottom: 20px;
  padding: 12px 16px;
  border-radius: 4px;

  &.bg-error {
    background-color: var(--error-banner-bg);
    border: 1px solid var(--error);
    color: var(--error);
  }
}

// Tiles view - fluid grid similar to Rancher catalog tiles
.tiles-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 20px;
  align-items: stretch;

  @media (max-width: 768px) {
    grid-template-columns: 1fr;
  }
}

.app-tile {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: 14px;
  background: transparent;
  padding: 20px;
  gap: 16px;
  min-height: 220px;
  transition: border-color 0.2s ease, background 0.2s ease;

  &:hover {
    border-color: var(--primary);
    background: transparent;
  }

  &.clickable-tile {
    cursor: pointer;

    &:focus {
      outline: 2px solid var(--primary);
      outline-offset: 2px;
    }
  }

  .tile-header {
    display: flex;
    align-items: flex-start;
    gap: 16px;
    padding: 0;
    border-bottom: none;

    .tile-logo {
      width: 52px;
      height: 52px;
      object-fit: contain;
      border-radius: 12px;
      background: var(--accent-btn);
      border: 1px solid var(--border, #e5e7eb);
      flex-shrink: 0;
      padding: 8px;
      box-shadow: 0 2px 4px rgba(15, 23, 42, 0.08);
    }

    .tile-info {
      flex: 1;
      min-width: 0;
    }
  }

  .tile-title-row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }

  .tile-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    line-height: 1.4;
    color: var(--body-text);
  }

  .tile-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    margin-top: 6px;
    color: var(--muted);
    font-size: 12px;
  }

  .tile-meta-item {
    display: inline-flex;
    align-items: center;
    font-weight: 500;
  }

  .tile-meta-item + .tile-meta-item {
    position: relative;
    padding-left: 12px;

    &::before {
      content: '•';
      position: absolute;
      left: 2px;
      color: var(--muted);
    }
  }

  .tile-content {
    flex: 1;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

.tile-description {
  margin: 0;
  color: var(--body-text);
  line-height: 1.5;
  font-size: 14px;
  flex: 1;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
    overflow: hidden;
  }
}

// List view (table)
.list-view {
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

      &.text-right {
        text-align: right;
      }
    }

    td {
      padding: 12px;
      border-bottom: 1px solid var(--border);
      vertical-align: middle;

      &.text-right {
        text-align: right;
      }

      &.text-center {
        text-align: center;
      }
    }

    tr:last-child td {
      border-bottom: none;
    }

    .main-row {
      transition: background-color 0.2s ease;

      &:hover {
        background: var(--sortable-table-accent-bg);
      }

      &.clickable-row {
        cursor: pointer;

        &:focus {
          outline: 2px solid var(--primary);
          outline-offset: -2px;
        }

        .col-actions {
          .icon-chevron-right {
            color: var(--muted);
            font-size: 16px;
          }
        }
      }
    }

    .empty-row {
      td {
        padding: 40px 12px;
        text-align: center;
        color: var(--muted);
        font-style: italic;
      }
    }
  }

  .name-cell {
    display: flex;
    align-items: center;
    gap: 12px;

    .table-logo {
      width: 32px;
      height: 32px;
      object-fit: contain;
      border-radius: 4px;
      background: var(--accent-btn);
      flex-shrink: 0;
    }

    .name-info {
      .app-name {
        font-weight: 600;
        color: var(--body-text);
        margin-bottom: 2px;
      }

      .app-meta {
        display: flex;
        gap: 6px;
      }
    }
  }

  .btn-group {
    display: flex;
    gap: 4px;
  }

  .list-description {
    display: inline-block;
    color: var(--body-text);
    font-size: 14px;
    line-height: 1.5;
  }
}

.badge-state {
  display: inline-block;
  padding: 4px 10px;
  font-size: 11px;
  border-radius: 16px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  border: none;

  &.badge-sm {
    padding: 3px 8px;
    font-size: 10px;
    border-radius: 12px;
  }

  &.bg-success {
    background: var(--success-banner-bg, #dcfce7);
    color: var(--success, #166534);
  }

  &.bg-info {
    background: var(--info-banner-bg, #dbeafe);
    color: var(--info, #1d4ed8);
  }

  &.bg-warning {
    background: var(--warning-banner-bg, #fef3c7);
    color: var(--warning, #d97706);
  }
}

// Button styling to match Rancher
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 14px;
  height: 32px;
  border-radius: 6px;
  font-weight: 500;
  font-size: 13px;
  line-height: 1;
  cursor: pointer;
  transition: all 0.15s ease;
  border: 1px solid;
  text-decoration: none;

  &.btn-sm {
    height: 28px;
    padding: 0 12px;
    font-size: 12px;
  }

  &.role-primary {
    background: var(--primary);
    border-color: var(--primary);
    color: var(--primary-text);

    &:hover {
      background: var(--primary);
      border-color: var(--primary);
      filter: brightness(0.9);
      transform: translateY(-1px);
      box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
    }

    &:disabled {
      background: var(--disabled-bg);
      border-color: var(--disabled-bg);
      cursor: not-allowed;
      opacity: 0.6;
    }
  }

  &.role-secondary {
    background: var(--body-bg);
    border-color: var(--border);
    color: var(--body-text);

    &:hover {
      background: var(--hover-bg);
      border-color: var(--muted);
    }

    &:disabled {
      background: var(--disabled-bg);
      border-color: var(--border);
      color: var(--disabled-text);
      cursor: not-allowed;
    }
  }

  &.btn-loading {
    position: relative;
    color: transparent;

    .icon-spinner {
      position: absolute;
      top: 50%;
      left: 50%;
      transform: translate(-50%, -50%);
      color: currentColor;
    }
  }

  .icon {
    font-size: 14px;

    &.icon-spinner {
      animation: spin 1s linear infinite;
    }
  }
}

// Empty state content
.empty-state-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 60px 20px;
  max-width: 400px;
  margin: 0 auto;

  .icon-4x {
    font-size: 64px;
    margin-bottom: 20px;
    opacity: 0.5;
  }

  h3 {
    margin: 0 0 12px 0;
    color: var(--body-text);
    font-size: 20px;
    font-weight: 600;
  }

  p {
    margin: 0;
    color: var(--muted);
    line-height: 1.5;
  }

  .empty-state-link {
    color: var(--primary);
    cursor: pointer;
    text-decoration: underline;
  }
}

// Responsive
@media (max-width: 1024px) {
  .fixed-header {
    .actions-container {
      gap: 8px;

      .search-box .input-sm {
        width: 200px;
      }

      .filter-group .form-control {
        min-width: 160px;
      }
    }
  }
}

@media (max-width: 768px) {
  .fixed-header {
    .page-header {
      .primary-title {
        font-size: 24px;
      }
    }

    .actions-container {
      flex-direction: column;
      align-items: stretch;
      gap: 12px;

      .search-box .input-sm {
        width: 100%;
      }

      .filter-group .form-control {
        width: 100%;
        min-width: 0;
      }

      .view-controls {
        margin-left: 0;
        align-self: center;
      }
    }
  }
}

/* Custom view toggle icons */
.view-icon-grid:before {
  content: "⊞";
  font-size: 18px;
  font-weight: bold;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
  position: relative;
  top: -3px;
}

.view-icon-list:before {
  content: "☰";
  font-size: 18px;
  font-weight: bold;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
  position: relative;
  top: -3px;
}

.icon.view-icon-grid,
.icon.view-icon-list {
  font-family: inherit;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
}

.app-tile-filler {
  visibility: hidden;
}

.tile-actions {
  display: flex;
  gap: 15px;

  .action-link {
    color: var(--muted);
    font-size: 16px;
    transition: color 0.2s ease;

    &:hover {
      color: var(--primary);
      text-decoration: none;
    }
  }
}
</style>
