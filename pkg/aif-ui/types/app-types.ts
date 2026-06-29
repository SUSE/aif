/**
 * App-related TypeScript interfaces and types
 * Provides comprehensive type definitions for app domain
 */

// === App Status and State Types ===

export type AppStatus = 
  | 'available'
  | 'installing' 
  | 'deployed'
  | 'upgrading'
  | 'uninstalling'
  | 'failed'
  | 'unknown';

export type AppState = 
  | 'active'
  | 'pending'
  | 'error' 
  | 'transitioning'
  | 'inactive';

export type InstallationStatus = 
  | 'pending'
  | 'installing'
  | 'deployed'
  | 'upgrading' 
  | 'uninstalling'
  | 'failed'
  | 'superseded'
  | 'unknown';

// === App Core Interfaces ===

export interface AppMetadata {
  name: string;
  displayName?: string;
  description?: string;
  version?: string;
  appVersion?: string;
  icon?: string;
  home?: string;
  sources?: string[];
  keywords?: string[];
  category?: string;
  maintainers?: AppMaintainer[];
  annotations?: Record<string, string>;
  labels?: Record<string, string>;
}

export interface AppMaintainer {
  name: string;
  email?: string;
  url?: string;
}

export interface AppRepository {
  name: string;
  url: string;
  type: 'helm' | 'oci' | 'git';
  branch?: string;
  path?: string;
  credentials?: {
    username?: string;
    password?: string;
    token?: string;
  };
}

export interface AppVersionInfo {
  version: string;
  appVersion?: string;
  description?: string;
  created: string;
  digest?: string;
  deprecated?: boolean;
  urls: string[];
  dependencies?: AppDependency[];
  maintainers?: AppMaintainer[];
  sources?: string[];
  keywords?: string[];
  annotations?: Record<string, string>;
}

export interface AppDependency {
  name: string;
  version: string;
  repository?: string;
  condition?: string;
  tags?: string[];
  enabled?: boolean;
  alias?: string;
}

// === Installation Related Types ===

export interface AppInstallationInfo {
  clusterId: string;
  namespace: string;
  releaseName: string;
  appId?: string; // Added to track which app this installation belongs to
  status: InstallationStatus;
  version?: string;
  chartVersion?: string;
  appVersion?: string;
  lastDeployed?: string;
  notes?: string;
  values?: Record<string, any>;
  userValues?: Record<string, any>;
  
  // Progress tracking
  progress?: InstallationProgress;
  events?: InstallationEvent[];
  resources?: InstallationResource[];
  
  // Error information
  error?: InstallationError;
  
  // Timestamps
  createdAt: string;
  updatedAt: string;
  lastHealthCheck?: string;
}

export interface InstallationProgress {
  phase: InstallationStatus;
  progress: number; // 0-100
  message: string;
  startedAt?: string;
  completedAt?: string;
  estimatedDuration?: number; // in seconds
}

export interface InstallationEvent {
  timestamp: string;
  phase: InstallationStatus;
  message: string;
  type: 'info' | 'warning' | 'error';
  source?: string;
}

export interface InstallationResource {
  kind: string;
  apiVersion?: string;
  name: string;
  namespace: string;
  ready: boolean;
  status: string;
  message?: string;
  conditions?: ResourceCondition[];
}

export interface ResourceCondition {
  type: string;
  status: 'True' | 'False' | 'Unknown';
  lastTransitionTime?: string;
  reason?: string;
  message?: string;
}

export interface InstallationError {
  code: string;
  message: string;
  details?: any;
  retryable: boolean;
  timestamp: string;
  phase?: InstallationStatus;
}

// === Installation Options ===

export interface InstallOptions {
  clusterId: string;
  namespace: string;
  releaseName: string;
  chartVersion?: string;
  values?: Record<string, any>;
  dryRun?: boolean;
  wait?: boolean;
  timeout?: number; // in seconds
  createNamespace?: boolean;
  skipCRDs?: boolean;
  atomic?: boolean;
  cleanupOnFail?: boolean;
  force?: boolean;
  resetValues?: boolean;
  reuseValues?: boolean;
  description?: string;
}

export interface UpgradeOptions extends Omit<InstallOptions, 'createNamespace'> {
  force?: boolean;
  resetValues?: boolean;
  reuseValues?: boolean;
  recreatePods?: boolean;
  maxHistory?: number;
}

export interface UninstallOptions {
  clusterId: string;
  namespace: string;
  releaseName: string;
  keepHistory?: boolean;
  dryRun?: boolean;
  timeout?: number;
  cascade?: 'background' | 'foreground' | 'orphan';
}

export interface RollbackOptions {
  clusterId: string;
  namespace: string;
  releaseName: string;
  revision?: number;
  dryRun?: boolean;
  wait?: boolean;
  timeout?: number;
  cleanupOnFail?: boolean;
  force?: boolean;
  recreatePods?: boolean;
}

