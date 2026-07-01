/**
 * Apps Store Module
 * Manages application state including discovery, filtering, and search functionality
 * Following standard Vuex patterns for consistency
 */

import { AppState } from '../types/state-types';
import { AppSummary, AppInstallationInfo } from '../../types/app-types';
import AppResource from '../../models/app/app-resource';
import { fetchSuseAiApps } from '../../services/app-collection';

// === Initial State ===
function createInitialState(): AppState {
  return {
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
  };
}

// === Getters ===
const getters = {
  allApps: (state: AppState): AppSummary[] => {
    return Object.values(state.apps);
  },
  
  appById: (state: AppState) => (id: string): AppSummary | null => {
    return state.apps[id] || null;
  },
  
  appsByCategory: (state: AppState): Record<string, AppSummary[]> => {
    const result: Record<string, AppSummary[]> = {};
    
    Object.values(state.apps).forEach(app => {
      const category = app.category || 'Other';
      if (!result[category]) {
        result[category] = [];
      }
      result[category].push(app);
    });
    
    return result;
  },
  
  appsByRepository: (state: AppState): Record<string, AppSummary[]> => {
    const result: Record<string, AppSummary[]> = {};
    
    Object.values(state.apps).forEach(app => {
      const repo = app.repository?.name || 'Unknown';
      if (!result[repo]) {
        result[repo] = [];
      }
      result[repo].push(app);
    });
    
    return result;
  },
  
  installedApps: (state: AppState, getters: any, rootState: any): AppSummary[] => {
    return Object.values(state.apps).filter(app => app.flags.isInstalled);
  },
  
  availableApps: (state: AppState, getters: any, rootState: any): AppSummary[] => {
    return Object.values(state.apps).filter(app => !app.flags.isInstalled);
  },
  
  filteredApps: (state: AppState): AppSummary[] => {
    let apps = Object.values(state.apps);
    const filters = state.filters;
    
    // Search text filter
    if (filters.searchText) {
      const searchTerm = filters.searchText.toLowerCase();
      apps = apps.filter(app => 
        app.name.toLowerCase().includes(searchTerm) ||
        app.displayName?.toLowerCase().includes(searchTerm) ||
        app.description?.toLowerCase().includes(searchTerm) ||
        (app.keywords || []).some(keyword => keyword.toLowerCase().includes(searchTerm))
      );
    }
    
    // Category filter
    if (filters.categories.length > 0) {
      apps = apps.filter(app => 
        filters.categories.includes(app.category || 'Other')
      );
    }
    
    // Repository filter
    if (filters.repositories.length > 0) {
      apps = apps.filter(app => 
        filters.repositories.includes(app.repository.name)
      );
    }
    
    // Status filter
    if (filters.statuses.length > 0) {
      apps = apps.filter(app => 
        filters.statuses.includes(app.status)
      );
    }
    
    // Cluster filter
    if (filters.clusters.length > 0) {
      apps = apps.filter(app => {
        const appInstallations = app.installations || [];
        return appInstallations.some(installation => 
          filters.clusters.includes(installation.clusterId)
        );
      });
    }
    
    // Only installed filter
    if (filters.onlyInstalled) {
      apps = apps.filter(app => app.flags.isInstalled);
    }
    
    // Only favorites filter (placeholder - you'd need to implement favorites)
    if (filters.onlyFavorites) {
      // apps = apps.filter(app => app.flags.isFavorite);
    }
    
    return apps;
  },
  
  searchResults: (state: AppState): AppSummary[] => {
    if (!state.filters.searchText) {
      return [];
    }
    
    const searchTerm = state.filters.searchText.toLowerCase();
    const results = new Set<AppSummary>();
    
    // Search by name
    Object.entries(state.searchIndex.byName).forEach(([name, appIds]) => {
      if (name.includes(searchTerm)) {
        appIds.forEach(id => {
          const app = state.apps[id];
          if (app) results.add(app);
        });
      }
    });
    
    // Search by keyword
    Object.entries(state.searchIndex.byKeyword).forEach(([keyword, appIds]) => {
      if (keyword.includes(searchTerm)) {
        appIds.forEach(id => {
          const app = state.apps[id];
          if (app) results.add(app);
        });
      }
    });
    
    // Search by description
    Object.entries(state.searchIndex.byDescription).forEach(([token, appIds]) => {
      if (token.includes(searchTerm)) {
        appIds.forEach(id => {
          const app = state.apps[id];
          if (app) results.add(app);
        });
      }
    });
    
    return Array.from(results);
  },
  
  categories: (state: AppState): string[] => {
    const categories = new Set<string>();
    Object.values(state.apps).forEach(app => {
      categories.add(app.category || 'Other');
    });
    return Array.from(categories).sort();
  },
  
  repositories: (state: AppState): string[] => {
    const repos = new Set<string>();
    Object.values(state.apps).forEach(app => {
      repos.add(app.repository.name);
    });
    return Array.from(repos).sort();
  },
  
  isLoading: (state: AppState): boolean => {
    return state.loading.loadingAll || 
           state.loading.loadingCategories ||
           Object.values(state.loading.loadingDetails).some(loading => loading) ||
           Object.values(state.loading.loadingCharts).some(loading => loading);
  },
  
  hasErrors: (state: AppState): boolean => {
    return Object.keys(state.loading.errors).length > 0;
  },
  
  getError: (state: AppState) => (appId: string): string | null => {
    return state.loading.errors[appId] || null;
  }
};

