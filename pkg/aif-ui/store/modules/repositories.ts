/**
 * Repositories Store Module
 * Manages repository state, synchronization, and configuration
 * Following standard Vuex patterns for consistency
 */

import { RepositoryState, SyncRepositoryPayload } from '../types/state-types';
import { RepositoryResourceData } from '../../models/cluster/repository-resource';

// === Initial State ===
function createInitialState(): RepositoryState {
  return {
    repositories: {},
    repositoriesByCluster: {},
    syncing: {},
    lastSyncCheck: null
  };
}

// === Getters ===
const getters = {
  allRepositories: (state: RepositoryState): RepositoryResourceData[] => {
    return Object.values(state.repositories).map(repoInfo => ({
      name: repoInfo.name,
      url: repoInfo.url,
      type: repoInfo.type,
      description: repoInfo.description,
      credentials: repoInfo.credentials,
      enabled: true, // Default to enabled for now
      ready: repoInfo.syncStatus === 'synced',
      error: repoInfo.syncError || undefined,
      stats: {
        chartCount: repoInfo.appsCount,
        lastIndexed: repoInfo.lastSynced || undefined,
        popularCharts: []
      },
      createdAt: repoInfo.createdAt,
      updatedAt: repoInfo.updatedAt
    }));
  },
  
  repositoryByName: (state: RepositoryState) => (name: string): RepositoryResourceData | null => {
    const repoInfo = state.repositories[name];
    if (!repoInfo) return null;
    
    return {
      name: repoInfo.name,
      url: repoInfo.url,
      type: repoInfo.type,
      description: repoInfo.description,
      credentials: repoInfo.credentials,
      enabled: true, // Default to enabled for now
      ready: repoInfo.syncStatus === 'synced',
      error: repoInfo.syncError || undefined,
      stats: {
        chartCount: repoInfo.appsCount,
        lastIndexed: repoInfo.lastSynced || undefined,
        popularCharts: []
      },
      createdAt: repoInfo.createdAt,
      updatedAt: repoInfo.updatedAt
    };
  },
  
  repositoriesForCluster: (state: RepositoryState) => (clusterId: string): RepositoryResourceData[] => {
    const repoNames = state.repositoriesByCluster[clusterId] || [];
    return repoNames.map(name => getters.repositoryByName(state)(name)).filter((repo): repo is RepositoryResourceData => repo !== null);
  },
  
  activeSyncOperations: (state: RepositoryState): Record<string, any> => {
    return state.syncing;
  },
  
  isSyncing: (state: RepositoryState) => (repoName: string): boolean => {
    return !!state.syncing[repoName];
  },
  
  syncProgress: (state: RepositoryState) => (repoName: string): number => {
    const syncOp = state.syncing[repoName];
    return syncOp ? syncOp.progress : 0;
  }
};

