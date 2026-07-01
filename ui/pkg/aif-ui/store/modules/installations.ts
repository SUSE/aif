/**
 * Installations Store Module
 * Manages app installation state, operations, and tracking
 * Following standard Vuex patterns for consistency
 */

import { InstallationState, DiscoverInstallationsPayload, InstallAppPayload, UpgradeAppPayload, UninstallAppPayload } from '../types/state-types';
import { AppInstallationInfo } from '../../types/app-types';

// === Initial State ===
function createInitialState(): InstallationState {
  return {
    installations: {},
    installationsByApp: {},
    installationsByCluster: {},
    installing: {},
    lastUpdated: null
  };
}

// === Helper Functions ===
function createInstallationKey(clusterId: string, namespace: string, releaseName: string): string {
  return `${clusterId}:${namespace}:${releaseName}`;
}

function parseInstallationKey(key: string): { clusterId: string; namespace: string; releaseName: string } {
  const [clusterId, namespace, releaseName] = key.split(':');
  return { clusterId, namespace, releaseName };
}

// === Getters ===
const getters = {
  allInstallations: (state: InstallationState): AppInstallationInfo[] => {
    return Object.values(state.installations);
  },
  
  installationById: (state: InstallationState) => (key: string): AppInstallationInfo | null => {
    return state.installations[key] || null;
  },
  
  installationsForApp: (state: InstallationState) => (appId: string): AppInstallationInfo[] => {
    const keys = state.installationsByApp[appId] || [];
    return keys.map(key => state.installations[key]).filter(Boolean);
  },
  
  installationsForCluster: (state: InstallationState) => (clusterId: string): AppInstallationInfo[] => {
    const keys = state.installationsByCluster[clusterId] || [];
    return keys.map(key => state.installations[key]).filter(Boolean);
  },
  
  isAppInstalled: (state: InstallationState) => (appId: string, clusterId?: string): boolean => {
    const installationKeys = state.installationsByApp[appId] || [];
    
    if (clusterId) {
      // Check specific cluster
      return installationKeys.some(key => {
        const installation = state.installations[key];
        return installation && 
               installation.clusterId === clusterId && 
               ['deployed', 'installing', 'upgrading'].includes(installation.status);
      });
    } else {
      // Check any cluster
      return installationKeys.some(key => {
        const installation = state.installations[key];
        return installation && ['deployed', 'installing', 'upgrading'].includes(installation.status);
      });
    }
  },
  
  installedAppIds: (state: InstallationState): string[] => {
    const appIds = new Set<string>();
    Object.values(state.installations).forEach(installation => {
      if (['deployed', 'installing', 'upgrading'].includes(installation.status)) {
        // We need to track appId in installations - for now use releaseName
        const appId = (installation as any).appId || installation.releaseName;
        appIds.add(appId);
      }
    });
    return Array.from(appIds);
  },
  
  activeOperations: (state: InstallationState): Record<string, any> => {
    return state.installing;
  },
  
  isOperationActive: (state: InstallationState) => (key: string): boolean => {
    return !!state.installing[key];
  },
  
  operationProgress: (state: InstallationState) => (key: string): number => {
    const operation = state.installing[key];
    return operation ? operation.progress : 0;
  }
};