// === Mutations ===
const mutations = {
  SET_APPS(state: AppState, apps: AppSummary[]) {
    // Clear existing apps
    state.apps = {};
    state.appsByCategory = {};
    state.appsByRepository = {};
    
    // Add new apps
    apps.forEach(app => {
      state.apps[app.id] = app;
    });
    
    // Rebuild indexes
    mutations.BUILD_SEARCH_INDEX(state);
    state.lastUpdated = new Date().toISOString();
  },
  
  SET_APP(state: AppState, app: AppSummary) {
    state.apps[app.id] = app;
    mutations.BUILD_SEARCH_INDEX(state);
  },
  
  REMOVE_APP(state: AppState, appId: string) {
    delete state.apps[appId];
    mutations.BUILD_SEARCH_INDEX(state);
  },
  
  UPDATE_APP_INSTALLATIONS(state: AppState, payload: { appId: string; installations: AppInstallationInfo[] }) {
    const app = state.apps[payload.appId];
    if (app) {
      app.installations = payload.installations.map(installation => ({
        clusterId: installation.clusterId,
        clusterName: installation.clusterId, // You might want to resolve this
        namespace: installation.namespace,
        releaseName: installation.releaseName,
        status: installation.status,
        version: installation.version,
        lastDeployed: installation.lastDeployed,
        ready: installation.status === 'deployed',
        error: installation.error?.message
      }));
      
      // Update flags based on installations
      app.flags.isInstalled = payload.installations.length > 0;
      app.flags.isRunning = payload.installations.some(i => i.status === 'deployed');
      app.flags.hasFailed = payload.installations.some(i => i.status === 'failed');
    }
  },
  
  SET_FILTERS(state: AppState, filters: Partial<AppState['filters']>) {
    Object.assign(state.filters, filters);
  },
  
  SET_LOADING(state: AppState, payload: { appId?: string; loading: boolean; type?: string }) {
    const { appId, loading, type = 'details' } = payload;
    
    if (appId) {
      if (type === 'details') {
        state.loading.loadingDetails[appId] = loading;
      } else if (type === 'charts') {
        state.loading.loadingCharts[appId] = loading;
      }
    } else {
      if (type === 'all') {
        state.loading.loadingAll = loading;
      } else if (type === 'categories') {
        state.loading.loadingCategories = loading;
      }
    }
  },
  
  SET_ERROR(state: AppState, payload: { appId: string; error: string | null }) {
    if (payload.error) {
      state.loading.errors[payload.appId] = payload.error;
    } else {
      delete state.loading.errors[payload.appId];
    }
  },
  
  BUILD_SEARCH_INDEX(state: AppState) {
    // Reset index
    state.searchIndex = {
      byName: {},
      byKeyword: {},
      byDescription: {}
    };
    
    Object.values(state.apps).forEach(app => {
      // Index by name
      const names = [
        app.name.toLowerCase(),
        ...(app.displayName ? [app.displayName.toLowerCase()] : [])
      ];
      
      names.forEach(name => {
        if (!state.searchIndex.byName[name]) {
          state.searchIndex.byName[name] = [];
        }
        state.searchIndex.byName[name].push(app.id);
      });
      
      // Index by keywords
      (app.keywords || []).forEach(keyword => {
        const key = keyword.toLowerCase();
        if (!state.searchIndex.byKeyword[key]) {
          state.searchIndex.byKeyword[key] = [];
        }
        state.searchIndex.byKeyword[key].push(app.id);
      });
      
      // Index by description tokens
      if (app.description) {
        const tokens = app.description.toLowerCase().split(/\s+/).filter(token => token.length > 2);
        tokens.forEach(token => {
          if (!state.searchIndex.byDescription[token]) {
            state.searchIndex.byDescription[token] = [];
          }
          state.searchIndex.byDescription[token].push(app.id);
        });
      }
    });
  },
  
  CLEAR_CACHE(state: AppState) {
    state.apps = {};
    state.appsByCategory = {};
    state.appsByRepository = {};
    state.searchIndex = {
      byName: {},
      byKeyword: {},
      byDescription: {}
    };
    state.loading = {
      loadingAll: false,
      loadingCategories: false,
      loadingDetails: {},
      loadingCharts: {},
      errors: {}
    };
    state.lastUpdated = null;
  }
};