// === App Statistics and Analytics ===

export interface AppStats {
  totalInstallations: number;
  activeInstallations: number;
  failedInstallations: number;
  clusters: string[];
  namespaces: string[];
  lastInstalled?: string;
  lastUpgraded?: string;
  averageInstallTime?: number; // in seconds
  successRate: number; // 0-1
  popularityScore: number; // 0-100
  downloadCount?: number;
  rating?: {
    average: number;
    count: number;
    distribution: Record<number, number>; // rating -> count
  };
}

export interface AppHealth {
  overall: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  ready: number;
  total: number;
  issues: AppHealthIssue[];
  lastCheck: string;
  checks: AppHealthCheck[];
}

export interface AppHealthIssue {
  type: 'error' | 'warning' | 'info';
  message: string;
  resource?: {
    kind: string;
    name: string;
    namespace: string;
  };
  cluster?: string;
  timestamp: string;
}

export interface AppHealthCheck {
  name: string;
  status: 'passing' | 'failing' | 'unknown';
  message?: string;
  lastCheck: string;
  cluster: string;
  namespace: string;
}

// === App Collection and Filtering ===

export interface AppFilter {
  status?: AppStatus[];
  state?: AppState[];
  clusters?: string[];
  namespaces?: string[];
  categories?: string[];
  keywords?: string[];
  repositories?: string[];
  maintainers?: string[];
  searchText?: string;
  hasInstallations?: boolean;
  isOfficial?: boolean;
  isVerified?: boolean;
  isPopular?: boolean;
  updatedAfter?: string;
  updatedBefore?: string;
}

export interface AppSortOptions {
  field: 'name' | 'status' | 'version' | 'updated' | 'popularity' | 'installs' | 'rating';
  direction: 'asc' | 'desc';
  secondary?: {
    field: AppSortOptions['field'];
    direction: 'asc' | 'desc';
  };
}

export interface AppQueryOptions {
  filter?: AppFilter;
  sort?: AppSortOptions;
  pagination?: {
    page: number;
    limit: number;
  };
  include?: ('installations' | 'stats' | 'health' | 'versions')[];
}

export interface AppQueryResult {
  apps: AppSummary[];
  total: number;
  page: number;
  limit: number;
  hasMore: boolean;
  filters: AppFilter;
  sort: AppSortOptions;
}

// === App Summary and List Types ===

export interface AppSummary {
  id: string;
  name: string;
  displayName?: string;
  description?: string;
  icon?: string;
  version?: string;
  appVersion?: string;
  category?: string;
  keywords?: string[];
  repository: {
    name: string;
    type: string;
  };
  status: AppStatus;
  state: AppState;
  installations: AppInstallationSummary[];
  stats?: AppStatsSummary;
  health?: AppHealthSummary;
  flags: AppFlags;
  updated: string;
  created?: string;
}

export interface AppInstallationSummary {
  clusterId: string;
  clusterName?: string;
  namespace: string;
  releaseName: string;
  status: InstallationStatus;
  version?: string;
  lastDeployed?: string;
  ready: boolean;
  error?: string;
}

export interface AppStatsSummary {
  installCount: number;
  clusterCount: number;
  popularityScore: number;
  successRate: number;
  averageRating?: number;
}

export interface AppHealthSummary {
  overall: AppHealth['overall'];
  ready: number;
  total: number;
  issueCount: number;
  lastCheck: string;
}

export interface AppFlags {
  isInstalled: boolean;
  isRunning: boolean;
  hasFailed: boolean;
  isTransitioning: boolean;
  isOfficial: boolean;
  isVerified: boolean;
  isPopular: boolean;
  isDeprecated: boolean;
  hasUpdates: boolean;
  needsAttention: boolean;
}

// === App Actions and Operations ===

export interface AppAction {
  name: string;
  label: string;
  description?: string;
  icon?: string;
  enabled: boolean;
  loading?: boolean;
  dangerous?: boolean;
  bulk?: boolean;
  requiresConfirmation?: boolean;
  confirmationMessage?: string;
  execute: (options?: any) => Promise<void>;
}

export interface AppBulkAction extends AppAction {
  bulk: true;
  execute: (apps: AppSummary[], options?: any) => Promise<void>;
}

export interface AppContextMenuAction extends AppAction {
  separator?: boolean;
  submenu?: AppContextMenuAction[];
}

// === App Configuration and Settings ===

export interface AppConfiguration {
  defaultNamespace: string;
  defaultTimeout: number;
  autoCreateNamespace: boolean;
  skipCRDs: boolean;
  atomic: boolean;
  cleanupOnFail: boolean;
  maxHistory: number;
  dryRunFirst: boolean;
  notifications: {
    onInstall: boolean;
    onUpgrade: boolean;
    onUninstall: boolean;
    onFailure: boolean;
  };
  healthCheck: {
    enabled: boolean;
    interval: number; // in minutes
    timeout: number; // in seconds
  };
}

