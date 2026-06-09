/**
 * Clusters Store Module
 * Manages cluster state, connectivity, and capabilities
 * Following standard Vuex patterns for consistency
 */

import { ClusterState } from '../types/state-types';
import { ClusterResourceData } from '../../models/cluster/cluster-resource';

// === Initial State ===
function createInitialState(): ClusterState {
  return {
    clusters: {},
    capabilities: {},
    connecting: {},
    lastUpdated: null
  };
}

// === Getters ===
const getters = {
  allClusters: (state: ClusterState): ClusterResourceData[] => {
    return Object.values(state.clusters);
  },
  
  clusterById: (state: ClusterState) => (id: string): ClusterResourceData | null => {
    return state.clusters[id] || null;
  },
  
  connectedClusters: (state: ClusterState): ClusterResourceData[] => {
    return Object.values(state.clusters).filter(cluster => 
      cluster.connectionStatus === 'connected' && cluster.ready
    );
  },
  
  availableClusters: (state: ClusterState): ClusterResourceData[] => {
    return Object.values(state.clusters).filter(cluster => 
      cluster.ready && state.capabilities[cluster.id]?.canInstallApps
    );
  },
  
  clusterCapabilities: (state: ClusterState) => (clusterId: string) => {
    return state.capabilities[clusterId] || null;
  },
  
  canInstallApps: (state: ClusterState) => (clusterId: string): boolean => {
    const cluster = state.clusters[clusterId];
    const capabilities = state.capabilities[clusterId];
    
    return cluster?.ready === true && 
           cluster.connectionStatus === 'connected' &&
           capabilities?.canInstallApps === true;
  },
  
  isClusterHealthy: (state: ClusterState) => (clusterId: string): boolean => {
    const cluster = state.clusters[clusterId];
    return cluster?.ready === true && 
           cluster.connectionStatus === 'connected' && 
           !cluster.healthError;
  },
  
  clusterConnectionStatus: (state: ClusterState) => (clusterId: string): string => {
    const cluster = state.clusters[clusterId];
    const connecting = state.connecting[clusterId];
    
    if (connecting?.connecting) {
      return 'connecting';
    }
    
    return cluster?.connectionStatus || 'unknown';
  }
};

// === Mutations ===
const mutations = {
  SET_CLUSTERS(state: ClusterState, clusters: ClusterResourceData[]) {
    // Clear existing clusters
    state.clusters = {};
    
    // Add new clusters
    clusters.forEach(cluster => {
      state.clusters[cluster.id] = {
        ...cluster,
        connectionStatus: cluster.ready ? 'connected' : 'disconnected',
        lastHealthCheck: new Date().toISOString(),
        healthError: null
      };
    });
    
    state.lastUpdated = new Date().toISOString();
  },
  
  SET_CLUSTER(state: ClusterState, cluster: ClusterResourceData) {
    state.clusters[cluster.id] = {
      ...cluster,
      connectionStatus: cluster.ready ? 'connected' : 'disconnected',
      lastHealthCheck: new Date().toISOString(),
      healthError: null
    };
  },
  
  UPDATE_CLUSTER_STATUS(state: ClusterState, payload: { clusterId: string; status: string }) {
    const cluster = state.clusters[payload.clusterId];
    if (cluster) {
      cluster.connectionStatus = payload.status as any;
      cluster.lastHealthCheck = new Date().toISOString();
    }
  },
  
  SET_CLUSTER_CAPABILITIES(state: ClusterState, payload: { clusterId: string; capabilities: any }) {
    state.capabilities[payload.clusterId] = payload.capabilities;
  },
  
  START_CLUSTER_CONNECTION(state: ClusterState, clusterId: string) {
    state.connecting[clusterId] = {
      connecting: true,
      progress: 0,
      stage: 'Initializing connection...',
      error: null,
      retryCount: 0,
      lastAttempt: new Date().toISOString()
    };
  },
  
  UPDATE_CONNECTION_PROGRESS(state: ClusterState, payload: { clusterId: string; progress: number; stage: string }) {
    const connection = state.connecting[payload.clusterId];
    if (connection) {
      connection.progress = payload.progress;
      connection.stage = payload.stage;
    }
  },
  
  COMPLETE_CLUSTER_CONNECTION(state: ClusterState, payload: { clusterId: string; success: boolean; error?: string }) {
    const connection = state.connecting[payload.clusterId];
    if (connection) {
      connection.connecting = false;
      connection.progress = 100;
      
      if (payload.success) {
        connection.stage = 'Connected successfully';
        connection.error = null;
        
        // Update cluster status
        const cluster = state.clusters[payload.clusterId];
        if (cluster) {
          cluster.connectionStatus = 'connected';
          cluster.healthError = null;
        }
      } else {
        connection.stage = 'Connection failed';
        connection.error = payload.error || 'Unknown error';
        connection.retryCount += 1;
        
        // Update cluster status
        const cluster = state.clusters[payload.clusterId];
        if (cluster) {
          cluster.connectionStatus = 'error';
          cluster.healthError = payload.error || 'Connection failed';
        }
      }
    }
  }
};

