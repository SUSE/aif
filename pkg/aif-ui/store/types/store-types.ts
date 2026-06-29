/**
 * Store interface definitions for SUSE AI extension
 * Defines the structure of Vuex store modules following Rancher UI patterns
 */

import { ActionContext, GetterTree, MutationTree, ActionTree } from 'vuex';
import { 
  SuseAIState,
  AppState,
  ClusterState,
  InstallationState,
  RepositoryState,
  DiscoveryProgress,
  NotificationState,
  DiscoverInstallationsPayload,
  InstallAppPayload,
  UpgradeAppPayload,
  UninstallAppPayload,
  SyncRepositoryPayload
} from './state-types';
import { AppSummary, AppInstallationInfo } from '../../types/app-types';
import { ClusterResourceData } from '../../models/cluster/cluster-resource';
import { RepositoryResourceData } from '../../models/cluster/repository-resource';

// === Common Store Module Interface ===
export interface StoreModule<S, R = any> {
  namespaced?: boolean;
  state: S | (() => S);
  getters?: GetterTree<S, R>;
  mutations?: MutationTree<S>;
  actions?: ActionTree<S, R>;
}

// === App Store Module ===
export interface AppStoreModule extends StoreModule<AppState, SuseAIState> {
  getters: {
    allApps: (state: AppState) => AppSummary[];
    appById: (state: AppState) => (id: string) => AppSummary | null;
    appsByCategory: (state: AppState) => Record<string, AppSummary[]>;
    appsByRepository: (state: AppState) => Record<string, AppSummary[]>;
    installedApps: (state: AppState, getters: any, rootState: SuseAIState) => AppSummary[];
    availableApps: (state: AppState, getters: any, rootState: SuseAIState) => AppSummary[];
    filteredApps: (state: AppState) => AppSummary[];
    searchResults: (state: AppState) => AppSummary[];
    categories: (state: AppState) => string[];
    repositories: (state: AppState) => string[];
    isLoading: (state: AppState) => boolean;
    hasErrors: (state: AppState) => boolean;
    getError: (state: AppState) => (appId: string) => string | null;
  };
  
  mutations: {
    SET_APPS: (state: AppState, apps: AppSummary[]) => void;
    SET_APP: (state: AppState, app: AppSummary) => void;
    REMOVE_APP: (state: AppState, appId: string) => void;
    UPDATE_APP_INSTALLATIONS: (state: AppState, payload: { appId: string; installations: AppInstallationInfo[] }) => void;
    SET_FILTERS: (state: AppState, filters: Partial<AppState['filters']>) => void;
    SET_LOADING: (state: AppState, payload: { appId?: string; loading: boolean; type?: string }) => void;
    SET_ERROR: (state: AppState, payload: { appId: string; error: string | null }) => void;
    BUILD_SEARCH_INDEX: (state: AppState) => void;
    CLEAR_CACHE: (state: AppState) => void;
  };
  
  actions: {
    fetchAllApps: (context: ActionContext<AppState, SuseAIState>, payload?: { force?: boolean }) => Promise<void>;
    fetchApp: (context: ActionContext<AppState, SuseAIState>, appId: string) => Promise<AppSummary>;
    searchApps: (context: ActionContext<AppState, SuseAIState>, query: string) => Promise<AppSummary[]>;
    updateFilters: (context: ActionContext<AppState, SuseAIState>, filters: Partial<AppState['filters']>) => void;
    refreshApps: (context: ActionContext<AppState, SuseAIState>) => Promise<void>;
    clearCache: (context: ActionContext<AppState, SuseAIState>) => void;
  };
}

// === Installation Store Module ===
export interface InstallationStoreModule extends StoreModule<InstallationState, SuseAIState> {
  getters: {
    allInstallations: (state: InstallationState) => AppInstallationInfo[];
    installationById: (state: InstallationState) => (key: string) => AppInstallationInfo | null;
    installationsForApp: (state: InstallationState) => (appId: string) => AppInstallationInfo[];
    installationsForCluster: (state: InstallationState) => (clusterId: string) => AppInstallationInfo[];
    isAppInstalled: (state: InstallationState) => (appId: string, clusterId?: string) => boolean;
    installedAppIds: (state: InstallationState) => string[];
    activeOperations: (state: InstallationState) => Record<string, any>;
    isOperationActive: (state: InstallationState) => (key: string) => boolean;
    operationProgress: (state: InstallationState) => (key: string) => number;
  };
  
  mutations: {
    SET_INSTALLATIONS: (state: InstallationState, installations: AppInstallationInfo[]) => void;
    SET_INSTALLATION: (state: InstallationState, installation: AppInstallationInfo) => void;
    REMOVE_INSTALLATION: (state: InstallationState, key: string) => void;
    UPDATE_INSTALLATION_STATUS: (state: InstallationState, payload: { key: string; status: string; progress?: number }) => void;
    START_OPERATION: (state: InstallationState, operation: any) => void;
    UPDATE_OPERATION: (state: InstallationState, payload: { key: string; progress: number; stage: string }) => void;
    COMPLETE_OPERATION: (state: InstallationState, key: string) => void;
    FAIL_OPERATION: (state: InstallationState, payload: { key: string; error: string }) => void;
    CLEAR_COMPLETED_OPERATIONS: (state: InstallationState) => void;
  };
  