// === Actions ===
const actions = {
  async fetchAllApps({ commit, dispatch, rootState }: any, payload: { force?: boolean, clusterId?: string } = {}) {
    commit('SET_LOADING', { loading: true, type: 'all' });

    try {
      // Use existing service to fetch apps
      const clusterId = payload.clusterId || 'local'; // Default to local cluster
      const apps = await fetchSuseAiApps(clusterId);

      // Convert raw app data to AppSummary format
      const appSummaries: AppSummary[] = apps.map((app: any) => ({
        id: app.slug_name || app.name,
        name: app.name,
        displayName: app.display_name || app.name,
        description: app.description,
        icon: app.logo_url,
        version: app.version,
        appVersion: app.app_version,
        category: app.category || 'Other',
        repository: {
          name: app.repository || 'default',
          type: app.packaging_format === 'HELM_CHART' ? 'helm' : 'unknown'
        },
        status: 'available', // Will be updated based on installations
        state: 'active',
        installations: [],
        stats: {
          installCount: 0,
          clusterCount: 0,
          popularityScore: 0,
          successRate: 1.0
        },
        health: {
          overall: 'unknown' as const,
          ready: 0,
          total: 0,
          issueCount: 0,
          lastCheck: new Date().toISOString()
        },
        flags: {
          isInstalled: false,
          isRunning: false,
          hasFailed: false,
          isTransitioning: false,
          isOfficial: false,
          isVerified: false,
          isPopular: false,
          isDeprecated: false,
          hasUpdates: false,
          needsAttention: false
        },
        updated: app.last_updated_at || new Date().toISOString(),
        created: app.created_at
      }));
      
      commit('SET_APPS', appSummaries);
      commit('SET_LOADING', { loading: false, type: 'all' });
      
    } catch (error: any) {
      console.error('Failed to fetch apps:', error);
      commit('SET_ERROR', { appId: 'global', error: error.message || 'Failed to fetch apps' });
      commit('SET_LOADING', { loading: false, type: 'all' });
      throw error;
    }
  },
  
  async fetchApp({ commit }: any, appId: string): Promise<AppSummary> {
    commit('SET_LOADING', { appId, loading: true });
    
    try {
      // Fetch specific app details
      // For now, return placeholder data until integrated with existing services
      const app: any = null; // Placeholder - will be replaced with actual service calls
      
      if (!app) {
        throw new Error(`App ${appId} not found`);
      }
      
      // Convert to AppSummary and commit
      const appSummary: AppSummary = {
        id: app.slug_name || app.name,
        name: app.name,
        displayName: app.display_name || app.name,
        description: app.description,
        icon: app.logo_url,
        version: app.version,
        appVersion: app.app_version,
        category: app.category || 'Other',
        keywords: app.keywords || [],
        repository: {
          name: app.repository || 'default',
          type: app.packaging_format === 'HELM_CHART' ? 'helm' : 'unknown'
        },
        status: 'available',
        state: 'active',
        installations: [],
        flags: {
          isInstalled: false,
          isRunning: false,
          hasFailed: false,
          isTransitioning: false,
          isOfficial: false,
          isVerified: false,
          isPopular: false,
          isDeprecated: false,
          hasUpdates: false,
          needsAttention: false
        },
        updated: app.last_updated_at || new Date().toISOString(),
        created: app.created_at
      };
      
      commit('SET_APP', appSummary);
      commit('SET_LOADING', { appId, loading: false });
      
      return appSummary;
      
    } catch (error: any) {
      console.error(`Failed to fetch app ${appId}:`, error);
      commit('SET_ERROR', { appId, error: error.message || 'Failed to fetch app' });
      commit('SET_LOADING', { appId, loading: false });
      throw error;
    }
  },
  
  async searchApps({ commit, getters }: any, query: string): Promise<AppSummary[]> {
    // Update search filter
    commit('SET_FILTERS', { searchText: query });
    
    // Return search results
    return getters.searchResults;
  },
  
  updateFilters({ commit }: any, filters: Partial<AppState['filters']>) {
    commit('SET_FILTERS', filters);
  },
  
  async refreshApps({ dispatch }: any) {
    await dispatch('fetchAllApps', { force: true });
  },
  
  clearCache({ commit }: any) {
    commit('CLEAR_CACHE');
  }
};

// === Module Definition ===
const appsModule = {
  namespaced: true,
  state: createInitialState,
  getters,
  mutations,
  actions
};

export default appsModule;