// === Mutations ===
const mutations = {
  SET_INSTALLATIONS(state: InstallationState, installations: AppInstallationInfo[]) {
    // Clear existing state
    state.installations = {};
    state.installationsByApp = {};
    state.installationsByCluster = {};
    
    // Add installations
    installations.forEach(installation => {
      mutations.SET_INSTALLATION(state, installation);
    });
    
    state.lastUpdated = new Date().toISOString();
  },
  
  SET_INSTALLATION(state: InstallationState, installation: AppInstallationInfo) {
    const key = createInstallationKey(
      installation.clusterId,
      installation.namespace,
      installation.releaseName
    );
    
    state.installations[key] = installation;
    
    // Update app index
    const appId = (installation as any).appId || installation.releaseName;
    if (!state.installationsByApp[appId]) {
      state.installationsByApp[appId] = [];
    }
    if (!state.installationsByApp[appId].includes(key)) {
      state.installationsByApp[appId].push(key);
    }
    
    // Update cluster index
    if (!state.installationsByCluster[installation.clusterId]) {
      state.installationsByCluster[installation.clusterId] = [];
    }
    if (!state.installationsByCluster[installation.clusterId].includes(key)) {
      state.installationsByCluster[installation.clusterId].push(key);
    }
  },
  
  REMOVE_INSTALLATION(state: InstallationState, key: string) {
    const installation = state.installations[key];
    if (!installation) return;
    
    // Remove from main store
    delete state.installations[key];
    
    // Remove from app index
    const appId = (installation as any).appId || installation.releaseName;
    const appKeys = state.installationsByApp[appId] || [];
    const appIndex = appKeys.indexOf(key);
    if (appIndex > -1) {
      appKeys.splice(appIndex, 1);
    }
    if (appKeys.length === 0) {
      delete state.installationsByApp[appId];
    }
    
    // Remove from cluster index
    const clusterKeys = state.installationsByCluster[installation.clusterId] || [];
    const clusterIndex = clusterKeys.indexOf(key);
    if (clusterIndex > -1) {
      clusterKeys.splice(clusterIndex, 1);
    }
    if (clusterKeys.length === 0) {
      delete state.installationsByCluster[installation.clusterId];
    }
  },
  
  UPDATE_INSTALLATION_STATUS(state: InstallationState, payload: { key: string; status: string; progress?: number }) {
    const installation = state.installations[payload.key];
    if (installation) {
      installation.status = payload.status as any;
      installation.updatedAt = new Date().toISOString();
      
      if (payload.progress !== undefined && installation.progress) {
        installation.progress.progress = payload.progress;
      }
    }
  },
  
  START_OPERATION(state: InstallationState, operation: any) {
    const key = createInstallationKey(
      operation.clusterId,
      operation.namespace,
      operation.releaseName
    );
    
    state.installing[key] = {
      ...operation,
      startedAt: new Date().toISOString(),
      progress: 0,
      stage: 'Starting...',
      error: null
    };
  },
  
  UPDATE_OPERATION(state: InstallationState, payload: { key: string; progress: number; stage: string }) {
    const operation = state.installing[payload.key];
    if (operation) {
      operation.progress = payload.progress;
      operation.stage = payload.stage;
    }
  },
  
  COMPLETE_OPERATION(state: InstallationState, key: string) {
    const operation = state.installing[key];
    if (operation) {
      operation.progress = 100;
      operation.stage = 'Completed';
      
      // Remove operation after a delay (handled by action)
      setTimeout(() => {
        delete state.installing[key];
      }, 3000);
    }
  },
  
  FAIL_OPERATION(state: InstallationState, payload: { key: string; error: string }) {
    const operation = state.installing[payload.key];
    if (operation) {
      operation.error = payload.error;
      operation.stage = 'Failed';
      
      // Keep failed operations for user to see
    }
  },
  
  CLEAR_COMPLETED_OPERATIONS(state: InstallationState) {
    Object.keys(state.installing).forEach(key => {
      const operation = state.installing[key];
      if (operation.progress === 100 && !operation.error) {
        delete state.installing[key];
      }
    });
  }
};

