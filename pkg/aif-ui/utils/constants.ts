/**
 * Shared constants for SUSE AI extension
 * Following standard patterns for consistent constant management
 */

// === Product Information ===
export const PRODUCT_NAME = 'SUSE AI';
export const PRODUCT_SLUG = 'suseai';
export const EXTENSION_NAME = 'aif-ui';
export const EXTENSION_VERSION = '0.1.0';

// === Store Namespaces ===
export const STORE_NAMESPACE = 'suseai';
export const STORE_MODULES = {
  APPS: 'apps',
  CLUSTERS: 'clusters', 
  INSTALLATIONS: 'installations',
  REPOSITORIES: 'repositories'
} as const;

// === Route Names ===
export const ROUTE_NAMES = {
  ROOT: 'suseai',
  APPS: 'suseai-apps',
  INSTALL: 'suseai-install',
  MANAGE: 'suseai-manage',
  REPOSITORIES: 'suseai-repositories',
  SETTINGS: 'suseai-settings'
} as const;

// === Discovery and Loading States ===
export const DISCOVERY_STAGES = {
  IDLE: 'idle',
  CONNECTING: 'connecting',
  DISCOVERING_REPOSITORIES: 'discovering-repositories',
  DISCOVERING_APPS: 'discovering-apps',
  DISCOVERING_INSTALLATIONS: 'discovering-installations',
  PROCESSING: 'processing',
  COMPLETED: 'completed',
  ERROR: 'error'
} as const;

export const LOADING_STATES = {
  IDLE: 'idle',
  LOADING: 'loading',
  SUCCESS: 'success',
  ERROR: 'error'
} as const;

// === Installation Status Constants ===
export const INSTALLATION_STATUS = {
  PENDING: 'pending',
  INSTALLING: 'installing',
  DEPLOYED: 'deployed',
  UPGRADING: 'upgrading',
  UNINSTALLING: 'uninstalling',
  FAILED: 'failed',
  SUPERSEDED: 'superseded',
  UNKNOWN: 'unknown'
} as const;

export const APP_STATUS = {
  AVAILABLE: 'available',
  INSTALLING: 'installing',
  DEPLOYED: 'deployed',
  UPGRADING: 'upgrading',
  UNINSTALLING: 'uninstalling',
  FAILED: 'failed',
  UNKNOWN: 'unknown'
} as const;

// === Health Status Constants ===
export const HEALTH_STATUS = {
  HEALTHY: 'healthy',
  DEGRADED: 'degraded',
  UNHEALTHY: 'unhealthy',
  UNKNOWN: 'unknown'
} as const;

export const CONNECTION_STATUS = {
  CONNECTED: 'connected',
  CONNECTING: 'connecting',
  DISCONNECTED: 'disconnected',
  ERROR: 'error'
} as const;

// === Repository Types and Status ===
export const REPOSITORY_TYPE = {
  HELM: 'helm',
  OCI: 'oci',
  GIT: 'git'
} as const;

export const REPOSITORY_STATUS = {
  ACTIVE: 'active',
  PENDING: 'pending',
  SYNCING: 'syncing',
  FAILED: 'failed',
  UNKNOWN: 'unknown'
} as const;

export const SYNC_STATUS = {
  SYNCED: 'synced',
  SYNCING: 'syncing',
  FAILED: 'failed',
  UNKNOWN: 'unknown'
} as const;

// === Operation Types ===
export const OPERATION_TYPE = {
  INSTALL: 'install',
  UPGRADE: 'upgrade',
  UNINSTALL: 'uninstall',
  ROLLBACK: 'rollback',
  RESTART: 'restart',
  SYNC: 'sync'
} as const;

// === UI Constants ===
export const VIEW_MODES = {
  GRID: 'grid',
  LIST: 'list',
  TABLE: 'table'
} as const;

export const SORT_DIRECTIONS = {
  ASC: 'asc',
  DESC: 'desc'
} as const;

export const APP_SORT_FIELDS = {
  NAME: 'name',
  STATUS: 'status',
  VERSION: 'version',
  UPDATED: 'updated',
  POPULARITY: 'popularity',
  INSTALLS: 'installs',
  RATING: 'rating'
} as const;

// === Notification Types ===
export const NOTIFICATION_TYPE = {
  INFO: 'info',
  SUCCESS: 'success',
  WARNING: 'warning',
  ERROR: 'error'
} as const;

export const NOTIFICATION_DURATION = {
  SHORT: 3000,
  MEDIUM: 5000,
  LONG: 8000,
  PERMANENT: 0
} as const;

// === Progress and Timeouts ===
export const PROGRESS_VALUES = {
  MIN: 0,
  MAX: 100,
  INDETERMINATE: -1
} as const;

export const TIMEOUT_VALUES = {
  SHORT: 5000,      // 5 seconds
  MEDIUM: 30000,    // 30 seconds
  LONG: 120000,     // 2 minutes
  EXTENDED: 300000, // 5 minutes
  INSTALL: 600000   // 10 minutes for installations
} as const;

