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



// === App Statistics and Analytics ===

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


export interface AppContextMenuAction extends AppAction {
  separator?: boolean;
  submenu?: AppContextMenuAction[];
}

// === App Configuration and Settings ===


// === App Events and Notifications ===

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


// === API Response Types ===
