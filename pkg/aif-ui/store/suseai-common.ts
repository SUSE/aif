/**
 * Main SUSE AI Extension Store
 * Follows standard Vuex patterns for consistency with Rancher UI extensions
 * Integrates with Rancher's store system
 */

import { SuseAIState, DiscoveryProgress, DiscoverInstallationsPayload } from './types/state-types';
import { SuseAIStore } from './types/store-types';

// Store modules
import appsModule from './modules/apps';
import clustersModule from './modules/clusters';
import installationsModule from './modules/installations';
import repositoriesModule from './modules/repositories';

// === Initial State ===
function createInitialState(): SuseAIState {
  return {
    discovery: {
      discovering: false,
      clustersProgress: {},
      totalClusters: 0,
      discoveredClusters: 0,
      totalApps: 0,
      discoveredApps: 0,
      errors: [],
      lastDiscovery: null,
      stage: 'idle'
    },
    
    installations: {
      installations: {},
      installationsByApp: {},
      installationsByCluster: {},
      installing: {},
      lastUpdated: null
    },
    
    repositories: {
      repositories: {},
      repositoriesByCluster: {},
      syncing: {},
      lastSyncCheck: null
    },
    
    apps: {
      apps: {},
      appsByCategory: {},
      appsByRepository: {},
      searchIndex: {
        byName: {},
        byKeyword: {},
        byDescription: {}
      },
      filters: {
        searchText: '',
        categories: [],
        repositories: [],
        statuses: [],
        clusters: [],
        onlyInstalled: false,
        onlyFavorites: false
      },
      loading: {
        loadingAll: false,
        loadingCategories: false,
        loadingDetails: {},
        loadingCharts: {},
        errors: {}
      },
      lastUpdated: null
    },
    
    clusters: {
      clusters: {},
      capabilities: {},
      connecting: {},
      lastUpdated: null
    },
    
    notifications: {
      notifications: [],
      unreadCount: 0,
      settings: {
        enabled: true,
        showInstallSuccess: true,
        showUpgradeSuccess: true,
        showErrors: true,
        showWarnings: true,
        autoHideAfter: 5
      }
    },
    
    settings: {
      autoRefresh: true,
      refreshInterval: 300, // 5 minutes
      defaultNamespace: 'default',
      showSystemApps: false,
      preferredView: 'table'
    },
    
    ui: {
      sidebarOpen: true,
      activeFilters: false,
      selectedApps: [],
      bulkOperationMode: false,
      currentView: 'apps',
      sortBy: 'name',
      sortDirection: 'asc'
    }
  };
}