export const RETRY_CONFIG = {
  MAX_ATTEMPTS: 3,
  BASE_DELAY: 1000,
  MAX_DELAY: 10000,
  BACKOFF_FACTOR: 2
} as const;

// === Default Values ===
export const DEFAULT_VALUES = {
  NAMESPACE: 'default',
  TIMEOUT: TIMEOUT_VALUES.MEDIUM,
  PAGE_SIZE: 20,
  REFRESH_INTERVAL: 300000, // 5 minutes
  SEARCH_DEBOUNCE: 300,
  MAX_CONCURRENT_OPERATIONS: 3
} as const;

// === API and Service Constants ===
export const API_ENDPOINTS = {
  APPS: '/api/apps',
  CHARTS: '/api/charts',
  INSTALLATIONS: '/api/installations',
  REPOSITORIES: '/api/repositories',
  CLUSTERS: '/api/clusters'
} as const;

export const HELM_CONSTANTS = {
  OWNER_LABEL: 'app.kubernetes.io/managed-by',
  OWNER_VALUE: 'Helm',
  NAME_LABEL: 'app.kubernetes.io/name',
  INSTANCE_LABEL: 'app.kubernetes.io/instance',
  VERSION_LABEL: 'app.kubernetes.io/version',
  COMPONENT_LABEL: 'app.kubernetes.io/component',
  PART_OF_LABEL: 'app.kubernetes.io/part-of'
} as const;

// === Feature Flags (will be used by feature-flags.ts) ===
export const FEATURE_FLAGS = {
  BULK_OPERATIONS: 'bulk-operations',
  ADVANCED_FILTERING: 'advanced-filtering',
  CUSTOM_REPOSITORIES: 'custom-repositories',
  HEALTH_MONITORING: 'health-monitoring',
  AUTO_UPDATES: 'auto-updates',
  ROLLBACK_SUPPORT: 'rollback-support',
  MULTI_CLUSTER: 'multi-cluster',
  OFFLINE_MODE: 'offline-mode',
  BACKUP_RESTORE: 'backup-restore',
  SECURITY_SCANNING: 'security-scanning'
} as const;

// === Version Requirements ===
export const VERSION_REQUIREMENTS = {
  MIN_RANCHER_VERSION: '2.6.0',
  MIN_KUBERNETES_VERSION: '1.20.0',
  MIN_HELM_VERSION: '3.8.0',
  RECOMMENDED_RANCHER_VERSION: '2.7.0',
  RECOMMENDED_KUBERNETES_VERSION: '1.24.0'
} as const;

// === Chart and App Categories ===
export const APP_CATEGORIES = {
  AI_ML: 'AI & ML',
  ANALYTICS: 'Analytics',
  DATABASE: 'Database',
  MONITORING: 'Monitoring',
  NETWORKING: 'Networking',
  SECURITY: 'Security',
  STORAGE: 'Storage',
  TOOLS: 'Tools',
  WEB: 'Web',
  OTHER: 'Other'
} as const;

// === Built-in Repository Configuration ===
export const BUILT_IN_REPOSITORIES = {
  RANCHER_CHARTS: {
    name: 'rancher-charts',
    displayName: 'Rancher Charts',
    url: 'https://releases.rancher.com/server-charts/stable',
    type: REPOSITORY_TYPE.HELM,
    description: 'Official Rancher application charts',
    official: true
  },
  RANCHER_PARTNER: {
    name: 'rancher-partner-charts',
    displayName: 'Rancher Partner Charts', 
    url: 'https://releases.rancher.com/server-charts/partner',
    type: REPOSITORY_TYPE.HELM,
    description: 'Certified partner application charts',
    official: true
  },
  SUSE_AI: {
    name: 'suse-ai-charts',
    displayName: 'SUSE AI Charts',
    url: 'https://registry.suse.com/charts/ai',
    type: REPOSITORY_TYPE.HELM,
    description: 'SUSE AI and ML application charts',
    official: true
  }
} as const;

