/**
 * Installation-related TypeScript interfaces and types
 * Provides comprehensive type definitions for installation management
 */

// === Installation Core Types ===

export type InstallationPhase = 
  | 'pending'
  | 'preparing'
  | 'installing'
  | 'deployed'
  | 'upgrading'
  | 'uninstalling'
  | 'rolling-back'
  | 'failed'
  | 'superseded'
  | 'unknown';

export type InstallationStatus = InstallationPhase;

export type InstallationType = 
  | 'install'
  | 'upgrade'
  | 'rollback'
  | 'uninstall';

export type HookPhase = 
  | 'pre-install'
  | 'post-install'
  | 'pre-delete'
  | 'post-delete'
  | 'pre-upgrade'
  | 'post-upgrade'
  | 'pre-rollback'
  | 'post-rollback'
  | 'test';

// === Installation Core Interfaces ===

export interface InstallationDetails {
  // Identity
  id: string;
  clusterId: string;
  clusterName?: string;
  namespace: string;
  releaseName: string;
  
  // Application information
  appId: string;
  appName: string;
  chartRepo: string;
  chartName: string;
  chartVersion: string;
  appVersion?: string;
  
  // Installation state
  status: InstallationStatus;
  revision: number;
  description?: string;
  notes?: string;
  
  // Configuration
  values: Record<string, any>;
  userValues: Record<string, any>;
  computedValues?: Record<string, any>;
  
  // Operation details
  type: InstallationType;
  dryRun: boolean;
  atomic: boolean;
  wait: boolean;
  timeout: number;
  createNamespace: boolean;
  skipCRDs: boolean;
  cleanupOnFail: boolean;
  force: boolean;
  resetValues: boolean;
  reuseValues: boolean;
  
  // Progress and monitoring
  progress?: InstallationProgress;
  events: InstallationEvent[];
  resources: InstallationResource[];
  hooks: InstallationHook[];
  
  // Error handling
  error?: InstallationError;
  warnings: string[];
  hasErrors: boolean;
  hasWarnings: boolean;
  
  // Health
  healthy: boolean;
  
  // User context
  installedBy?: string;
  installationNotes?: string;
  
  // Timestamps
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
  lastDeployed?: string;
  firstDeployed?: string;
  lastHealthCheck?: string;
}

export interface InstallationProgress {
  phase: InstallationStatus;
  step: string;
  progress: number; // 0-100
  message: string;
  
  // Time tracking
  startedAt: string;
  completedAt?: string;
  estimatedDuration?: number; // in seconds
  elapsedTime: number; // in seconds
  remainingTime?: number; // in seconds
  
  // Step tracking
  currentStep: number;
  totalSteps: number;
  steps: InstallationStep[];
  
  // Resource tracking
  resourcesProcessed: number;
  totalResources: number;
  
  // Hook tracking
  hooksExecuted: number;
  totalHooks: number;
  currentHook?: string;
}

export interface InstallationStep {
  name: string;
  description: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'skipped';
  startedAt?: string;
  completedAt?: string;
  duration?: number; // in seconds
  error?: string;
  
  // Sub-steps
  subSteps?: InstallationSubStep[];
  currentSubStep?: number;
}

export interface InstallationSubStep {
  name: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  message?: string;
  duration?: number; // in milliseconds
}

export interface InstallationEvent {
  id: string;
  timestamp: string;
  phase: InstallationStatus;
  type: 'info' | 'warning' | 'error' | 'debug';
  source: string;
  message: string;
  details?: Record<string, any>;
  
  // Context
  step?: string;
  resource?: {
    kind: string;
    name: string;
    namespace?: string;
  };
  hook?: string;
  
  // Correlation
  correlationId?: string;
  parentEventId?: string;
}

export interface InstallationResource {
  // Resource identity
  kind: string;
  apiVersion: string;
  name: string;
  namespace: string;
  uid?: string;
  
  // Status
  ready: boolean;
  status: string;
  phase?: string;
  message?: string;
  reason?: string;
  
  // Conditions
  conditions?: ResourceCondition[];
  
  // Dependencies
  dependsOn?: string[];
  dependents?: string[];
  