  actions: {
    discoverInstallations: (context: ActionContext<InstallationState, SuseAIState>, payload: DiscoverInstallationsPayload) => Promise<void>;
    installApp: (context: ActionContext<InstallationState, SuseAIState>, payload: InstallAppPayload) => Promise<void>;
    upgradeApp: (context: ActionContext<InstallationState, SuseAIState>, payload: UpgradeAppPayload) => Promise<void>;
    uninstallApp: (context: ActionContext<InstallationState, SuseAIState>, payload: UninstallAppPayload) => Promise<void>;
    rollbackApp: (context: ActionContext<InstallationState, SuseAIState>, payload: any) => Promise<void>;
    refreshInstallations: (context: ActionContext<InstallationState, SuseAIState>, clusterId?: string) => Promise<void>;
    pollOperationStatus: (context: ActionContext<InstallationState, SuseAIState>, operationKey: string) => Promise<void>;
    cancelOperation: (context: ActionContext<InstallationState, SuseAIState>, operationKey: string) => Promise<void>;
  };
}

// === Cluster Store Module ===
export interface ClusterStoreModule extends StoreModule<ClusterState, SuseAIState> {
  getters: {
    allClusters: (state: ClusterState) => ClusterResourceData[];
    clusterById: (state: ClusterState) => (id: string) => ClusterResourceData | null;
    connectedClusters: (state: ClusterState) => ClusterResourceData[];
    availableClusters: (state: ClusterState) => ClusterResourceData[];
    clusterCapabilities: (state: ClusterState) => (clusterId: string) => any;
    canInstallApps: (state: ClusterState) => (clusterId: string) => boolean;
    isClusterHealthy: (state: ClusterState) => (clusterId: string) => boolean;
    clusterConnectionStatus: (state: ClusterState) => (clusterId: string) => string;
  };
  
  mutations: {
    SET_CLUSTERS: (state: ClusterState, clusters: ClusterResourceData[]) => void;
    SET_CLUSTER: (state: ClusterState, cluster: ClusterResourceData) => void;
    UPDATE_CLUSTER_STATUS: (state: ClusterState, payload: { clusterId: string; status: string }) => void;
    SET_CLUSTER_CAPABILITIES: (state: ClusterState, payload: { clusterId: string; capabilities: any }) => void;
    START_CLUSTER_CONNECTION: (state: ClusterState, clusterId: string) => void;
    UPDATE_CONNECTION_PROGRESS: (state: ClusterState, payload: { clusterId: string; progress: number; stage: string }) => void;
    COMPLETE_CLUSTER_CONNECTION: (state: ClusterState, payload: { clusterId: string; success: boolean; error?: string }) => void;
  };
  
  actions: {
    fetchClusters: (context: ActionContext<ClusterState, SuseAIState>) => Promise<void>;
    connectToCluster: (context: ActionContext<ClusterState, SuseAIState>, clusterId: string) => Promise<void>;
    checkClusterHealth: (context: ActionContext<ClusterState, SuseAIState>, clusterId: string) => Promise<void>;
    refreshClusterCapabilities: (context: ActionContext<ClusterState, SuseAIState>, clusterId: string) => Promise<void>;
  };
}

// === Repository Store Module ===
export interface RepositoryStoreModule extends StoreModule<RepositoryState, SuseAIState> {
  getters: {
    allRepositories: (state: RepositoryState) => RepositoryResourceData[];
    repositoryByName: (state: RepositoryState) => (name: string) => RepositoryResourceData | null;
    repositoriesForCluster: (state: RepositoryState) => (clusterId: string) => RepositoryResourceData[];
    activeSyncOperations: (state: RepositoryState) => Record<string, any>;
    isSyncing: (state: RepositoryState) => (repoName: string) => boolean;
    syncProgress: (state: RepositoryState) => (repoName: string) => number;
  };
  
  mutations: {
    SET_REPOSITORIES: (state: RepositoryState, repositories: RepositoryResourceData[]) => void;
    SET_REPOSITORY: (state: RepositoryState, repository: RepositoryResourceData) => void;
    UPDATE_REPOSITORY_STATUS: (state: RepositoryState, payload: { repoName: string; status: string; lastSynced?: string }) => void;
    START_SYNC_OPERATION: (state: RepositoryState, operation: any) => void;
    UPDATE_SYNC_PROGRESS: (state: RepositoryState, payload: { repoName: string; progress: number; stage: string }) => void;
    COMPLETE_SYNC_OPERATION: (state: RepositoryState, payload: { repoName: string; success: boolean; error?: string }) => void;
  };
  