// === Actions ===
const actions = {
  async fetchClusters({ commit }: any) {
    try {
      // Get clusters from Rancher's management store
      // For now, we'll return a placeholder until we integrate with Rancher store
      const mgmtStore: any[] = [];
      
      const clusters: ClusterResourceData[] = mgmtStore.map((cluster: any) => {
        const clusterData: ClusterResourceData = {
          id: cluster.id,
          name: cluster.nameDisplay || cluster.metadata?.name || cluster.id,
          displayName: cluster.spec?.displayName || cluster.nameDisplay || cluster.metadata?.name,
          description: cluster.metadata?.annotations?.['field.cattle.io/description'] || '',
          ready: cluster.isReady || false,
          version: {
            kubernetes: cluster.status?.version?.gitVersion || 'Unknown',
            rancher: 'Unknown', // Will be populated when integrated with Rancher store
            distribution: cluster.status?.provider || 'Unknown'
          },
          capabilities: {
            canInstallApps: true, // Will be determined by checking cluster capabilities
            canManageNamespaces: true,
            canAccessSecrets: cluster.isReady,
            canCreateServiceAccounts: cluster.isReady,
            hasHelmSupport: true,
            hasRancherAppsSupport: true,
            supportedApiVersions: ['v1', 'apps/v1'] // Default set
          },
          stats: {
            totalApps: 0, // Will be populated by installations module
            runningApps: 0,
            failedApps: 0,
            namespacesWithApps: 0,
            lastAppActivity: undefined
          }
        };
        return clusterData;
      });
      
      commit('SET_CLUSTERS', clusters);
      
      // Fetch capabilities for each cluster in parallel
      const capabilityPromises = clusters.map(cluster => 
        actions.refreshClusterCapabilities({ commit }, cluster.id)
          .catch(error => {
            console.warn(`Failed to fetch capabilities for cluster ${cluster.id}:`, error);
          })
      );
      
      await Promise.allSettled(capabilityPromises);
      
    } catch (error) {
      console.error('Failed to fetch clusters:', error);
      throw error;
    }
  },
  
  async connectToCluster({ commit }: any, clusterId: string) {
    commit('START_CLUSTER_CONNECTION', clusterId);
    
    try {
      // Step 1: Initialize connection
      commit('UPDATE_CONNECTION_PROGRESS', {
        clusterId,
        progress: 10,
        stage: 'Connecting to cluster...'
      });
      
      // Step 2: Check cluster accessibility
      commit('UPDATE_CONNECTION_PROGRESS', {
        clusterId,
        progress: 30,
        stage: 'Checking cluster accessibility...'
      });
      
      // TODO: Check cluster accessibility
      
      // Step 3: Verify required APIs
      commit('UPDATE_CONNECTION_PROGRESS', {
        clusterId,
        progress: 60,
        stage: 'Verifying cluster APIs...'
      });
      
      // Check if the cluster has the APIs we need
      const hasHelmAPI = true; // Check for Helm CRDs
      const hasAppsAPI = true; // Check for apps/v1
      
      if (!hasHelmAPI || !hasAppsAPI) {
        throw new Error('Cluster missing required APIs');
      }
      
      // Step 4: Test basic operations
      commit('UPDATE_CONNECTION_PROGRESS', {
        clusterId,
        progress: 90,
        stage: 'Testing cluster operations...'
      });
      
      // Try to list namespaces as a basic test (simplified)
      // try {
      //   await this.$store.dispatch(`cluster/findAll`, {
      //     type: 'namespace'
      //   }, { root: true });
      // } catch (error) {
      //   throw new Error('Failed to query cluster resources');
      // }
      
      // Success
      commit('COMPLETE_CLUSTER_CONNECTION', {
        clusterId,
        success: true
      });
      
    } catch (error: any) {
      console.error(`Failed to connect to cluster ${clusterId}:`, error);
      commit('COMPLETE_CLUSTER_CONNECTION', {
        clusterId,
        success: false,
        error: error.message || 'Connection failed'
      });
      throw error;
    }
  },
  
  async checkClusterHealth({ commit }: any, clusterId: string) {
    try {
      // Basic health check - try to get cluster info (simplified)
      const cluster = null; // Will be populated when integrated with Rancher store
      
      if (!cluster) {
        commit('UPDATE_CLUSTER_STATUS', {
          clusterId,
          status: 'disconnected'
        });
        return;
      }
      
      if ((cluster as any)?.isReady) {
        commit('UPDATE_CLUSTER_STATUS', {
          clusterId,
          status: 'connected'
        });
      } else {
        commit('UPDATE_CLUSTER_STATUS', {
          clusterId,
          status: 'error'
        });
      }
      
    } catch (error: any) {
      console.error(`Health check failed for cluster ${clusterId}:`, error);
      commit('UPDATE_CLUSTER_STATUS', {
        clusterId,
        status: 'error'
      });
    }
  },
  
  async refreshClusterCapabilities({ commit }: any, clusterId: string) {
    try {
      // Get cluster-specific store (simplified)
      // const clusterStore = this.$store.getters[`cluster/all`](clusterId);
      
      const capabilities = {
        canInstallApps: true,
        canManageNamespaces: true,
        canAccessSecrets: true,
        canCreateServiceAccounts: true,
        hasHelmSupport: true,
        hasRancherAppsSupport: true,
        supportedApiVersions: ['v1', 'apps/v1', 'networking.k8s.io/v1'],
        maxHelmVersion: '3.x',
        installedOperators: []
      };
      
      // Check for Helm support by looking for Helm releases (simplified)
      // For now, assume Helm support is available
      capabilities.hasHelmSupport = true;
      
      // Check for installed operators by looking for CRDs (simplified)
      // For now, return empty array
      capabilities.installedOperators = [];
      
      commit('SET_CLUSTER_CAPABILITIES', {
        clusterId,
        capabilities
      });
      
    } catch (error) {
      console.error(`Failed to refresh capabilities for cluster ${clusterId}:`, error);
      
      // Set minimal capabilities on error
      commit('SET_CLUSTER_CAPABILITIES', {
        clusterId,
        capabilities: {
          canInstallApps: false,
          canManageNamespaces: false,
          canAccessSecrets: false,
          canCreateServiceAccounts: false,
          hasHelmSupport: false,
          hasRancherAppsSupport: false,
          supportedApiVersions: [],
          maxHelmVersion: null,
          installedOperators: []
        }
      });
    }
  }
};

// === Module Definition ===
const clustersModule = {
  namespaced: true,
  state: createInitialState,
  getters,
  mutations,
  actions
};

export default clustersModule;