// === Getters ===
const getters = {
  // Discovery getters
  isDiscovering: (state: SuseAIState): boolean => state.discovery.discovering,
  
  discoveryProgress: (state: SuseAIState): DiscoveryProgress => state.discovery,
  
  discoveryErrors: (state: SuseAIState) => state.discovery.errors,
  
  // Installation Info getter (specific from task list)
  installationInfo: (state: SuseAIState) => (appId: string, clusterId?: string) => {
    const installationKeys = state.installations.installationsByApp[appId] || [];
    
    if (clusterId) {
      // Find installation for specific cluster
      return installationKeys
        .map(key => state.installations.installations[key])
        .find(installation => installation && installation.clusterId === clusterId) || null;
    }
    
    // Return all installations for this app
    return installationKeys
      .map(key => state.installations.installations[key])
      .filter(Boolean);
  },

  // Installed Apps getter (specific from task list)
  installedApps: (state: SuseAIState) => {
    const installedAppIds = new Set<string>();
    Object.values(state.installations.installations).forEach(installation => {
      if (installation.status === 'deployed') {
        const appId = (installation as any).appId || installation.releaseName;
        installedAppIds.add(appId);
      }
    });
    
    return Array.from(installedAppIds)
      .map(id => state.apps.apps[id])
      .filter(Boolean);
  },

  // Apps for Cluster getter (specific from task list)  
  appsForCluster: (state: SuseAIState) => (clusterId: string) => {
    const cluster = state.clusters.clusters[clusterId];
    if (!cluster || !cluster.ready) {
      return {
        available: [],
        installed: []
      };
    }
    
    const installationKeys = state.installations.installationsByCluster[clusterId] || [];
    const installedAppIds = new Set<string>();
    
    installationKeys.forEach(key => {
      const installation = state.installations.installations[key];
      if (installation && installation.status === 'deployed') {
        const appId = (installation as any).appId || installation.releaseName;
        installedAppIds.add(appId);
      }
    });
    
    const allApps = Object.values(state.apps.apps);
    const installed = allApps.filter(app => installedAppIds.has(app.id));
    const available = allApps.filter(app => !installedAppIds.has(app.id));
    
    return {
      available,
      installed,
      total: allApps.length,
      installedCount: installed.length,
      availableCount: available.length
    };
  },

  // Cross-module getters that combine data from multiple modules
  appsWithInstallations: (state: SuseAIState) => {
    const apps = Object.values(state.apps.apps);
    return apps.filter(app => {
      const installationKeys = state.installations.installationsByApp[app.id] || [];
      return installationKeys.length > 0;
    });
  },
  
  installedAppsForCluster: (state: SuseAIState) => (clusterId: string) => {
    const installationKeys = state.installations.installationsByCluster[clusterId] || [];
    const appIds = new Set<string>();
    
    installationKeys.forEach(key => {
      const installation = state.installations.installations[key];
      if (installation && installation.status === 'deployed') {
        // Extract appId from installation (you'll need to add this field)
        const appId = installation.appId || installation.releaseName;
        appIds.add(appId);
      }
    });
    
    return Array.from(appIds).map(id => state.apps.apps[id]).filter(Boolean);
  },
  
  availableAppsForCluster: (state: SuseAIState) => (clusterId: string) => {
    const cluster = state.clusters.clusters[clusterId];
    if (!cluster || !cluster.ready) {
      return [];
    }
    
    return Object.values(state.apps.apps).filter(app => {
      // Add logic to filter apps based on cluster capabilities
      return true; // For now, return all apps
    });
  },
  
  // Statistics getters
  totalAppsCount: (state: SuseAIState): number => Object.keys(state.apps.apps).length,
  
  installedAppsCount: (state: SuseAIState): number => {
    const uniqueAppIds = new Set<string>();
    Object.values(state.installations.installations).forEach(installation => {
      if (installation.status === 'deployed') {
        const appId = (installation as any).appId || installation.releaseName;
        uniqueAppIds.add(appId);
      }
    });
    return uniqueAppIds.size;
  },
  
  availableAppsCount: (state: SuseAIState): number => {
    const installedAppIds = new Set<string>();
    Object.values(state.installations.installations).forEach(installation => {
      if (installation.status === 'deployed') {
        const appId = (installation as any).appId || installation.releaseName;
        installedAppIds.add(appId);
      }
    });
    
    return Object.keys(state.apps.apps).length - installedAppIds.size;
  },
  
  clustersWithAppsCount: (state: SuseAIState): number => {
    return Object.keys(state.installations.installationsByCluster).length;
  },
  
  // UI state getters
  isBulkMode: (state: SuseAIState): boolean => state.ui.bulkOperationMode,
  
  selectedAppsCount: (state: SuseAIState): number => state.ui.selectedApps.length,
  
  canPerformBulkOperation: (state: SuseAIState) => (operation: string): boolean => {
    if (state.ui.selectedApps.length === 0) {
      return false;
    }
    
    // Add logic based on operation type and selected apps
    switch (operation) {
      case 'install':
        return state.ui.selectedApps.some(appId => {
          const app = state.apps.apps[appId];
          return app && !app.flags.isInstalled;
        });
      case 'upgrade':
        return state.ui.selectedApps.some(appId => {
          const app = state.apps.apps[appId];
          return app && app.flags.isInstalled && app.flags.hasUpdates;
        });
      case 'uninstall':
        return state.ui.selectedApps.some(appId => {
          const app = state.apps.apps[appId];
          return app && app.flags.isInstalled;
        });
      default:
        return false;
    }
  }
};