export interface AppPreferences {
  defaultView: 'grid' | 'list' | 'table';
  itemsPerPage: number;
  showDescriptions: boolean;
  showIcons: boolean;
  showStats: boolean;
  showHealth: boolean;
  groupBy: 'none' | 'category' | 'repository' | 'status' | 'cluster';
  defaultFilters: AppFilter;
  defaultSort: AppSortOptions;
  autoRefreshInterval?: number; // in seconds
}

// === App Events and Notifications ===

export interface AppEvent {
  id: string;
  type: 'install' | 'upgrade' | 'uninstall' | 'rollback' | 'health' | 'error';
  appId: string;
  appName: string;
  clusterId: string;
  clusterName?: string;
  namespace: string;
  releaseName: string;
  message: string;
  details?: Record<string, any>;
  timestamp: string;
  user?: string;
  severity: 'info' | 'warning' | 'error' | 'success';
  read: boolean;
}

export interface AppNotification {
  id: string;
  title: string;
  message: string;
  type: 'info' | 'success' | 'warning' | 'error';
  app?: {
    id: string;
    name: string;
    clusterId: string;
    namespace: string;
  };
  timestamp: string;
  persistent?: boolean;
  actions?: Array<{
    label: string;
    action: () => void;
    primary?: boolean;
  }>;
}

// === Validation and Schema Types ===

export interface AppValidationResult {
  valid: boolean;
  errors: AppValidationError[];
  warnings: AppValidationWarning[];
}

export interface AppValidationError {
  field: string;
  message: string;
  code: string;
  value?: any;
}

export interface AppValidationWarning {
  field: string;
  message: string;
  suggestion?: string;
}

export interface AppFormField {
  name: string;
  label: string;
  type: 'string' | 'number' | 'boolean' | 'array' | 'object' | 'select' | 'multiselect';
  description?: string;
  placeholder?: string;
  required?: boolean;
  disabled?: boolean;
  hidden?: boolean;
  default?: any;
  validation?: {
    min?: number;
    max?: number;
    pattern?: string;
    custom?: (value: any) => string | null;
  };
  options?: Array<{ label: string; value: any; disabled?: boolean }>;
  dependsOn?: string;
  showWhen?: (values: Record<string, any>) => boolean;
}

export interface AppFormSchema {
  fields: AppFormField[];
  sections?: Array<{
    title: string;
    description?: string;
    fields: string[];
    collapsible?: boolean;
    collapsed?: boolean;
  }>;
  validation?: (values: Record<string, any>) => AppValidationResult;
}

// === API Response Types ===

export interface ApiResponse<T = any> {
  data: T;
  success: boolean;
  message?: string;
  errors?: string[];
  warnings?: string[];
  metadata?: {
    total?: number;
    page?: number;
    limit?: number;
    hasMore?: boolean;
    requestId?: string;
    timestamp: string;
  };
}

export interface ApiError {
  code: string;
  message: string;
  details?: any;
  field?: string;
  timestamp: string;
  requestId?: string;
}

// === Utility Types ===

export type AppId = string;
export type ClusterId = string;
export type NamespaceName = string;
export type ReleaseName = string;
export type AppVersionString = string;

export type PartialBy<T, K extends keyof T> = Omit<T, K> & Partial<Pick<T, K>>;
export type RequiredBy<T, K extends keyof T> = T & Required<Pick<T, K>>;

// === Type Guards ===

export function isAppStatus(value: any): value is AppStatus {
  return typeof value === 'string' && [
    'available', 'installing', 'deployed', 'upgrading', 
    'uninstalling', 'failed', 'unknown'
  ].includes(value);
}

export function isInstallationStatus(value: any): value is InstallationStatus {
  return typeof value === 'string' && [
    'pending', 'installing', 'deployed', 'upgrading',
    'uninstalling', 'failed', 'superseded', 'unknown'
  ].includes(value);
}

export function isAppAction(value: any): value is AppAction {
  return value && 
    typeof value.name === 'string' &&
    typeof value.label === 'string' &&
    typeof value.enabled === 'boolean' &&
    typeof value.execute === 'function';
}

export function hasInstallationInfo(app: AppSummary): app is AppSummary & { installations: AppInstallationSummary[] } {
  return app.installations && app.installations.length > 0;
}

export function hasAppStats(app: AppSummary): app is AppSummary & { stats: AppStatsSummary } {
  return !!app.stats;
}

export function hasAppHealth(app: AppSummary): app is AppSummary & { health: AppHealthSummary } {
  return !!app.health;
}