  // Metadata
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  ownerReferences?: OwnerReference[];
  
  // Health
  health?: ResourceHealth;
  
  // Timestamps
  createdAt: string;
  updatedAt: string;
  lastChecked: string;
}

export interface ResourceCondition {
  type: string;
  status: 'True' | 'False' | 'Unknown';
  lastTransitionTime: string;
  lastUpdateTime?: string;
  reason?: string;
  message?: string;
  observedGeneration?: number;
}

export interface OwnerReference {
  apiVersion: string;
  kind: string;
  name: string;
  uid: string;
  controller?: boolean;
  blockOwnerDeletion?: boolean;
}

export interface ResourceHealth {
  status: 'healthy' | 'progressing' | 'degraded' | 'suspended' | 'unknown';
  message?: string;
  lastCheck: string;
  
  // Specific resource checks
  checks: ResourceHealthCheck[];
}

export interface ResourceHealthCheck {
  name: string;
  status: 'passing' | 'warning' | 'critical' | 'unknown';
  message?: string;
  lastRun: string;
  duration?: number; // in milliseconds
}

export interface InstallationHook {
  name: string;
  kind: string;
  phase: HookPhase;
  weight: number;
  deletePolicy?: 'before-hook-creation' | 'hook-succeeded' | 'hook-failed';
  
  // Status
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'unknown';
  startedAt?: string;
  completedAt?: string;
  duration?: number; // in seconds
  
  // Output
  logs?: string;
  events?: InstallationEvent[];
  
  // Error details
  error?: {
    message: string;
    reason: string;
    exitCode?: number;
  };
}

export interface InstallationError {
  code: string;
  message: string;
  type: 'validation' | 'resource' | 'network' | 'permission' | 'timeout' | 'helm' | 'kubernetes' | 'unknown';
  phase: InstallationStatus;
  retryable: boolean;
  
  // Context
  resource?: {
    kind: string;
    name: string;
    namespace?: string;
  };
  hook?: string;
  step?: string;
  
  // Details
  details?: Record<string, any>;
  stackTrace?: string;
  suggestions?: string[];
  
  // Correlation
  timestamp: string;
  correlationId?: string;
}

// === Installation Options ===

export interface BaseInstallationOptions {
  clusterId: string;
  namespace: string;
  releaseName: string;
  
  // Chart information
  chartRepo: string;
  chartName: string;
  chartVersion?: string;
  
  // Values
  values?: Record<string, any>;
  valuesFiles?: string[];
  
  // Behavior options
  dryRun?: boolean;
  wait?: boolean;
  waitForJobs?: boolean;
  timeout?: number; // in seconds
  atomic?: boolean;
  skipCRDs?: boolean;
  createNamespace?: boolean;
  
  // Advanced options
  force?: boolean;
  disableHooks?: boolean;
  noHooks?: boolean;
  replace?: boolean;
  verify?: boolean;
  
  // Metadata
  description?: string;
  generateName?: boolean;
  
  // Hooks
  preInstallHook?: InstallationHookConfig;
  postInstallHook?: InstallationHookConfig;
}

export interface InstallOptions extends BaseInstallationOptions {
  // Install-specific options
  dependencyUpdate?: boolean;
  cleanupOnFail?: boolean;
  
  // Validation
  validate?: boolean;
  schemaValidation?: boolean;
}

export interface UpgradeOptions extends BaseInstallationOptions {
  // Upgrade-specific options
  install?: boolean; // Install if not exists
  resetValues?: boolean;
  reuseValues?: boolean;
  reuseBinary?: boolean;
  maxHistory?: number;
  historyMax?: number;
  
  // Rollback options
  cleanupOnFail?: boolean;
  force?: boolean;
  recreatePods?: boolean;
}

export interface RollbackOptions {
  clusterId: string;
  namespace: string;
  releaseName: string;
  
  // Rollback options
  revision?: number;
  dryRun?: boolean;
  wait?: boolean;
  timeout?: number;
  cleanupOnFail?: boolean;
  force?: boolean;
  recreatePods?: boolean;
  noHooks?: boolean;
  