// === Mutations ===
const mutations = {
  SET_REPOSITORIES(state: RepositoryState, repositories: RepositoryResourceData[]) {
    // Clear existing state
    state.repositories = {};
    state.repositoriesByCluster = {};
    
    // Add repositories
    repositories.forEach(repo => {
      mutations.SET_REPOSITORY(state, repo);
    });
    
    state.lastSyncCheck = new Date().toISOString();
  },
  
  SET_REPOSITORY(state: RepositoryState, repository: RepositoryResourceData) {
    const repoInfo = {
      ...repository,
      clustersWithRepo: [],
      appsCount: repository.stats?.chartCount || 0,
      lastSynced: repository.stats?.lastIndexed || null,
      syncStatus: repository.ready ? 'synced' as const : 'unknown' as const,
      syncError: repository.error || null
    };
    
    state.repositories[repository.name] = repoInfo;
    
    // For now, assume repositories are available on all clusters
    // In reality, you'd need to check which clusters have this repo configured
    // This would be populated when fetching from actual cluster data
  },
  
  UPDATE_REPOSITORY_STATUS(state: RepositoryState, payload: { repoName: string; status: string; lastSynced?: string }) {
    const repo = state.repositories[payload.repoName];
    if (repo) {
      repo.syncStatus = payload.status as any;
      if (payload.lastSynced) {
        repo.lastSynced = payload.lastSynced;
      }
      repo.updatedAt = new Date().toISOString();
    }
  },
  
  START_SYNC_OPERATION(state: RepositoryState, operation: any) {
    state.syncing[operation.repoName] = {
      repoName: operation.repoName,
      clusterId: operation.clusterId,
      progress: 0,
      stage: 'Initializing sync...',
      startedAt: new Date().toISOString(),
      error: null
    };
  },
  
  UPDATE_SYNC_PROGRESS(state: RepositoryState, payload: { repoName: string; progress: number; stage: string }) {
    const syncOp = state.syncing[payload.repoName];
    if (syncOp) {
      syncOp.progress = payload.progress;
      syncOp.stage = payload.stage;
    }
  },
  
  COMPLETE_SYNC_OPERATION(state: RepositoryState, payload: { repoName: string; success: boolean; error?: string }) {
    const syncOp = state.syncing[payload.repoName];
    if (syncOp) {
      if (payload.success) {
        syncOp.progress = 100;
        syncOp.stage = 'Sync completed';
        
        // Update repository status
        const repo = state.repositories[payload.repoName];
        if (repo) {
          repo.syncStatus = 'synced';
          repo.lastSynced = new Date().toISOString();
          repo.syncError = null;
        }
        
        // Remove operation after delay
        setTimeout(() => {
          delete state.syncing[payload.repoName];
        }, 3000);
      } else {
        syncOp.error = payload.error || 'Sync failed';
        syncOp.stage = 'Sync failed';
        
        // Update repository status
        const repo = state.repositories[payload.repoName];
        if (repo) {
          repo.syncStatus = 'failed';
          repo.syncError = payload.error || 'Sync failed';
        }
      }
    }
  }
};

