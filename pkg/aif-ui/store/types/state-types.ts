/**
 * State type definitions for SUSE AI extension store
 * Following domain model patterns for consistent state management
 */

import { AppSummary, AppInstallationInfo } from '../../types/app-types';
import { ClusterResourceData } from '../../models/cluster/cluster-resource';
import { RepositoryResourceData } from '../../models/cluster/repository-resource';

// === Discovery Progress States ===
export interface DiscoveryProgress {
  discovering: boolean;
  clustersProgress: Record<string, ClusterDiscoveryProgress>;
  totalClusters: number;
  discoveredClusters: number;
  totalApps: number;
  discoveredApps: number;
  errors: DiscoveryError[];
  lastDiscovery: string | null;
  stage: DiscoveryStage;
}

export interface ClusterDiscoveryProgress {
  clusterId: string;
  clusterName: string;
  stage: DiscoveryStage;
  progress: number; // 0-100
  appsFound: number;
  repositoriesFound: number;
  error: string | null;
  startedAt: string;
  completedAt: string | null;
}

export type DiscoveryStage = 
  | 'idle'
  | 'connecting'
  | 'discovering-repositories'
  | 'discovering-apps'
  | 'discovering-installations'
  | 'processing'
  | 'completed'
  | 'error';

export interface DiscoveryError {
  clusterId: string;
  clusterName?: string;
  stage: DiscoveryStage;
  error: string;
  timestamp: string;
  retryable: boolean;
}

// === Installation State Management ===
export interface InstallationState {
  installations: Record<string, AppInstallationInfo>; // key: clusterId-namespace-releaseName
  installationsByApp: Record<string, string[]>; // appId -> installationKeys
  installationsByCluster: Record<string, string[]>; // clusterId -> installationKeys
  installing: Record<string, InstallationOperation>; // installationKey -> operation
  lastUpdated: string | null;
}

export interface InstallationOperation {
  type: 'install' | 'upgrade' | 'uninstall' | 'rollback';
  appId: string;
  clusterId: string;
  namespace: string;
  releaseName: string;
  progress: number; // 0-100
  stage: string;
  startedAt: string;
  estimatedCompletion: string | null;
  error: string | null;
}

// === Repository State Management ===
export interface RepositoryState {
  repositories: Record<string, RepositoryInfo>; // repoName -> RepositoryInfo
  repositoriesByCluster: Record<string, string[]>; // clusterId -> repoNames
  syncing: Record<string, RepositorySyncOperation>; // repoName -> sync operation
  lastSyncCheck: string | null;
}

export interface RepositoryInfo extends RepositoryResourceData {
  clustersWithRepo: string[];
  appsCount: number;
  lastSynced: string | null;
  syncStatus: 'synced' | 'syncing' | 'failed' | 'unknown';
  syncError: string | null;
}

export interface RepositorySyncOperation {
  repoName: string;
  clusterId: string;
  progress: number; // 0-100
  stage: string;
  startedAt: string;
  error: string | null;
}

// === App State Management ===
export interface AppState {
  apps: Record<string, AppSummary>; // appId -> AppSummary
  appsByCategory: Record<string, string[]>; // category -> appIds
  appsByRepository: Record<string, string[]>; // repoName -> appIds
  searchIndex: AppSearchIndex;
  filters: AppFilters;
  loading: AppLoadingState;
  lastUpdated: string | null;
}

export interface AppSearchIndex {
  byName: Record<string, string[]>; // normalized name -> appIds
  byKeyword: Record<string, string[]>; // keyword -> appIds
  byDescription: Record<string, string[]>; // text token -> appIds
}

export interface AppFilters {
  searchText: string;
  categories: string[];
  repositories: string[];
  statuses: string[];
  clusters: string[];
  onlyInstalled: boolean;
  onlyFavorites: boolean;
}