  // History
  historyMax?: number;
}

export interface UninstallOptions {
  clusterId: string;
  namespace: string;
  releaseName: string;
  
  // Uninstall options
  keepHistory?: boolean;
  dryRun?: boolean;
  noHooks?: boolean;
  timeout?: number;
  wait?: boolean;
  
  // Cleanup options
  cascade?: 'background' | 'foreground' | 'orphan';
  gracePeriod?: number;
  ignoreDaemonSets?: boolean;
  deleteEmptyDirData?: boolean;
  force?: boolean;
  
  // Description
  description?: string;
}

export interface InstallationHookConfig {
  enabled: boolean;
  image?: string;
  command?: string[];
  args?: string[];
  env?: Record<string, string>;
  resources?: {
    requests?: Record<string, string>;
    limits?: Record<string, string>;
  };
  timeout?: number;
}

// === Installation History ===

export interface InstallationHistory {
  installations: InstallationHistoryEntry[];
  totalRevisions: number;
  currentRevision: number;
  oldestRevision: number;
}

export interface InstallationHistoryEntry {
  revision: number;
  status: InstallationStatus;
  description: string;
  chartVersion: string;
  appVersion?: string;
  
  // Timestamps
  updatedAt: string;
  deployedAt?: string;
  
  // Size and resources
  configSize: number;
  manifestSize: number;
  resourceCount: number;
  
  // User context
  installedBy?: string;
  
  // Metadata
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  
  // Rollback capability
  rollbackable: boolean;
  rollbackReason?: string;
}

// === Installation Metrics ===

export interface InstallationMetrics {
  // Duration metrics
  totalDuration: number; // in seconds
  phaseDurations: Record<InstallationStatus, number>;
  stepDurations: Record<string, number>;
  hookDurations: Record<string, number>;
  
  // Resource metrics
  resourceCounts: Record<string, number>; // kind -> count
  readyResourceCounts: Record<string, number>;
  
  // Event metrics
  eventCounts: Record<string, number>; // type -> count
  errorCounts: Record<string, number>; // phase -> count
  
  // Performance
  throughput: {
    resourcesPerSecond: number;
    hooksPerSecond: number;
  };
  
  // Success rates
  successRate: number; // 0-1
  retryCount: number;
  rollbackCount: number;
}

export interface InstallationAnalytics {
  // Aggregated metrics
  totalInstallations: number;
  successfulInstallations: number;
  failedInstallations: number;
  averageDuration: number;
  
  // Trends
  installationsOverTime: TimeSeriesData[];
  successRateOverTime: TimeSeriesData[];
  durationTrendOverTime: TimeSeriesData[];
  
  // Breakdowns
  installationsByCluster: Record<string, number>;
  installationsByNamespace: Record<string, number>;
  installationsByChart: Record<string, number>;
  installationsByStatus: Record<InstallationStatus, number>;
  
  // Error analysis
  commonErrors: ErrorFrequency[];
  errorsByPhase: Record<InstallationStatus, number>;
  errorsByResource: Record<string, number>;
  
  // Performance insights
  slowestInstallations: InstallationPerformanceData[];
  fastestInstallations: InstallationPerformanceData[];
  bottleneckAnalysis: BottleneckAnalysis[];
}

export interface TimeSeriesData {
  timestamp: string;
  value: number;
  label?: string;
}

export interface ErrorFrequency {
  error: string;
  count: number;
  percentage: number;
  examples: string[];
}

export interface InstallationPerformanceData {
  installationId: string;
  releaseName: string;
  chartName: string;
  clusterId: string;
  duration: number;
  phase: InstallationStatus;
  bottlenecks: string[];
}

export interface BottleneckAnalysis {
  phase: InstallationStatus;
  step?: string;
  averageDuration: number;
  percentile95Duration: number;
  frequency: number;
  suggestions: string[];
}

// === Installation Collections ===

export interface InstallationFilter {
  statuses?: InstallationStatus[];
  phases?: InstallationStatus[];
  types?: InstallationType[];
  clusters?: string[];
  namespaces?: string[];
  apps?: string[];
  charts?: string[];
  repos?: string[];
  installedBy?: string[];
  