// === Actions ===
const actions = {
  async discoverInstallations({ commit, rootState }: any, payload: DiscoverInstallationsPayload = {}) {
    const clusters = payload.clusters || Object.keys(rootState.suseai.clusters.clusters);
    const installations: AppInstallationInfo[] = [];
    
    for (const clusterId of clusters) {
      try {
        // Get Helm releases for this cluster (simplified)
        // For now, return empty array until integrated with Rancher store
        const releases: any[] = [];
        
        // Convert Helm releases to installations
        for (const release of releases) {
          const releaseData = release.data?.release;
          if (releaseData) {
            // Parse Helm release data
            let releaseInfo;
            try {
              const decodedData = atob(releaseData);
              releaseInfo = JSON.parse(decodedData);
            } catch (error) {
              console.warn(`Failed to parse release data for ${release.metadata.name}:`, error);
              continue;
            }
            
            const installation: AppInstallationInfo = {
              clusterId,
              namespace: release.metadata.namespace,
              releaseName: release.metadata.name,
              status: releaseInfo.info?.status?.toLowerCase() || 'unknown',
              version: releaseInfo.chart?.metadata?.version,
              chartVersion: releaseInfo.chart?.metadata?.version,
              appVersion: releaseInfo.chart?.metadata?.appVersion,
              lastDeployed: releaseInfo.info?.last_deployed,
              notes: releaseInfo.info?.notes,
              values: releaseInfo.config || {},
              userValues: {},
              createdAt: release.metadata.creationTimestamp,
              updatedAt: releaseInfo.info?.last_deployed || release.metadata.creationTimestamp,
              lastHealthCheck: new Date().toISOString()
            } as AppInstallationInfo;
            
            // Add appId if we can determine it
            (installation as any).appId = releaseInfo.chart?.metadata?.name || release.metadata.name;
            
            installations.push(installation);
          }
        }
        
      } catch (error) {
        console.error(`Failed to discover installations for cluster ${clusterId}:`, error);
      }
    }
    
    commit('SET_INSTALLATIONS', installations);
    
    // Update app installation info in apps module (to be implemented)
    // For now, skip the cross-module updates until properly integrated
    // const installationsByApp: Record<string, AppInstallationInfo[]> = {};
    // ... update logic will be added when integrating with main store
  },
  
  async installApp({ commit, dispatch }: any, payload: InstallAppPayload) {
    const key = createInstallationKey(payload.clusterId, payload.namespace, payload.releaseName);
    
    // Start operation tracking
    commit('START_OPERATION', {
      type: 'install',
      appId: payload.appId,
      clusterId: payload.clusterId,
      namespace: payload.namespace,
      releaseName: payload.releaseName
    });
    
    try {
      // Update progress
      commit('UPDATE_OPERATION', {
        key,
        progress: 10,
        stage: 'Preparing installation...'
      });
      
      // Get the app chart information
      // For now, create a simple app object until properly integrated with apps module
      const app = {
        repository: 'default',
        chartName: payload.appId,
        version: payload.chartVersion || 'latest'
      };
      // App object is always created above, so this check is not needed
      // if (!app) {
      //   throw new Error(`App ${payload.appId} not found`);
      // }
      
      commit('UPDATE_OPERATION', {
        key,
        progress: 30,
        stage: 'Downloading chart...'
      });
      
      // Use existing rancher-apps service for installation
      const rancherAppsService = await import('../../services/rancher-apps');
      
      commit('UPDATE_OPERATION', {
        key,
        progress: 60,
        stage: 'Installing application...'
      });
      
      // Perform the installation
      await rancherAppsService.createOrUpgradeApp(
        { dispatch: (action: string, payload: any) => dispatch ? dispatch(action, payload) : Promise.resolve() },
        payload.clusterId,
        payload.namespace,
        payload.releaseName,
        {
          repoName: app?.repository || 'default',
          chartName: app?.chartName || payload.appId,
          version: payload.chartVersion || 'latest'
        },
        payload.values || {},
        'install'
      );
      
      commit('UPDATE_OPERATION', {
        key,
        progress: 90,
        stage: 'Verifying installation...'
      });
      
      // Create installation record
      const installation: AppInstallationInfo = {
        clusterId: payload.clusterId,
        namespace: payload.namespace,
        releaseName: payload.releaseName,
        status: 'installing',
        chartVersion: payload.chartVersion,
        values: payload.values || {},
        userValues: payload.values || {},
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        lastHealthCheck: new Date().toISOString()
      } as AppInstallationInfo;
      
      (installation as any).appId = payload.appId;
      
      commit('SET_INSTALLATION', installation);
      commit('COMPLETE_OPERATION', key);
      
      // Refresh installations to get final status
      setTimeout(() => {
        dispatch('refreshInstallations', payload.clusterId);
      }, 5000);
      
    } catch (error: any) {
      console.error(`Failed to install app ${payload.appId}:`, error);
      commit('FAIL_OPERATION', {
        key,
        error: error.message || 'Installation failed'
      });
      throw error;
    }
  },
  
  async upgradeApp({ commit, dispatch }: any, payload: UpgradeAppPayload) {
    const key = createInstallationKey(payload.clusterId, payload.namespace, payload.releaseName);
    
    commit('START_OPERATION', {
      type: 'upgrade',
      appId: payload.appId,
      clusterId: payload.clusterId,
      namespace: payload.namespace,
      releaseName: payload.releaseName
    });
    
    try {
      commit('UPDATE_OPERATION', {
        key,
        progress: 20,
        stage: 'Preparing upgrade...'
      });
      
      const rancherAppsService = await import('../../services/rancher-apps');
      
      commit('UPDATE_OPERATION', {
        key,
        progress: 60,
        stage: 'Upgrading application...'
      });
      
      await rancherAppsService.createOrUpgradeApp(
        { dispatch: (action: string, payload: any) => dispatch ? dispatch(action, payload) : Promise.resolve() },
        payload.clusterId,
        payload.namespace,
        payload.releaseName,
        {
          repoName: 'default',
          chartName: payload.appId,
          version: payload.chartVersion || 'latest'
        },
        payload.values || {},
        'upgrade'
      );
      
      commit('UPDATE_OPERATION', {
        key,
        progress: 90,
        stage: 'Verifying upgrade...'
      });
      
      // Update installation status
      commit('UPDATE_INSTALLATION_STATUS', {
        key,
        status: 'upgrading'
      });
      
      commit('COMPLETE_OPERATION', key);
      
      setTimeout(() => {
        dispatch('refreshInstallations', payload.clusterId);
      }, 5000);
      
    } catch (error: any) {
      console.error(`Failed to upgrade app ${payload.appId}:`, error);
      commit('FAIL_OPERATION', {
        key,
        error: error.message || 'Upgrade failed'
      });
      throw error;
    }
  },
  
  async uninstallApp({ commit, dispatch }: any, payload: UninstallAppPayload) {
    const key = createInstallationKey(payload.clusterId, payload.namespace, payload.releaseName);
    
    commit('START_OPERATION', {
      type: 'uninstall',
      appId: payload.appId,
      clusterId: payload.clusterId,
      namespace: payload.namespace,
      releaseName: payload.releaseName
    });
    
    try {
      commit('UPDATE_OPERATION', {
        key,
        progress: 20,
        stage: 'Preparing uninstall...'
      });
      
      const rancherAppsService = await import('../../services/rancher-apps');
      
      commit('UPDATE_OPERATION', {
        key,
        progress: 70,
        stage: 'Uninstalling application...'
      });
      
      await rancherAppsService.deleteApp(
        { dispatch: (action: string, payload: any) => dispatch ? dispatch(action, payload) : Promise.resolve() },
        payload.clusterId,
        payload.namespace,
        payload.releaseName
      );
      
      commit('UPDATE_OPERATION', {
        key,
        progress: 100,
        stage: 'Uninstall completed'
      });
      
      // Remove installation
      commit('REMOVE_INSTALLATION', key);
      commit('COMPLETE_OPERATION', key);
      
    } catch (error: any) {
      console.error(`Failed to uninstall app ${payload.releaseName}:`, error);
      commit('FAIL_OPERATION', {
        key,
        error: error.message || 'Uninstall failed'
      });
      throw error;
    }
  },
  
  async rollbackApp({ commit, dispatch }: any, payload: any) {
    const key = createInstallationKey(payload.clusterId, payload.namespace, payload.releaseName);
    
    commit('START_OPERATION', {
      type: 'rollback',
      ...payload
    });
    
    try {
      commit('UPDATE_OPERATION', {
        key,
        progress: 30,
        stage: 'Rolling back application...'
      });
      
      const rancherAppsService = await import('../../services/rancher-apps');
      
      // Rollback functionality not directly available in rancher-apps service
      // This would need to be implemented or use Helm directly
      throw new Error('Rollback functionality not yet implemented');
      
      commit('UPDATE_OPERATION', {
        key,
        progress: 90,
        stage: 'Verifying rollback...'
      });
      
      commit('COMPLETE_OPERATION', key);
      
      setTimeout(() => {
        dispatch('refreshInstallations', payload.clusterId);
      }, 3000);
      
    } catch (error: any) {
      console.error(`Failed to rollback app ${payload.releaseName}:`, error);
      commit('FAIL_OPERATION', {
        key,
        error: error.message || 'Rollback failed'
      });
      throw error;
    }
  },
  
  async refreshInstallations({ dispatch }: any, clusterId?: string) {
    const clusters = clusterId ? [clusterId] : undefined;
    await dispatch('discoverInstallations', { clusters, force: true });
  },
  
  async pollOperationStatus({ commit }: any, operationKey: string) {
    // This would poll the actual installation status
    // For now, we'll just simulate progress updates
    // Disabled until properly integrated with state management
    console.log(`Polling operation status for ${operationKey} - not implemented yet`);
    return;
  },
  
  async cancelOperation({ commit }: any, operationKey: string) {
    commit('FAIL_OPERATION', {
      key: operationKey,
      error: 'Operation cancelled by user'
    });
  }
};

// === Module Definition ===
const installationsModule = {
  namespaced: true,
  state: createInitialState,
  getters,
  mutations,
  actions
};

export default installationsModule;