export interface AppLoadingState {
  loadingAll: boolean;
  loadingCategories: boolean;
  loadingDetails: Record<string, boolean>; // appId -> loading
  loadingCharts: Record<string, boolean>; // appId -> loading
  errors: Record<string, string>; // appId -> error message
}

// === Cluster State Management ===
export interface ClusterState {
  clusters: Record<string, ClusterInfo>; // clusterId -> ClusterInfo
  capabilities: Record<string, ClusterCapabilities>; // clusterId -> capabilities
  connecting: Record<string, ClusterConnection>; // clusterId -> connection info
  lastUpdated: string | null;
}

export interface ClusterInfo extends ClusterResourceData {
  connectionStatus: 'connected' | 'connecting' | 'disconnected' | 'error';
  lastHealthCheck: string | null;
  healthError: string | null;
}

export interface ClusterCapabilities {
  canInstallApps: boolean;
  canManageNamespaces: boolean;
  canAccessSecrets: boolean;
  canCreateServiceAccounts: boolean;
  hasHelmSupport: boolean;
  hasRancherAppsSupport: boolean;
  supportedApiVersions: string[];
  maxHelmVersion: string | null;
  installedOperators: string[];
}

export interface ClusterConnection {
  connecting: boolean;
  progress: number; // 0-100
  stage: string;
  error: string | null;
  retryCount: number;
  lastAttempt: string;
}

// === Notification State ===
export interface NotificationState {
  notifications: AppNotification[];
  unreadCount: number;
  settings: NotificationSettings;
}

export interface AppNotification {
  id: string;
  type: 'info' | 'success' | 'warning' | 'error';
  title: string;
  message: string;
  appId?: string;
  clusterId?: string;
  timestamp: string;
  read: boolean;
  persistent: boolean;
  actions?: NotificationAction[];
}

export interface NotificationAction {
  label: string;
  action: string; // action name to dispatch
  payload?: any;
  primary?: boolean;
}

export interface NotificationSettings {
  enabled: boolean;
  showInstallSuccess: boolean;
  showUpgradeSuccess: boolean;
  showErrors: boolean;
  showWarnings: boolean;
  autoHideAfter: number; // seconds, 0 = never
}

// === Root State Interface ===
export interface SuseAIState {
  discovery: DiscoveryProgress;
  installations: InstallationState;
  repositories: RepositoryState;
  apps: AppState;
  clusters: ClusterState;
  notifications: NotificationState;
  
  // Global settings
  settings: {
    autoRefresh: boolean;
    refreshInterval: number; // seconds
    defaultNamespace: string;
    showSystemApps: boolean;
    preferredView: 'grid' | 'list' | 'table';
  };
  
  // UI state
  ui: {
    sidebarOpen: boolean;
    activeFilters: boolean;
    selectedApps: string[];
    bulkOperationMode: boolean;
    currentView: string;
    sortBy: string;
    sortDirection: 'asc' | 'desc';
  };
}

// === Action Payload Types ===
export interface DiscoverInstallationsPayload {
  clusters?: string[];
  force?: boolean;
  includeSystemApps?: boolean;
}

export interface InstallAppPayload {
  appId: string;
  clusterId: string;
  namespace: string;
  releaseName: string;
  values?: Record<string, any>;
  chartVersion?: string;
}

export interface UpgradeAppPayload {
  appId: string;
  clusterId: string;
  namespace: string;
  releaseName: string;
  values?: Record<string, any>;
  chartVersion?: string;
  resetValues?: boolean;
}

export interface UninstallAppPayload {
  appId: string;
  clusterId: string;
  namespace: string;
  releaseName: string;
  keepHistory?: boolean;
}

export interface SyncRepositoryPayload {
  repoName: string;
  clusterId: string;
  force?: boolean;
}

// === Utility Types ===
export type StateKey = keyof SuseAIState;
export type AppStateKey = keyof AppState;
export type ClusterStateKey = keyof ClusterState;
export type InstallationStateKey = keyof InstallationState;
export type RepositoryStateKey = keyof RepositoryState;