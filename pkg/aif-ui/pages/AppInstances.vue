<template>
  <main class="main-layout">
    <div class="outlet">
      <!-- Header with breadcrumb and actions -->
      <header class="fixed-header">
        <!-- Breadcrumb navigation with app meta -->
        <div class="breadcrumb-nav">
          <button
            class="btn btn-link breadcrumb-link"
            @click="navigateToApps"
            :aria-label="t('suseai.apps.backToApps', 'Back to Apps')"
          >
            <i class="icon icon-chevron-left" aria-hidden="true" />
            <span>{{ t('suseai.apps.title', 'Apps') }}</span>
          </button>
          <span class="breadcrumb-separator">/</span>
          <span class="breadcrumb-current">{{ appDisplayName }}</span>
          <div v-if="appInfo" class="app-meta-inline">
            <span v-if="appInfo.packaging_format" class="badge-state" :class="getBadgeClass(appInfo.packaging_format)">
              {{ formatPackagingType(appInfo.packaging_format) }}
            </span>
            <span v-if="appInfo.version" class="app-version">v{{ appInfo.version }}</span>
          </div>
        </div>

        <!-- Actions toolbar -->
        <div class="actions-container" role="toolbar" aria-label="Instance actions">
          <!-- Left side: Search and filters -->
          <div class="left-actions">
            <div class="search-box">
              <label for="instance-search" class="sr-only">Search instances</label>
              <input
                id="instance-search"
                v-model="search"
                type="search"
                :placeholder="t('suseai.instances.search', 'Search instances')"
                class="input-sm"
                aria-label="Search instances"
              />
            </div>

            <div class="filter-group">
              <label for="cluster-filter" class="sr-only">Filter by cluster</label>
              <select
                id="cluster-filter"
                v-model="selectedCluster"
                class="form-control"
                aria-label="Filter instances by cluster"
              >
                <option value="all">All Clusters</option>
                <option v-for="cluster in availableClusters" :key="cluster" :value="cluster">
                  {{ cluster }}
                </option>
              </select>
            </div>

            <div class="filter-group">
              <label for="status-filter" class="sr-only">Filter by status</label>
              <select
                id="status-filter"
                v-model="selectedStatus"
                class="form-control"
                aria-label="Filter instances by status"
              >
                <option value="all">All Status</option>
                <option value="deployed">Deployed</option>
                <option value="installing">Installing</option>
                <option value="upgrading">Upgrading</option>
                <option value="failed">Failed</option>
              </select>
            </div>
          </div>

          <!-- Right side: Action buttons -->
          <div class="right-actions">
            <button
              class="btn role-primary"
              @click="onInstall"
              :disabled="loading"
              :title="t('suseai.instances.installNew', 'Install new instance')"
              :aria-label="t('suseai.instances.installNew', 'Install new instance')"
              type="button"
            >
              <i class="icon icon-plus" aria-hidden="true" />
              {{ t('suseai.instances.install', 'Install') }}
            </button>

            <button
              class="btn role-secondary"
              @click="refresh"
              :disabled="loading"
              :title="t('suseai.instances.refresh', 'Refresh instances')"
              :aria-label="loading ? 'Refreshing instances...' : 'Refresh instances'"
              type="button"
            >
              <i v-if="loading" class="icon icon-spinner icon-spin" aria-hidden="true" />
              <i v-else class="icon icon-refresh" aria-hidden="true" />
              {{ t('suseai.instances.refresh', 'Refresh') }}
            </button>
          </div>
        </div>
      </header>

      <!-- Error state -->
      <div v-if="error" class="banner bg-error">
        <span>{{ error }}</span>
      </div>

      <!-- Main content area -->
      <div class="main-content">
        <!-- Results summary -->
        <div class="results-summary" aria-live="polite">
          <div v-if="loading" class="inline-loading">
            <i class="icon icon-spinner icon-spin" aria-hidden="true" />
            <span>{{ t('suseai.instances.loading', 'Loading instances...') }}</span>
          </div>
          <div v-else-if="filteredInstances.length" class="results-text">
            {{ filteredInstances.length }} {{ filteredInstances.length === 1 ? 'instance' : 'instances' }} found
          </div>
          <div v-else-if="!loading && !error" class="results-text">
            No instances found
          </div>
        </div>

        <!-- Instances table -->
        <div v-if="!loading && filteredInstances.length" class="instances-table">
          <table class="table" role="table" aria-label="Application instances">
            <thead>
              <tr>
                <th scope="col">{{ t('suseai.instances.name', 'Instance Name') }}</th>
                <th scope="col">{{ t('suseai.instances.cluster', 'Cluster') }}</th>
                <th scope="col">{{ t('suseai.instances.namespace', 'Namespace') }}</th>
                <th scope="col">{{ t('suseai.instances.version', 'Version') }}</th>
                <th scope="col">{{ t('suseai.instances.status', 'Status') }}</th>
                <th scope="col" class="text-right">{{ t('suseai.instances.actions', 'Actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="instance in filteredInstances"
                :key="`${instance.clusterId}-${instance.namespace}-${instance.releaseName}`"
                class="instance-row"
              >
                <!-- Instance Name -->
                <td class="col-name">
                  <div class="instance-name-cell">
                    <div class="instance-name">{{ instance.instanceName || instance.releaseName }}</div>
                    <div v-if="instance.description" class="instance-description text-muted">
                      {{ instance.description }}
                    </div>
                  </div>
                </td>

                <!-- Cluster -->
                <td class="col-cluster">
                  <span class="cluster-name">{{ instance.clusterName || instance.clusterId }}</span>
                </td>

                <!-- Namespace -->
                <td class="col-namespace">
                  <span class="namespace-name">{{ instance.namespace }}</span>
                </td>

                <!-- Version -->
                <td class="col-version">
                  <span class="version-info">
                    {{ instance.version || instance.chartVersion || '—' }}
                  </span>
                </td>

                <!-- Status -->
                <td class="col-status">
                  <span
                    class="status-badge"
                    :class="getStatusClass(instance.status)"
                    :title="getStatusTooltip(instance)"
                  >
                    <i :class="getStatusIcon(instance.status)" aria-hidden="true" />
                    {{ getStatusLabel(instance.status) }}
                  </span>
                  <div v-if="instance.lastDeployed" class="last-deployed text-muted">
                    {{ formatDate(instance.lastDeployed) }}
                  </div>
                </td>


                <!-- Actions -->
                <td class="col-actions text-right">
                  <div class="btn-group" role="group" :aria-label="`Actions for ${instance.instanceName || instance.releaseName}`">
                    <button
                      class="btn btn-sm role-secondary manage-instance-btn"
                      @click="onManage(instance)"
                      :disabled="!canManage(instance)"
                      :title="t('suseai.instances.manage', 'Manage instance')"
                      :aria-label="`Manage ${instance.instanceName || instance.releaseName}`"
                    >
                      <i class="icon icon-edit" aria-hidden="true" />
                      {{ t('suseai.instances.manage', 'Manage') }}
                    </button>
                    <button
                      class="btn btn-sm role-secondary text-error"
                      @click="onDelete(instance)"
                      :disabled="!canDelete(instance) || deletingInstances.has(getInstanceKey(instance))"
                      :title="t('suseai.instances.delete', 'Delete instance')"
                      :aria-label="`Delete ${instance.instanceName || instance.releaseName}`"
                    >
                      <i v-if="deletingInstances.has(getInstanceKey(instance))" class="icon icon-spinner icon-spin" aria-hidden="true" />
                      <i v-else class="icon icon-delete" aria-hidden="true" />
                      <span v-if="deletingInstances.has(getInstanceKey(instance))">
                        {{ t('suseai.instances.deleting', 'Deleting...') }}
                      </span>
                      <span v-else>
                        {{ t('suseai.instances.delete', 'Delete') }}
                      </span>
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Empty state -->
        <div v-if="!loading && !filteredInstances.length && !error" class="empty-state-content">
          <i class="icon icon-folder-open icon-4x text-muted" />
          <h3>{{ t('suseai.instances.noInstances', 'No instances found') }}</h3>
          <p class="text-muted">
            {{ hasSearchOrFilter
              ? t('suseai.instances.noInstancesFiltered', 'No instances match your search criteria. Try adjusting your filters.')
              : t('suseai.instances.noInstancesAvailable', 'This application has not been installed yet.')
            }}
          </p>
          <button
            v-if="!hasSearchOrFilter"
            class="btn role-primary install-first-btn"
            @click="onInstall"
          >
            <i class="icon icon-plus" aria-hidden="true" />
            {{ t('suseai.instances.installFirst', 'Install First Instance') }}
          </button>
        </div>
      </div>
    </div>
  </main>
</template>

<script lang="ts">
import { defineComponent, computed, getCurrentInstance, onMounted, onUnmounted, ref } from 'vue';
import type { AppInstallationSummary } from '../types/app-types';
import type { AppCollectionItem } from '../services/app-collection';
import { fetchAppsFromRepository } from '../services/app-collection';
import { discoverExistingInstall, getClusters, listCatalogApps, deleteApp } from '../services/rancher-apps';
import { PRODUCT } from '../config/suseai';

interface InstanceData extends AppInstallationSummary {
  instanceName?: string;
  description?: string;
  clusterName?: string;
  chartVersion?: string;
  appVersion?: string;
}

export default defineComponent({
  name: 'AppInstances',

  props: {
    slug: {
      type: String,
      required: true
    }
  },

  setup(props) {
    const vm = getCurrentInstance();
    const $router = (vm as any)?.proxy?.$router;
    const $route = (vm as any)?.proxy?.$route;
    const store = (vm as any)?.proxy?.$store;
    const currentClusterId = ($route?.params?.cluster as string) || 'local';

    // State
    const loading = ref(true);
    const error = ref<string | null>(null);
    const search = ref('');
    const selectedCluster = ref('all');
    const selectedStatus = ref('all');
    const deletingInstances = ref<Set<string>>(new Set());
    const autoRefreshPaused = ref(false);

    // Real app info loaded from data
    const appInfo = ref<AppCollectionItem | null>(null);

    // Real instances data
    const instances = ref<InstanceData[]>([]);

    // Computed properties
    const appDisplayName = computed(() => {
      return appInfo.value?.name || props.slug;
    });

    const filteredInstances = computed(() => {
      let filtered = instances.value.slice();

      // Apply search filter
      if (search.value) {
        const searchLower = search.value.toLowerCase();
        filtered = filtered.filter(instance =>
          (instance.instanceName || instance.releaseName).toLowerCase().includes(searchLower) ||
          instance.namespace.toLowerCase().includes(searchLower) ||
          (instance.clusterName || instance.clusterId).toLowerCase().includes(searchLower)
        );
      }

      // Apply cluster filter
      if (selectedCluster.value !== 'all') {
        filtered = filtered.filter(instance =>
          instance.clusterId === selectedCluster.value
        );
      }

      // Apply status filter
      if (selectedStatus.value !== 'all') {
        filtered = filtered.filter(instance =>
          instance.status === selectedStatus.value
        );
      }

      return filtered;
    });

    const availableClusters = computed(() => {
      const clusters = new Set(instances.value.map(i => i.clusterId));
      return Array.from(clusters).sort();
    });

    const hasSearchOrFilter = computed(() => {
      return search.value || selectedCluster.value !== 'all' || selectedStatus.value !== 'all';
    });

    // Methods
    const getBadgeClass = (format: string) => {
      return format === 'HELM_CHART' ? 'bg-success' : 'bg-info';
    };

    const formatPackagingType = (format: string) => {
      return format === 'HELM_CHART' ? 'Helm' : 'Container';
    };

    const getStatusClass = (status: string) => {
      switch (status) {
        case 'deployed': return 'status-success';
        case 'installing':
        case 'upgrading': return 'status-warning';
        case 'failed': return 'status-error';
        default: return 'status-info';
      }
    };

    const getStatusIcon = (status: string) => {
      switch (status) {
        case 'deployed': return 'icon-checkmark';
        case 'installing':
        case 'upgrading': return 'icon-spinner icon-spin';
        case 'failed': return 'icon-error';
        default: return 'icon-info';
      }
    };

    const getStatusLabel = (status: string) => {
      switch (status) {
        case 'deployed': return 'Ready';
        case 'installing': return 'Installing';
        case 'upgrading': return 'Upgrading';
        case 'failed': return 'Failed';
        default: return status;
      }
    };

    const getStatusTooltip = (instance: InstanceData) => {
      if (instance.error) {
        return `Status: ${getStatusLabel(instance.status)} - ${instance.error}`;
      }
      return `Status: ${getStatusLabel(instance.status)}`;
    };

    const formatDate = (dateString: string) => {
      try {
        const date = new Date(dateString);
        return date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
      } catch {
        return dateString;
      }
    };

    const getInstanceKey = (instance: InstanceData) => {
      return `${instance.clusterId}-${instance.namespace}-${instance.releaseName}`;
    };

    const canManage = (instance: InstanceData) => {
      return ['deployed', 'failed'].includes(instance.status);
    };

    const canDelete = (instance: InstanceData) => {
      return !['installing', 'upgrading'].includes(instance.status);
    };

    const onImgError = (event: Event) => {
      const img = event.target as HTMLImageElement;
      img.src = require('../assets/generic-app.svg');
    };

    const navigateToApps = async () => {
      await $router.push({
        name: `c-cluster-suseai-apps`,
        params: { cluster: currentClusterId }
      });
    };

    const onInstall = async () => {
      // Navigate to install page for this app
      await $router.push({
        name: `c-cluster-${PRODUCT}-install`,
        params: {
          cluster: currentClusterId,
          slug: props.slug
        },
        query: {
          repo: $route.query.repo
        }
      });
    };

    const onManage = async (instance: InstanceData) => {
      // Navigate to manage page with instance context
      await $router.push({
        name: `c-cluster-${PRODUCT}-manage`,
        params: {
          cluster: currentClusterId,
          slug: props.slug
        },
        query: {
          // Pass instance context via query params
          instanceCluster: instance.clusterId,
          instanceNamespace: instance.namespace,
          instanceName: instance.releaseName,
          repo: $route.query.repo
        }
      });
    };

    const onDelete = async (instance: InstanceData) => {
      // Confirm deletion
      const confirmMessage = `Are you sure you want to delete "${instance.instanceName || instance.releaseName}"?\n\nCluster: ${instance.clusterName || instance.clusterId}\nNamespace: ${instance.namespace}\n\nThis action cannot be undone.`;

      if (!confirm(confirmMessage)) return;

      const instanceKey = getInstanceKey(instance);
      deletingInstances.value.add(instanceKey);

      try {
        console.log(`[SUSE-AI] Deleting instance: ${instance.releaseName} in ${instance.clusterId}/${instance.namespace}`);

        // Call the actual delete service
        await deleteApp(store, instance.clusterId, instance.namespace, instance.releaseName);

        console.log(`[SUSE-AI] Successfully deleted instance: ${instance.releaseName}`);

        // Remove from instances array
        const index = instances.value.findIndex(i => getInstanceKey(i) === instanceKey);
        if (index !== -1) {
          instances.value.splice(index, 1);
        }

        // Show success message
        if (store.dispatch) {
          store.dispatch('growl/success', {
            title: 'Instance Deleted',
            message: `${instance.instanceName || instance.releaseName} has been deleted successfully.`
          });
        }
      } catch (err: any) {
        console.error('Delete failed:', err);
        error.value = `Failed to delete instance: ${err?.message || 'Unknown error'}`;

        // Show error message
        if (store.dispatch) {
          store.dispatch('growl/error', {
            title: 'Delete Failed',
            message: `Failed to delete ${instance.instanceName || instance.releaseName}: ${err?.message || 'Unknown error'}`
          });
        }
      } finally {
        deletingInstances.value.delete(instanceKey);
      }
    };

    const loadAppInfo = async () => {
      try {
        // Check if repository was passed via query params
        const repoFromQuery = $route?.query?.repo as string;

        // If we have a specific repository from navigation, try that first
        if (repoFromQuery) {
          try {
            console.log(`[SUSE-AI] Loading app info from specified repository: ${repoFromQuery}`);
            const repoApps = await fetchAppsFromRepository(store, repoFromQuery);
            const repoApp = repoApps.find(a => a.slug_name === props.slug);
            if (repoApp) {
              appInfo.value = repoApp;
              console.log(`[SUSE-AI] Found app "${props.slug}" in repository "${repoFromQuery}"`);
              return;
            }
          } catch (repoErr) {
            console.warn(`Failed to load app from specified repository ${repoFromQuery}:`, repoErr);
          }
        }
      } catch (err: any) {
        console.error('Failed to load app info:', err);
        error.value = `Failed to load app information: ${err?.message || 'Unknown error'}`;
      }
    };

    // Cached cluster fetching to prevent repeated API calls
    const getClustersWithCache = async () => {
      const now = Date.now();
      if (clustersCache && (now - clustersCacheTime) < CLUSTERS_CACHE_TTL) {
        return clustersCache;
      }

      const clusters = await getClusters(store);
      clustersCache = clusters;
      clustersCacheTime = now;
      return clusters;
    };

    const loadAppInstances = async () => {
      try {
        const allClusters = await getClustersWithCache();
        const foundInstances: InstanceData[] = [];

        // Search for installations across all clusters
        for (const cluster of allClusters) {
          try {
            console.log(`[SUSE-AI] Searching for ${props.slug} instances in cluster ${cluster.name}...`);

            // Get all catalog apps in this cluster
            const catalogApps = await listCatalogApps(store, cluster.id);

            // Filter apps that match our slug
            for (const catalogApp of catalogApps) {
              const meta = catalogApp?.metadata || {};
              const spec = catalogApp?.spec || {};
              const chart = spec?.chart?.metadata?.name || spec?.chartName || '';
              const release = meta?.name || '';

              // Check if this app matches our slug (by chart name)
              const matchesChart = chart.toLowerCase() === props.slug.toLowerCase();

              if (matchesChart) {
                console.log(`[SUSE-AI] Found instance: ${release} in ${cluster.name}/${meta.namespace}`);

                // Extract status information from the catalog app
                // The API returns status in metadata.state.name and status.summary.state
                const metadataState = (catalogApp as any)?.metadata?.state?.name;
                const statusState = catalogApp?.status?.summary?.state;
                const actualStatus = statusState || metadataState || 'unknown';

                console.log(`[SUSE-AI] Instance ${release} status: metadata.state.name="${metadataState}", status.summary.state="${statusState}"`);

                // Determine instance status
                let instanceStatus: 'pending' | 'installing' | 'deployed' | 'upgrading' | 'uninstalling' | 'failed' | 'superseded' | 'unknown' = 'unknown';
                let ready = false;
                let errorMessage: string | undefined;

                // Map the actual status from Rancher to our expected values
                switch (actualStatus?.toLowerCase()) {
                  case 'deployed':
                    instanceStatus = 'deployed';
                    ready = !(catalogApp as any)?.metadata?.state?.error && !(catalogApp as any)?.metadata?.state?.transitioning;
                    break;
                  case 'failed':
                  case 'error':
                    instanceStatus = 'failed';
                    errorMessage = (catalogApp as any)?.metadata?.state?.message || 'Deployment failed';
                    break;
                  case 'installing':
                  case 'pending':
                  case 'pending-install':
                    instanceStatus = 'installing';
                    break;
                  case 'active':
                  case 'upgrading':
                  case 'pending-upgrade':
                  case 'pending-rollback':
                    instanceStatus = 'upgrading';
                    break;
                  case 'uninstalling':
                    instanceStatus = 'uninstalling';
                    break;
                  case 'superseded':
                    instanceStatus = 'superseded';
                    break;
                  default:
                    instanceStatus = 'unknown';
                    break;
                }

                // If state indicates error, override status
                if ((catalogApp as any)?.metadata?.state?.error) {
                  instanceStatus = 'failed';
                  errorMessage = (catalogApp as any)?.metadata?.state?.message || 'Application error';
                }

                const instance: InstanceData = {
                  clusterId: cluster.id,
                  clusterName: cluster.name,
                  namespace: meta.namespace || 'default',
                  releaseName: release,
                  instanceName: release, // Use release name as instance name for now
                  status: instanceStatus,
                  version: spec?.chart?.metadata?.version || spec?.version || '',
                  chartVersion: spec?.chart?.metadata?.version || spec?.version || '',
                  appVersion: spec?.chart?.metadata?.version || '', // AppVersion is usually same as chart version
                  lastDeployed: meta?.annotations?.['cattle.io/timestamp'] || '',
                  ready,
                  error: errorMessage
                };

                foundInstances.push(instance);
              }
            }
          } catch (clusterErr) {
            console.warn(`Failed to search cluster ${cluster.name}:`, clusterErr);
          }
        }

        instances.value = foundInstances;
        console.log(`[SUSE-AI] Found ${foundInstances.length} instances of ${props.slug}`);

      } catch (err: any) {
        console.error('Failed to load app instances:', err);
        error.value = `Failed to load instances: ${err?.message || 'Unknown error'}`;
      }
    };

    const refresh = async () => {
      loading.value = true;
      error.value = null;

      try {
        await Promise.all([
          loadAppInfo(),
          loadAppInstances()
        ]);
      } catch (err: any) {
        console.error('Failed to refresh:', err);
        error.value = 'Failed to refresh data';
      } finally {
        loading.value = false;
      }
    };

    // Auto-refresh functionality for real-time status updates
    let refreshTimer: NodeJS.Timeout | null = null;
    let clustersCache: any[] | null = null;
    let clustersCacheTime = 0;
    const CLUSTERS_CACHE_TTL = 60000; // Cache clusters for 1 minute

    const silentRefresh = async () => {
      // Skip refresh if paused, loading, or if any delete operations are in progress
      if (autoRefreshPaused.value || loading.value || deletingInstances.value.size > 0) {
        return;
      }

      try {
        // Don't show loading state during auto-refresh for better UX
        await Promise.all([
          loadAppInfo(),
          loadAppInstances()
        ]);
      } catch (err: any) {
        // Silently handle errors during auto-refresh to avoid spamming users
        console.warn('[SUSE-AI] Silent refresh failed:', err);
      }
    };

    // Initialize
    onMounted(() => {
      refresh();

      // Start auto-refresh timer (every 30 seconds to reduce load)
      refreshTimer = setInterval(() => {
        // Only refresh if not currently loading to prevent overlapping operations
        if (!loading.value) {
          silentRefresh();
        }
      }, 30000);
    });

    // Cleanup timer and cache on unmount
    onUnmounted(() => {
      if (refreshTimer) {
        clearInterval(refreshTimer);
        refreshTimer = null;
      }
      // Clear cache to prevent memory leaks
      clustersCache = null;
      clustersCacheTime = 0;
    });

    // Translation helper
    const t = (key: string, fallback: string) => {
      // Will be replaced with proper i18n in later phases
      return fallback;
    };

    return {
      // State
      loading,
      error,
      search,
      selectedCluster,
      selectedStatus,
      deletingInstances,

      // Computed
      appInfo,
      appDisplayName,
      filteredInstances,
      availableClusters,
      hasSearchOrFilter,

      // Methods
      getBadgeClass,
      formatPackagingType,
      getStatusClass,
      getStatusIcon,
      getStatusLabel,
      getStatusTooltip,
      formatDate,
      getInstanceKey,
      canManage,
      canDelete,
      onImgError,
      navigateToApps,
      onInstall,
      onManage,
      onDelete,
      refresh,
      t
    };
  }
});
</script>

<style lang="scss" scoped>
// Main layout following Apps.vue patterns
.main-layout {
  background: var(--body-bg, #ffffff);
  min-height: 100vh;
  padding: 20px 24px;
}

.fixed-header {
  margin-bottom: 30px;
}

// Breadcrumb navigation with inline app meta
.breadcrumb-nav {
  display: flex;
  align-items: center;
  margin-bottom: 20px;
  font-size: 14px;

  .breadcrumb-link {
    display: flex;
    align-items: center;
    color: var(--primary);
    text-decoration: none;
    font-weight: 900;
    font-size: 18px;
    padding: 0px;
    border-radius: 4px;
    transition: all 0.15s ease;
    background: transparent;

    &:hover {
      text-decoration: none;

      span {
        text-decoration: underline;
      }
    }

    .icon {
      font-size: 12px;
    }

    .icon-chevron-left {
      color: var(--muted);
    }
  }

  .breadcrumb-separator {
    margin: 0 8px;
    color: var(--muted);
  }

  .breadcrumb-current {
    color: var(--body-text);
    font-weight: 600;
    font-size: 18px;
  }

  .app-meta-inline {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-left: 16px;

    .app-version {
      color: var(--muted);
      font-weight: 500;
      font-size: 14px;
    }
  }
}

// Actions container with left/right layout
.actions-container {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 40px;

  .left-actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .right-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

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

  .btn {
    height: 32px;
    padding: 0 16px;
    font-weight: 500;
    font-size: 13px;
    border-radius: var(--border-radius);
    transition: all 0.15s ease;
    display: flex;
    align-items: center;
    gap: 6px;

    &.role-primary {
      background: var(--primary);
      color: var(--primary-text);
      border: 1px solid var(--primary);

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

    &.role-secondary {
      background: var(--body-bg);
      border: 1px solid var(--border);
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

    .icon {
      font-size: 14px;

      &.icon-spinner {
        animation: spin 1s linear infinite;
      }
    }
  }
}

// Results summary
.results-summary {
  padding: 8px 0 16px;
  color: var(--muted);
  font-size: 14px;
  font-weight: 500;
}

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

// Instances table
.instances-table {
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
    }

    tr:last-child td {
      border-bottom: none;
    }

    .instance-row {
      transition: background-color 0.2s ease;

      &:hover {
        background: var(--sortable-table-accent-bg);
      }
    }
  }

  // Table cell specific styles
  .instance-name-cell {
    .instance-name {
      font-weight: 600;
      color: var(--body-text);
      margin-bottom: 2px;
    }

    .instance-description {
      font-size: 12px;
      line-height: 1.4;
    }
  }

  .cluster-name,
  .namespace-name {
    font-family: monospace;
    background: var(--accent-btn);
    padding: 2px 6px;
    border-radius: 3px;
    font-size: 12px;
    border: 1px solid var(--border);
  }

  .version-info {
    font-weight: 500;
  }

  .app-version-info {
    font-size: 11px;
    margin-top: 2px;
  }

  .status-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 8px;
    border-radius: 12px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;

    &.status-success {
      background: var(--success-banner-bg);
      color: var(--success);
    }

    &.status-warning {
      background: var(--warning-banner-bg);
      color: var(--warning);
    }

    &.status-error {
      background: var(--error-banner-bg);
      color: var(--error);
    }

    &.status-info {
      background: var(--info-banner-bg);
      color: var(--info);
    }

    .icon {
      font-size: 10px;
    }
  }

  .last-deployed {
    font-size: 11px;
    margin-top: 2px;
  }

  .btn-group {
    display: flex;
    gap: 4px;
    justify-content: flex-end;

    .btn {
      height: 28px;
      padding: 0 12px;
      font-size: 12px;

      &.text-error {
        color: var(--error);

        &:hover {
          background: var(--error-banner-bg);
          border-color: var(--error);
        }
      }
    }

    .manage-instance-btn {
      display: flex;
      align-items: center;
      gap: 6px;
    }

    .btn.text-error .icon-delete {
      font-size: 14px;
    }

  }
}

// Badge state (reuse from Apps.vue)
.badge-state {
  display: inline-block;
  padding: 4px 10px;
  font-size: 11px;
  border-radius: 16px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  border: none;

  &.bg-success {
    background: var(--success-banner-bg);
    color: var(--success);
  }

  &.bg-info {
    background: var(--info-banner-bg);
    color: var(--info);
  }
}

// Empty state (reuse from Apps.vue)
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
    margin: 0 0 20px 0;
    color: var(--muted);
    line-height: 1.5;
  }

  .install-first-btn {
    display: flex;
    align-items: center;
    gap: 6px;
  }
}

// Error banner (reuse from Apps.vue)
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

// Keyframes for animations
@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

// Responsive design
@media (max-width: 1024px) {
  .actions-container {
    gap: 8px;

    .left-actions {
      gap: 8px;

      .search-box .input-sm {
        width: 180px;
      }

      .filter-group .form-control {
        min-width: 120px;
        width: 120px;
      }
    }

    .right-actions {
      gap: 6px;
    }
  }
}

@media (max-width: 768px) {
  .breadcrumb-nav {
    flex-wrap: wrap;
    gap: 8px;

    .app-meta-inline {
      margin-left: 0;
      margin-top: 4px;
      flex-basis: 100%;
    }
  }

  .actions-container {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;

    .left-actions,
    .right-actions {
      width: 100%;
      justify-content: stretch;
    }

    .left-actions {
      flex-direction: column;
      gap: 8px;

      .search-box .input-sm {
        width: 100%;
      }

      .filter-group .form-control {
        width: 100%;
        min-width: 0;
      }
    }

    .right-actions {
      flex-direction: row;
      justify-content: center;
    }
  }

  .instances-table {
    overflow-x: auto;

    .table {
      min-width: 800px;
    }
  }
}
</style>