  actions: {
    fetchRepositories: (context: ActionContext<RepositoryState, SuseAIState>, clusterId?: string) => Promise<void>;
    syncRepository: (context: ActionContext<RepositoryState, SuseAIState>, payload: SyncRepositoryPayload) => Promise<void>;
    addRepository: (context: ActionContext<RepositoryState, SuseAIState>, repository: RepositoryResourceData) => Promise<void>;
    removeRepository: (context: ActionContext<RepositoryState, SuseAIState>, payload: { repoName: string; clusterId: string }) => Promise<void>;
    refreshRepositories: (context: ActionContext<RepositoryState, SuseAIState>) => Promise<void>;
  };
}

// === Main Store Interface ===
export interface SuseAIStore {
  state: SuseAIState;
  
  getters: {
    // Discovery getters
    isDiscovering: (state: SuseAIState) => boolean;
    discoveryProgress: (state: SuseAIState) => DiscoveryProgress;
    discoveryErrors: (state: SuseAIState) => any[];
    
    // Cross-module getters
    appsWithInstallations: (state: SuseAIState) => AppSummary[];
    installedAppsForCluster: (state: SuseAIState) => (clusterId: string) => AppSummary[];
    availableAppsForCluster: (state: SuseAIState) => (clusterId: string) => AppSummary[];
    
    // Statistics getters
    totalAppsCount: (state: SuseAIState) => number;
    installedAppsCount: (state: SuseAIState) => number;
    availableAppsCount: (state: SuseAIState) => number;
    clustersWithAppsCount: (state: SuseAIState) => number;
    
    // UI state getters
    isBulkMode: (state: SuseAIState) => boolean;
    selectedAppsCount: (state: SuseAIState) => number;
    canPerformBulkOperation: (state: SuseAIState) => (operation: string) => boolean;
  };
  
  mutations: {
    // Discovery mutations
    START_DISCOVERY: (state: SuseAIState) => void;
    UPDATE_DISCOVERY_PROGRESS: (state: SuseAIState, payload: any) => void;
    COMPLETE_DISCOVERY: (state: SuseAIState, payload?: any) => void;
    FAIL_DISCOVERY: (state: SuseAIState, error: any) => void;
    
    // Settings mutations
    UPDATE_SETTINGS: (state: SuseAIState, settings: Partial<SuseAIState['settings']>) => void;
    
    // UI mutations
    SET_SIDEBAR_OPEN: (state: SuseAIState, open: boolean) => void;
    TOGGLE_BULK_MODE: (state: SuseAIState) => void;
    SELECT_APP: (state: SuseAIState, appId: string) => void;
    DESELECT_APP: (state: SuseAIState, appId: string) => void;
    CLEAR_SELECTED_APPS: (state: SuseAIState) => void;
    SET_CURRENT_VIEW: (state: SuseAIState, view: string) => void;
    SET_SORT: (state: SuseAIState, payload: { field: string; direction: 'asc' | 'desc' }) => void;
  };
  
  actions: {
    // Main discovery action
    discoverAllData: (context: ActionContext<SuseAIState, SuseAIState>, payload?: DiscoverInstallationsPayload) => Promise<void>;
    
    // Bulk operations
    performBulkInstall: (context: ActionContext<SuseAIState, SuseAIState>, payload: any) => Promise<void>;
    performBulkUpgrade: (context: ActionContext<SuseAIState, SuseAIState>, payload: any) => Promise<void>;
    performBulkUninstall: (context: ActionContext<SuseAIState, SuseAIState>, payload: any) => Promise<void>;
    
    // Settings actions  
    updateSettings: (context: ActionContext<SuseAIState, SuseAIState>, settings: Partial<SuseAIState['settings']>) => void;
    loadSettings: (context: ActionContext<SuseAIState, SuseAIState>) => Promise<void>;
    saveSettings: (context: ActionContext<SuseAIState, SuseAIState>) => Promise<void>;
    
    // Initialization
    initialize: (context: ActionContext<SuseAIState, SuseAIState>) => Promise<void>;
    cleanup: (context: ActionContext<SuseAIState, SuseAIState>) => void;
  };
  
  modules: {
    apps: AppStoreModule;
    installations: InstallationStoreModule;
    clusters: ClusterStoreModule;
    repositories: RepositoryStoreModule;
  };
}

// === Store Plugin Interface ===
export interface StorePlugin {
  install: (store: any) => void;
}

// === Store Configuration ===
export interface StoreConfig {
  strict?: boolean;
  plugins?: StorePlugin[];
  devtools?: boolean;
}

// === Utility Types ===
export type ModuleName = 'apps' | 'installations' | 'clusters' | 'repositories';
export type GetterName<T> = T extends { getters: infer G } ? keyof G : never;
export type MutationName<T> = T extends { mutations: infer M } ? keyof M : never;
export type ActionName<T> = T extends { actions: infer A } ? keyof A : never;