// === Mutations ===
const mutations = {
  // Installation Info mutations (specific from task list)
  setInstallationInfo(state: SuseAIState, payload: { appId: string; clusterId: string; installation: any }) {
    const key = `${payload.clusterId}/${payload.installation.namespace}/${payload.installation.releaseName}`;
    
    // Set installation in main installations object
    state.installations.installations[key] = {
      ...payload.installation,
      appId: payload.appId,
      clusterId: payload.clusterId
    };
    
    // Update indexes
    if (!state.installations.installationsByApp[payload.appId]) {
      state.installations.installationsByApp[payload.appId] = [];
    }
    if (!state.installations.installationsByApp[payload.appId].includes(key)) {
      state.installations.installationsByApp[payload.appId].push(key);
    }
    
    if (!state.installations.installationsByCluster[payload.clusterId]) {
      state.installations.installationsByCluster[payload.clusterId] = [];
    }
    if (!state.installations.installationsByCluster[payload.clusterId].includes(key)) {
      state.installations.installationsByCluster[payload.clusterId].push(key);
    }
    
    state.installations.lastUpdated = new Date().toISOString();
  },

  // Discovery Progress mutations (specific from task list)
  setDiscoveryProgress(state: SuseAIState, progress: Partial<DiscoveryProgress>) {
    Object.assign(state.discovery, progress);
  },

  // Repositories mutations (specific from task list)  
  setRepositories(state: SuseAIState, repositories: any[]) {
    // Clear existing repositories
    state.repositories.repositories = {};
    state.repositories.repositoriesByCluster = {};
    
    // Add new repositories
    repositories.forEach(repo => {
      state.repositories.repositories[repo.name] = repo;
      
      // Add to cluster index if clusterId is provided
      if (repo.clusterId) {
        if (!state.repositories.repositoriesByCluster[repo.clusterId]) {
          state.repositories.repositoriesByCluster[repo.clusterId] = [];
        }
        if (!state.repositories.repositoriesByCluster[repo.clusterId].includes(repo.name)) {
          state.repositories.repositoriesByCluster[repo.clusterId].push(repo.name);
        }
      }
    });
    
    state.repositories.lastSyncCheck = new Date().toISOString();
  },

  // Discovery mutations
  START_DISCOVERY(state: SuseAIState) {
    state.discovery.discovering = true;
    state.discovery.stage = 'connecting';
    state.discovery.errors = [];
    state.discovery.clustersProgress = {};
    state.discovery.totalClusters = 0;
    state.discovery.discoveredClusters = 0;
    state.discovery.totalApps = 0;
    state.discovery.discoveredApps = 0;
  },
  
  UPDATE_DISCOVERY_PROGRESS(state: SuseAIState, payload: any) {
    if (payload.stage) {
      state.discovery.stage = payload.stage;
    }
    if (payload.clustersProgress) {
      Object.assign(state.discovery.clustersProgress, payload.clustersProgress);
    }
    if (payload.totalClusters !== undefined) {
      state.discovery.totalClusters = payload.totalClusters;
    }
    if (payload.discoveredClusters !== undefined) {
      state.discovery.discoveredClusters = payload.discoveredClusters;
    }
    if (payload.totalApps !== undefined) {
      state.discovery.totalApps = payload.totalApps;
    }
    if (payload.discoveredApps !== undefined) {
      state.discovery.discoveredApps = payload.discoveredApps;
    }
  },
  
  COMPLETE_DISCOVERY(state: SuseAIState, payload?: any) {
    state.discovery.discovering = false;
    state.discovery.stage = 'completed';
    state.discovery.lastDiscovery = new Date().toISOString();
    
    if (payload?.errors && payload.errors.length > 0) {
      state.discovery.errors = payload.errors;
    }
  },
  
  FAIL_DISCOVERY(state: SuseAIState, error: any) {
    state.discovery.discovering = false;
    state.discovery.stage = 'error';
    state.discovery.errors.push({
      clusterId: error.clusterId || 'unknown',
      clusterName: error.clusterName,
      stage: state.discovery.stage,
      error: error.message || 'Discovery failed',
      timestamp: new Date().toISOString(),
      retryable: error.retryable !== false
    });
  },
  
  // Settings mutations
  UPDATE_SETTINGS(state: SuseAIState, settings: Partial<SuseAIState['settings']>) {
    Object.assign(state.settings, settings);
  },
  
  // UI mutations
  SET_SIDEBAR_OPEN(state: SuseAIState, open: boolean) {
    state.ui.sidebarOpen = open;
  },
  
  TOGGLE_BULK_MODE(state: SuseAIState) {
    state.ui.bulkOperationMode = !state.ui.bulkOperationMode;
    if (!state.ui.bulkOperationMode) {
      state.ui.selectedApps = [];
    }
  },
  
  SELECT_APP(state: SuseAIState, appId: string) {
    if (!state.ui.selectedApps.includes(appId)) {
      state.ui.selectedApps.push(appId);
    }
  },
  
  DESELECT_APP(state: SuseAIState, appId: string) {
    const index = state.ui.selectedApps.indexOf(appId);
    if (index > -1) {
      state.ui.selectedApps.splice(index, 1);
    }
  },
  
  CLEAR_SELECTED_APPS(state: SuseAIState) {
    state.ui.selectedApps = [];
  },
  
  SET_CURRENT_VIEW(state: SuseAIState, view: string) {
    state.ui.currentView = view;
  },
  
  SET_SORT(state: SuseAIState, payload: { field: string; direction: 'asc' | 'desc' }) {
    state.ui.sortBy = payload.field;
    state.ui.sortDirection = payload.direction;
  }
};