// === Actions ===
const actions = {
  async fetchRepositories({ commit }: any, clusterId?: string) {
    try {
      // Get Helm repositories from cluster(s)
      let repositories: RepositoryResourceData[] = [];
      
      if (clusterId) {
        // Fetch repositories for specific cluster
        repositories = await actions.fetchRepositoriesForCluster({ commit }, clusterId);
      } else {
        // Fetch repositories for all clusters
        const clusters: string[] = []; // Will get from rootState when integrated
        
        for (const cId of clusters) {
          const clusterRepos = await actions.fetchRepositoriesForCluster({ commit }, cId);
          
          // Merge repositories, avoiding duplicates
          clusterRepos.forEach(repo => {
            const existing = repositories.find(r => r.name === repo.name && r.url === repo.url);
            if (!existing) {
              repositories.push(repo);
            }
          });
        }
      }
      
      commit('SET_REPOSITORIES', repositories);
      
    } catch (error) {
      console.error('Failed to fetch repositories:', error);
      throw error;
    }
  },
  
  async fetchRepositoriesForCluster({ commit }: any, clusterId: string): Promise<RepositoryResourceData[]> {
    try {
      // Look for Rancher catalog repositories (simplified)
      // For now, return empty array until integrated with Rancher store
      const catalogRepos: any[] = [];
      
      const repositories: RepositoryResourceData[] = catalogRepos.map((repo: any) => ({
        name: repo.metadata.name,
        displayName: repo.metadata.name,
        description: repo.metadata.annotations?.['field.cattle.io/description'] || '',
        url: repo.spec.url,
        type: 'helm' as const,
        enabled: true,
        ready: repo.status?.conditions?.find((c: any) => c.type === 'Downloaded')?.status === 'True',
        credentials: repo.spec.auth ? {
          username: repo.spec.auth.username,
          // Don't expose password
        } : undefined,
        syncInterval: repo.spec.forceUpdate || '24h',
        lastSync: repo.status?.lastRefreshTimestamp,
        // Status determined by ready field instead of separate status property
        error: repo.status?.conditions?.find((c: any) => c.type === 'Downloaded' && c.status !== 'True')?.message,
        stats: {
          chartCount: repo.status?.downloadTime ? 0 : 0,
          popularCharts: []
        },
        createdAt: repo.metadata.creationTimestamp,
        updatedAt: repo.status?.lastRefreshTimestamp || repo.metadata.creationTimestamp
      }));
      
      // Also look for legacy app catalogs (simplified)
      // For now, skip legacy catalog check
      
      // Add built-in repositories
      const builtInRepos: RepositoryResourceData[] = [
        {
          name: 'rancher-charts',
          displayName: 'Rancher Charts',
          url: 'https://releases.rancher.com/server-charts/stable',
          type: 'helm' as const,
          description: 'Rancher official charts',
          enabled: true,
          ready: true,
          stats: {
            chartCount: 0,
            popularCharts: []
          },
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString()
        },
        {
          name: 'rancher-partner-charts',
          displayName: 'Rancher Partner Charts',
          url: 'https://releases.rancher.com/server-charts/partner',
          type: 'helm' as const,
          description: 'Rancher partner charts',
          enabled: true,
          ready: true,
          stats: {
            chartCount: 0,
            popularCharts: []
          },
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString()
        }
      ];
      
      builtInRepos.forEach(repo => {
        const existing = repositories.find(r => r.name === repo.name);
        if (!existing) {
          repositories.push(repo);
        }
      });
      
      return repositories;
      
    } catch (error) {
      console.error(`Failed to fetch repositories for cluster ${clusterId}:`, error);
      return [];
    }
  },
  
  async syncRepository({ commit }: any, payload: SyncRepositoryPayload) {
    commit('START_SYNC_OPERATION', payload);
    
    try {
      commit('UPDATE_SYNC_PROGRESS', {
        repoName: payload.repoName,
        progress: 20,
        stage: 'Connecting to repository...'
      });
      
      // Find the repository resource (simplified)
      // For now, simulate repository sync
      commit('UPDATE_SYNC_PROGRESS', {
        repoName: payload.repoName,
        progress: 50,
        stage: 'Refreshing repository index...'
      });
      
      // Simulate repository refresh
      await new Promise(resolve => setTimeout(resolve, 1000));
      
      commit('UPDATE_SYNC_PROGRESS', {
        repoName: payload.repoName,
        progress: 80,
        stage: 'Updating chart index...'
      });
      
      // Wait for refresh to complete (simplified)
      await new Promise(resolve => setTimeout(resolve, 2000));
      
      commit('COMPLETE_SYNC_OPERATION', {
        repoName: payload.repoName,
        success: true
      });
      
    } catch (error: any) {
      console.error(`Failed to sync repository ${payload.repoName}:`, error);
      commit('COMPLETE_SYNC_OPERATION', {
        repoName: payload.repoName,
        success: false,
        error: error.message || 'Sync failed'
      });
      throw error;
    }
  },
  
  async addRepository({ commit, dispatch }: any, repository: RepositoryResourceData) {
    try {
      // Create new ClusterRepo resource
      const resource: any = {
        type: 'catalog.cattle.io.clusterrepo',
        metadata: {
          name: repository.name
        },
        spec: {
          url: repository.url,
          gitRepo: repository.type === 'git' ? repository.url : undefined,
          helmRepo: repository.type === 'helm' ? repository.url : undefined
        }
      };
      
      if (repository.credentials) {
        resource.spec.auth = {
          username: repository.credentials.username,
          password: repository.credentials.password
        };
      }
      
      // Create repository resource (simplified)
      // await this.$store.dispatch(`cluster/create`, resource, { root: true });
      
      // Refresh repositories
      await dispatch('fetchRepositories');
      
    } catch (error: any) {
      console.error(`Failed to add repository ${repository.name}:`, error);
      throw error;
    }
  },
  
  async removeRepository({ commit, dispatch }: any, payload: { repoName: string; clusterId: string }) {
    try {
      // Find and remove repository resource (simplified)
      // For now, skip actual removal
      console.log(`Would remove repository ${payload.repoName}`);
      
      // Refresh repositories
      await dispatch('fetchRepositories');
      
    } catch (error: any) {
      console.error(`Failed to remove repository ${payload.repoName}:`, error);
      throw error;
    }
  },
  
  async refreshRepositories({ dispatch }: any) {
    await dispatch('fetchRepositories', { force: true });
  }
};

// === Module Definition ===
const repositoriesModule = {
  namespaced: true,
  state: createInitialState,
  getters,
  mutations,
  actions
};

export default repositoriesModule;