  // Time filters
  createdAfter?: string;
  createdBefore?: string;
  updatedAfter?: string;
  updatedBefore?: string;
  completedAfter?: string;
  completedBefore?: string;
  
  // Status filters
  hasErrors?: boolean;
  hasWarnings?: boolean;
  needsAttention?: boolean;
  isHealthy?: boolean;
  isTransitioning?: boolean;
  
  // Progress filters
  progressMin?: number;
  progressMax?: number;
  durationMin?: number; // in seconds
  durationMax?: number; // in seconds
  
  // Search
  searchText?: string;
  
  // Labels and annotations
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface InstallationSortOptions {
  field: 'name' | 'status' | 'created' | 'updated' | 'completed' | 'duration' | 'progress' | 'cluster' | 'namespace';
  direction: 'asc' | 'desc';
  secondary?: {
    field: InstallationSortOptions['field'];
    direction: 'asc' | 'desc';
  };
}

export interface InstallationQueryOptions {
  filter?: InstallationFilter;
  sort?: InstallationSortOptions;
  pagination?: {
    page: number;
    limit: number;
  };
  include?: ('progress' | 'events' | 'resources' | 'hooks' | 'history' | 'metrics')[];
}

export interface InstallationQueryResult {
  installations: InstallationSummary[];
  total: number;
  page: number;
  limit: number;
  hasMore: boolean;
  filters: InstallationFilter;
  sort: InstallationSortOptions;
}

// === Installation Summary ===

export interface InstallationSummary {
  id: string;
  clusterId: string;
  clusterName?: string;
  namespace: string;
  releaseName: string;
  appId: string;
  appName: string;
  chartName: string;
  chartVersion: string;
  appVersion?: string;
  status: InstallationStatus;
  revision: number;
  
  // Progress
  progress?: number; // 0-100
  currentStep?: string;
  
  // Health
  healthy: boolean;
  readyResources: number;
  totalResources: number;
  
  // Flags
  hasErrors: boolean;
  hasWarnings: boolean;
  needsAttention: boolean;
  isTransitioning: boolean;
  
  // Times
  duration?: number; // in seconds
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  
  // User context
  installedBy?: string;
}

// === Utility Types ===

export type InstallationId = string;
export type ReleaseName = string;
export type RevisionNumber = number;

// === Type Guards ===

export function isInstallationStatus(value: any): value is InstallationStatus {
  return typeof value === 'string' && [
    'pending', 'preparing', 'installing', 'deployed', 'upgrading', 
    'uninstalling', 'rolling-back', 'failed', 'superseded', 'unknown'
  ].includes(value);
}

export function isInstallationType(value: any): value is InstallationType {
  return typeof value === 'string' && [
    'install', 'upgrade', 'rollback', 'uninstall'
  ].includes(value);
}

export function isHookPhase(value: any): value is HookPhase {
  return typeof value === 'string' && [
    'pre-install', 'post-install', 'pre-delete', 'post-delete',
    'pre-upgrade', 'post-upgrade', 'pre-rollback', 'post-rollback', 'test'
  ].includes(value);
}

export function isTransitioning(installation: InstallationSummary | InstallationDetails): boolean {
  return ['pending', 'preparing', 'installing', 'upgrading', 'uninstalling', 'rolling-back'].includes(installation.status);
}

export function isCompleted(installation: InstallationSummary | InstallationDetails): boolean {
  return ['deployed', 'failed', 'superseded'].includes(installation.status);
}

export function isSuccessful(installation: InstallationSummary | InstallationDetails): boolean {
  return installation.status === 'deployed';
}

export function isFailed(installation: InstallationSummary | InstallationDetails): boolean {
  return installation.status === 'failed';
}

export function needsAttention(installation: InstallationSummary | InstallationDetails): boolean {
  return installation.hasErrors || 
    installation.hasWarnings || 
    !installation.healthy ||
    (isTransitioning(installation) && (Date.now() - new Date(installation.updatedAt).getTime()) > 30 * 60 * 1000); // 30 minutes
}