// === Actions ===
const actions = {
  // Discovery Installations action (specific from task list)
  async discoverInstallations(context: any, payload: DiscoverInstallationsPayload = {}) {
    const { commit, dispatch } = context;
    
    try {
      commit('setDiscoveryProgress', { 
        stage: 'discovering-installations',
        discovering: true 
      });
      
      // Delegate to installations module
      await dispatch('installations/discoverInstallations', payload);
      
      commit('setDiscoveryProgress', { 
        stage: 'completed',
        discovering: false 
      });
      
    } catch (error: any) {
      commit('setDiscoveryProgress', { 
        stage: 'error',
        discovering: false,
        errors: [error] 
      });
      throw error;
    }
  },

  // Install action (specific from task list)
  async install(context: any, payload: any) {
    const { commit, dispatch } = context;
    
    try {
      // Delegate to installations module
      const result = await dispatch('installations/installApp', payload);
      
      // Update installation info in main store
      if (result && payload.appId && payload.clusterId) {
        commit('setInstallationInfo', {
          appId: payload.appId,
          clusterId: payload.clusterId,
          installation: {
            namespace: payload.namespace || 'default',
            releaseName: payload.releaseName || payload.appId,
            status: 'installing',
            chartVersion: payload.chartVersion,
            values: payload.values || {}
          }
        });
      }
      
      return result;
    } catch (error: any) {
      console.error('Installation failed:', error);
      throw error;
    }
  },

  // Upgrade action (specific from task list)
  async upgrade(context: any, payload: any) {
    const { commit, dispatch } = context;
    
    try {
      // Delegate to installations module
      const result = await dispatch('installations/upgradeApp', payload);
      
      // Update installation info in main store
      if (result && payload.appId && payload.clusterId) {
        commit('setInstallationInfo', {
          appId: payload.appId,
          clusterId: payload.clusterId,
          installation: {
            namespace: payload.namespace || 'default',
            releaseName: payload.releaseName || payload.appId,
            status: 'upgrading',
            chartVersion: payload.chartVersion,
            values: payload.values || {}
          }
        });
      }
      
      return result;
    } catch (error: any) {
      console.error('Upgrade failed:', error);
      throw error;
    }
  },

  // Uninstall action (specific from task list)
  async uninstall(context: any, payload: any) {
    const { commit, dispatch } = context;
    
    try {
      // Delegate to installations module
      const result = await dispatch('installations/uninstallApp', payload);
      
      // Update installation info in main store
      if (result && payload.appId && payload.clusterId) {
        commit('setInstallationInfo', {
          appId: payload.appId,
          clusterId: payload.clusterId,
          installation: {
            namespace: payload.namespace || 'default',
            releaseName: payload.releaseName || payload.appId,
            status: 'uninstalling',
            chartVersion: payload.chartVersion,
            values: payload.values || {}
          }
        });
      }
      
      return result;
    } catch (error: any) {
      console.error('Uninstall failed:', error);
      throw error;
    }
  },

  // Main discovery action - coordinates all discovery across modules
  async discoverAllData(context: any, payload: DiscoverInstallationsPayload = {}) {
    const { commit, dispatch } = context;
    commit('START_DISCOVERY');
    
    try {
      // Step 1: Discover clusters
      commit('UPDATE_DISCOVERY_PROGRESS', { stage: 'connecting' });
      await dispatch('clusters/fetchClusters');
      
      const clusters = Object.values(context.rootState.suseai.clusters.clusters);
      commit('UPDATE_DISCOVERY_PROGRESS', { 
        totalClusters: clusters.length,
        stage: 'discovering-repositories'
      });
      
      // Step 2: Discover repositories in parallel
      await dispatch('repositories/fetchRepositories');
      
      // Step 3: Discover apps
      commit('UPDATE_DISCOVERY_PROGRESS', { stage: 'discovering-apps' });
      await dispatch('apps/fetchAllApps', { force: payload.force });
      
      // Step 4: Discover installations
      commit('UPDATE_DISCOVERY_PROGRESS', { stage: 'discovering-installations' });
      await dispatch('installations/discoverInstallations', payload);
      
      // Step 5: Complete discovery
      commit('UPDATE_DISCOVERY_PROGRESS', { stage: 'processing' });
      
      commit('COMPLETE_DISCOVERY');
      
    } catch (error: any) {
      console.error('Discovery failed:', error);
      commit('FAIL_DISCOVERY', error);
      throw error;
    }
  },
  
  // Bulk operations
  async performBulkInstall({ state, dispatch }: any, payload: any) {
    const selectedApps = state.ui.selectedApps;
    const promises = selectedApps.map((appId: string) =>
      dispatch('installations/installApp', {
        appId,
        ...payload
      })
    );
    
    await Promise.allSettled(promises);
  },
  
  async performBulkUpgrade({ state, dispatch }: any, payload: any) {
    const selectedApps = state.ui.selectedApps;
    const promises = selectedApps.map((appId: string) =>
      dispatch('installations/upgradeApp', {
        appId,
        ...payload
      })
    );
    
    await Promise.allSettled(promises);
  },
  
  async performBulkUninstall({ state, dispatch }: any, payload: any) {
    const selectedApps = state.ui.selectedApps;
    const promises = selectedApps.map((appId: string) => {
      const installationKeys = state.installations.installationsByApp[appId] || [];
      return installationKeys.map((key: string) => {
        const installation = state.installations.installations[key];
        return dispatch('installations/uninstallApp', {
          appId,
          clusterId: installation.clusterId,
          namespace: installation.namespace,
          releaseName: installation.releaseName,
          ...payload
        });
      });
    }).flat();
    
    await Promise.allSettled(promises);
  },
  
  // Settings actions
  updateSettings({ commit }: any, settings: Partial<SuseAIState['settings']>) {
    commit('UPDATE_SETTINGS', settings);
  },
  
  async loadSettings({ commit }: any) {
    // Load settings from localStorage or API
    try {
      const savedSettings = localStorage.getItem('suseai-settings');
      if (savedSettings) {
        const settings = JSON.parse(savedSettings);
        commit('UPDATE_SETTINGS', settings);
      }
    } catch (error) {
      console.warn('Failed to load settings:', error);
    }
  },
  
  async saveSettings({ state }: any) {
    // Save settings to localStorage or API
    try {
      localStorage.setItem('suseai-settings', JSON.stringify(state.settings));
    } catch (error) {
      console.warn('Failed to save settings:', error);
    }
  },
  
  // Initialization
  async initialize({ dispatch }: any) {
    await dispatch('loadSettings');
    // Additional initialization logic
  },
  
  cleanup({ commit }: any) {
    // Cleanup when leaving the extension
    commit('CLEAR_SELECTED_APPS');
    // Additional cleanup logic
  }
};

// === Store Module Definition ===
const suseaiStore = {
  namespaced: true,
  state: createInitialState,
  getters,
  mutations,
  actions,
  modules: {
    apps: appsModule,
    clusters: clustersModule,
    installations: installationsModule,
    repositories: repositoriesModule
  }
};

export default suseaiStore;
export { createInitialState };