// === Error Codes ===
export const ERROR_CODES = {
  // Generic errors
  UNKNOWN: 'ERR_UNKNOWN',
  NETWORK: 'ERR_NETWORK',
  TIMEOUT: 'ERR_TIMEOUT',
  UNAUTHORIZED: 'ERR_UNAUTHORIZED',
  FORBIDDEN: 'ERR_FORBIDDEN',
  NOT_FOUND: 'ERR_NOT_FOUND',
  
  // App-specific errors
  APP_NOT_FOUND: 'ERR_APP_NOT_FOUND',
  APP_ALREADY_INSTALLED: 'ERR_APP_ALREADY_INSTALLED',
  APP_NOT_INSTALLED: 'ERR_APP_NOT_INSTALLED',
  
  // Cluster errors
  CLUSTER_NOT_FOUND: 'ERR_CLUSTER_NOT_FOUND',
  CLUSTER_NOT_READY: 'ERR_CLUSTER_NOT_READY',
  CLUSTER_CONNECTION_FAILED: 'ERR_CLUSTER_CONNECTION_FAILED',
  
  // Installation errors
  INSTALLATION_FAILED: 'ERR_INSTALLATION_FAILED',
  UPGRADE_FAILED: 'ERR_UPGRADE_FAILED',
  UNINSTALL_FAILED: 'ERR_UNINSTALL_FAILED',
  ROLLBACK_FAILED: 'ERR_ROLLBACK_FAILED',
  
  // Repository errors
  REPO_NOT_FOUND: 'ERR_REPO_NOT_FOUND',
  REPO_SYNC_FAILED: 'ERR_REPO_SYNC_FAILED',
  REPO_AUTH_FAILED: 'ERR_REPO_AUTH_FAILED',
  
  // Chart errors
  CHART_NOT_FOUND: 'ERR_CHART_NOT_FOUND',
  CHART_DOWNLOAD_FAILED: 'ERR_CHART_DOWNLOAD_FAILED',
  CHART_VALUES_INVALID: 'ERR_CHART_VALUES_INVALID',

  // Validation errors
  REQUIRED_FIELD: 'ERR_REQUIRED_FIELD',
  INVALID_FORMAT: 'ERR_INVALID_FORMAT',
  INVALID_VALUE: 'ERR_INVALID_VALUE',
  INVALID_LENGTH: 'ERR_INVALID_LENGTH'
} as const;

// === Event Names (for event bus) ===
export const EVENT_NAMES = {
  APP_INSTALLED: 'app:installed',
  APP_UPGRADED: 'app:upgraded',
  APP_UNINSTALLED: 'app:uninstalled',
  APP_FAILED: 'app:failed',
  
  CLUSTER_CONNECTED: 'cluster:connected',
  CLUSTER_DISCONNECTED: 'cluster:disconnected',
  
  REPO_SYNCED: 'repo:synced',
  REPO_SYNC_FAILED: 'repo:sync-failed',
  
  DISCOVERY_STARTED: 'discovery:started',
  DISCOVERY_COMPLETED: 'discovery:completed',
  DISCOVERY_FAILED: 'discovery:failed'
} as const;

// === Local Storage Keys ===
export const STORAGE_KEYS = {
  SETTINGS: 'suseai-settings',
  UI_STATE: 'suseai-ui-state',
  FILTERS: 'suseai-filters',
  FAVORITES: 'suseai-favorites',
  LAST_DISCOVERY: 'suseai-last-discovery'
} as const;

// === Operator Service Coordinates ===
// These constants build the Rancher proxy URL used by operator-api.ts.
// OPERATOR_NAMESPACE must match the Helm release namespace (default: aif-operator).
export const MANAGEMENT_CLUSTER  = 'local';
export const OPERATOR_NAMESPACE  = 'aif-operator';
export const OPERATOR_SERVICE    = 'aif-operator';
export const OPERATOR_PORT       = 8080;

// === Type Exports (for TypeScript) ===
export type ProductSlug = typeof PRODUCT_SLUG;
export type StoreModule = typeof STORE_MODULES[keyof typeof STORE_MODULES];
export type RouteName = typeof ROUTE_NAMES[keyof typeof ROUTE_NAMES];
export type DiscoveryStage = typeof DISCOVERY_STAGES[keyof typeof DISCOVERY_STAGES];
export type LoadingState = typeof LOADING_STATES[keyof typeof LOADING_STATES];
export type InstallationStatus = typeof INSTALLATION_STATUS[keyof typeof INSTALLATION_STATUS];
export type AppStatus = typeof APP_STATUS[keyof typeof APP_STATUS];
export type HealthStatus = typeof HEALTH_STATUS[keyof typeof HEALTH_STATUS];
export type ConnectionStatus = typeof CONNECTION_STATUS[keyof typeof CONNECTION_STATUS];
export type RepositoryType = typeof REPOSITORY_TYPE[keyof typeof REPOSITORY_TYPE];
export type RepositoryStatus = typeof REPOSITORY_STATUS[keyof typeof REPOSITORY_STATUS];
export type SyncStatus = typeof SYNC_STATUS[keyof typeof SYNC_STATUS];
export type OperationType = typeof OPERATION_TYPE[keyof typeof OPERATION_TYPE];
export type ViewMode = typeof VIEW_MODES[keyof typeof VIEW_MODES];
export type SortDirection = typeof SORT_DIRECTIONS[keyof typeof SORT_DIRECTIONS];
export type AppSortField = typeof APP_SORT_FIELDS[keyof typeof APP_SORT_FIELDS];
export type NotificationType = typeof NOTIFICATION_TYPE[keyof typeof NOTIFICATION_TYPE];
export type FeatureFlag = typeof FEATURE_FLAGS[keyof typeof FEATURE_FLAGS];
export type AppCategory = typeof APP_CATEGORIES[keyof typeof APP_CATEGORIES];
export type ErrorCode = typeof ERROR_CODES[keyof typeof ERROR_CODES];
export type EventName = typeof EVENT_NAMES[keyof typeof EVENT_NAMES];
export type StorageKey = typeof STORAGE_KEYS[keyof typeof STORAGE_KEYS];
