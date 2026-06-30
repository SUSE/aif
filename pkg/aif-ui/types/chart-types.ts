/**
 * Chart-related TypeScript interfaces and types
 * Provides comprehensive type definitions for chart domain
 */

// === Chart Core Types ===

export type ChartStatus = 
  | 'available'
  | 'deprecated'
  | 'unavailable'
  | 'loading'
  | 'error'
  | 'unknown';

export type RepositoryType = 
  | 'helm'
  | 'oci'
  | 'git'
  | 'local';

export type RepositoryStatus = 
  | 'ready'
  | 'syncing'
  | 'error'
  | 'unknown';

export type ValueType = 
  | 'string'
  | 'number'
  | 'boolean'
  | 'array'
  | 'object'
  | 'select'
  | 'multiselect'
  | 'password'
  | 'textarea'
  | 'json'
  | 'yaml';

// === Chart Repository ===

export interface ChartRepository {
  name: string;
  url: string;
  type: RepositoryType;
  status: RepositoryStatus;
  
  // Authentication
  username?: string;
  password?: string;
  token?: string;
  caFile?: string;
  certFile?: string;
  keyFile?: string;
  insecureSkipTLSVerify?: boolean;
  
  // OCI specific
  ociRegistry?: string;
  
  // Git specific
  branch?: string;
  path?: string;
  sshKey?: string;
  
  // Metadata
  description?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  
  // Status information
  lastSync?: string;
  lastError?: string;
  syncInProgress: boolean;
  chartCount: number;
  
  // Statistics
  downloadCount?: number;
  popularCharts?: string[];
  
  // Health
  healthy: boolean;
  healthMessage?: string;
  lastHealthCheck?: string;
  
  // Timestamps
  created: string;
  updated: string;
}

// === Chart Metadata ===

export interface ChartMetadata {
  name: string;
  version: string;
  displayName?: string;
  description?: string;
  home?: string;
  icon?: string;
  appVersion?: string;
  
  // Classification
  type?: 'application' | 'library';
  keywords: string[];
  category?: string;
  
  // Maintainers
  maintainers: ChartMaintainer[];
  
  // Sources and documentation
  sources: string[];
  urls: string[];
  
  // Dependencies
  dependencies?: ChartDependency[];
  
  // Conditions
  condition?: string;
  tags?: string[];
  
  // Metadata
  annotations?: Record<string, string>;
  
  // Status flags
  deprecated?: boolean;
  tillerVersion?: string;
  kubeVersion?: string;
  engine?: string;
}

export interface ChartMaintainer {
  name: string;
  email?: string;
  url?: string;
  organization?: string;
  avatar?: string;
}

export interface ChartDependency {
  name: string;
  version: string;
  repository?: string;
  condition?: string;
  tags?: string[];
  enabled?: boolean;
  alias?: string;
  importValues?: any;
}

// === Chart Versions ===

export interface ChartVersionInfo {
  version: string;
  appVersion?: string;
  description?: string;
  
  // Metadata
  created: string;
  digest: string;
  urls: string[];
  
  // Status
  deprecated?: boolean;
  prerelease?: boolean;
  
  // Dependencies
  dependencies?: ChartDependency[];
  
  // Maintainers (can differ per version)
  maintainers?: ChartMaintainer[];
  
  // Sources (can differ per version)
  sources?: string[];
  keywords?: string[];
  
  // Annotations (can differ per version)
  annotations?: Record<string, string>;
  
  // Size information
  size?: number; // in bytes
  
  // Downloads
  downloadCount?: number;
  
  // Compatibility
  kubeVersion?: string;
  kubeVersions?: string[];
}

export interface ChartVersionComparison {
  oldVersion: ChartVersionInfo;
  newVersion: ChartVersionInfo;
  changes: ChartVersionChange[];
  compatibility: {
    breaking: boolean;
    reason?: string;
  };
  upgradeNotes?: string;
}

export interface ChartVersionChange {
  type: 'added' | 'removed' | 'modified' | 'deprecated';
  category: 'dependency' | 'value' | 'template' | 'metadata' | 'breaking';
  description: string;
  path?: string;
  severity: 'low' | 'medium' | 'high' | 'critical';
}

// === Chart Values ===

export interface ChartValueSchema {
  type: ValueType;
  title?: string;
  description?: string;
  
  // Default and examples
  default?: any;
  examples?: any[];
  
  // Validation
  required?: boolean;
  pattern?: string;
  format?: string;
  enum?: any[];
  
  // Numeric constraints
  minimum?: number;
  maximum?: number;
  multipleOf?: number;
  
  // String constraints
  minLength?: number;
  maxLength?: number;
  
  // Array constraints
  minItems?: number;
  maxItems?: number;
  uniqueItems?: boolean;
  items?: ChartValueSchema;
  
  // Object constraints
  properties?: Record<string, ChartValueSchema>;
  additionalProperties?: boolean | ChartValueSchema;
  minProperties?: number;
  maxProperties?: number;
  
  // UI hints
  widget?: 'input' | 'textarea' | 'select' | 'multiselect' | 'checkbox' | 'radio' | 'slider' | 'file';
  placeholder?: string;
  helpText?: string;
  group?: string;
  order?: number;
  hidden?: boolean;
  advanced?: boolean;
  
  // Conditional display
  showIf?: Record<string, any>;
  hideIf?: Record<string, any>;
  dependsOn?: string[];
  
  // Options for select/multiselect
  options?: ValueOption[];
  
  // Dynamic options
  optionsFrom?: {
    api?: string;
    values?: string;
    cluster?: boolean;
  };
}

export interface ValueOption {
  label: string;
  value: any;
  description?: string;
  disabled?: boolean;
  group?: string;
}

export interface ChartValues {
  raw: Record<string, any>;
  computed: Record<string, any>;
  user: Record<string, any>;
  defaults: Record<string, any>;
  schema?: Record<string, ChartValueSchema>;
  
  // Validation
  valid: boolean;
  errors: ValueValidationError[];
  warnings: ValueValidationWarning[];
  
  // Processing metadata
  processed: boolean;
  processedAt?: string;
}

export interface ValueValidationError {
  path: string;
  message: string;
  value: any;
  schema?: ChartValueSchema;
  code: string;
}

export interface ValueValidationWarning {
  path: string;
  message: string;
  suggestion?: string;
  value: any;
}

export interface ValuesDiff {
  path: string;
  oldValue: any;
  newValue: any;
  type: 'added' | 'removed' | 'modified';
  impact: 'low' | 'medium' | 'high';
  description?: string;
}

// === Chart Statistics ===

export interface ChartStatistics {
  // Download metrics
  totalDownloads: number;
  downloadsThisMonth: number;
  downloadsThisWeek: number;
  downloadTrend: 'up' | 'down' | 'stable';
  
  // Installation metrics
  totalInstalls: number;
  activeInstalls: number;
  successfulInstalls: number;
  failedInstalls: number;
  successRate: number; // 0-1
  
  // Version metrics
  totalVersions: number;
  latestVersion: string;
  oldestVersion: string;
  versionsLastYear: number;
  releaseFrequency: number; // releases per month
  
  // User engagement
  rating?: ChartRating;
  reviews?: ChartReview[];
  stars?: number;
  watchers?: number;
  forks?: number;
  
  // Usage patterns
  popularVersions: PopularVersion[];
  popularClusters: PopularCluster[];
  installationsByRegion?: Record<string, number>;
  
  // Popularity metrics
  popularityScore: number; // 0-100
  trendingScore: number; // 0-100
  maturityScore: number; // 0-100
  
  // Time series data
  downloadHistory?: TimeSeriesPoint[];
  installHistory?: TimeSeriesPoint[];
  ratingHistory?: TimeSeriesPoint[];
}

export interface ChartRating {
  average: number;
  count: number;
  distribution: Record<number, number>; // rating (1-5) -> count
}

export interface ChartReview {
  id: string;
  user: string;
  rating: number;
  title?: string;
  comment: string;
  version: string;
  verified: boolean;
  helpful: number;
  timestamp: string;
  
  // Context
  cluster?: string;
  useCase?: string;
  pros?: string[];
  cons?: string[];
}

export interface PopularVersion {
  version: string;
  percentage: number;
  installCount: number;
}

export interface PopularCluster {
  clusterId: string;
  clusterName?: string;
  installCount: number;
  lastInstall: string;
}

export interface TimeSeriesPoint {
  timestamp: string;
  value: number;
  label?: string;
}

// === Chart Quality and Health ===

export interface ChartQuality {
  overall: 'excellent' | 'good' | 'fair' | 'poor';
  score: number; // 0-100
  
  // Quality dimensions
  documentation: QualityScore;
  metadata: QualityScore;
  values: QualityScore;
  templates: QualityScore;
  testing: QualityScore;
  security: QualityScore;
  maintenance: QualityScore;
  
  // Issues
  issues: QualityIssue[];
  recommendations: QualityRecommendation[];
  
  // Certifications
  certifications: ChartCertification[];
  
  // Last assessment
  lastAssessed: string;
  assessmentVersion: string;
}

export interface QualityScore {
  score: number; // 0-100
  weight: number; // contribution to overall score
  status: 'excellent' | 'good' | 'fair' | 'poor';
  issues: string[];
  recommendations: string[];
}

export interface QualityIssue {
  category: 'documentation' | 'metadata' | 'values' | 'templates' | 'security' | 'maintenance';
  severity: 'critical' | 'high' | 'medium' | 'low' | 'info';
  message: string;
  file?: string;
  line?: number;
  rule?: string;
  fix?: string;
}

export interface QualityRecommendation {
  category: string;
  priority: 'high' | 'medium' | 'low';
  title: string;
  description: string;
  action: string;
  benefit: string;
}

export interface ChartCertification {
  type: 'official' | 'verified' | 'partner' | 'community';
  issuer: string;
  issuedAt: string;
  validUntil?: string;
  criteria: string[];
  badge?: string;
}

// === Chart Discovery and Search ===

export interface ChartSearchQuery {
  // Text search
  query?: string;
  
  // Filters
  repositories?: string[];
  categories?: string[];
  keywords?: string[];
  maintainers?: string[];
  
  // Status filters
  status?: ChartStatus[];
  deprecated?: boolean;
  official?: boolean;
  verified?: boolean;
  
  // Version filters
  hasVersions?: boolean;
  latestVersionOnly?: boolean;
  versionPattern?: string;
  
  // Popularity filters
  minRating?: number;
  minDownloads?: number;
  minInstalls?: number;
  popularityThreshold?: number;
  
  // Date filters
  updatedAfter?: string;
  updatedBefore?: string;
  createdAfter?: string;
  createdBefore?: string;
  
  // Compatibility filters
  kubernetesVersion?: string;
  helmVersion?: string;
  architecture?: string;
  
  // Sorting
  sort?: ChartSortOptions;
  
  // Pagination
  page?: number;
  limit?: number;
  
  // Include additional data
  include?: ('versions' | 'stats' | 'quality' | 'values')[];
}

export interface ChartSortOptions {
  field: 'relevance' | 'name' | 'popularity' | 'rating' | 'downloads' | 'updated' | 'created';
  direction: 'asc' | 'desc';
  secondary?: {
    field: ChartSortOptions['field'];
    direction: 'asc' | 'desc';
  };
}

export interface ChartSearchResult {
  charts: ChartSummary[];
  total: number;
  page: number;
  limit: number;
  hasMore: boolean;
  facets: SearchFacets;
  suggestions: string[];
  query: ChartSearchQuery;
  searchTime: number; // in milliseconds
}

export interface SearchFacets {
  repositories: FacetCount[];
  categories: FacetCount[];
  keywords: FacetCount[];
  maintainers: FacetCount[];
  ratings: FacetCount[];
  statuses: FacetCount[];
}

export interface FacetCount {
  value: string;
  count: number;
  selected: boolean;
}

// === Chart Summary ===

export interface ChartSummary {
  id: string;
  name: string;
  displayName?: string;
  description?: string;
  icon?: string;
  category?: string;
  
  // Repository
  repository: {
    name: string;
    type: RepositoryType;
    url: string;
  };
  
  // Versions
  latestVersion: string;
  appVersion?: string;
  versionCount: number;
  hasPrerelease: boolean;
  
  // Status
  status: ChartStatus;
  deprecated: boolean;
  
  // Quality indicators
  official: boolean;
  verified: boolean;
  quality?: {
    score: number;
    status: string;
  };
  
  // Statistics
  stats?: {
    downloads: number;
    installs: number;
    rating?: number;
    popularity: number;
  };
  
  // Maintainers
  maintainers: string[];
  
  // Keywords and classification
  keywords: string[];
  
  // Timestamps
  created: string;
  updated: string;
  
  // Flags
  flags: ChartFlags;
}

export interface ChartFlags {
  isPopular: boolean;
  isTrending: boolean;
  isNew: boolean;
  isUpdated: boolean;
  hasBreakingChanges: boolean;
  needsAttention: boolean;
  isCompatible: boolean;
  hasDocumentation: boolean;
  isWellMaintained: boolean;
}

// === Chart Collections ===

export interface ChartCollection {
  name: string;
  displayName?: string;
  description?: string;
  icon?: string;
  
  // Charts
  charts: string[]; // chart IDs
  chartCount: number;
  
  // Metadata
  tags: string[];
  category?: string;
  
  // Ownership
  owner?: string;
  organization?: string;
  visibility: 'public' | 'private' | 'org';
  
  // Stats
  followers?: number;
  stars?: number;
  
  // Timestamps
  created: string;
  updated: string;
}

export interface ChartCategory {
  name: string;
  displayName: string;
  description?: string;
  icon?: string;
  color?: string;
  
  // Hierarchy
  parent?: string;
  children?: string[];
  level: number;
  path: string;
  
  // Stats
  chartCount: number;
  popularCharts?: string[];
  
  // Metadata
  keywords?: string[];
  aliases?: string[];
}

// === Chart Operations ===

export interface ChartOperation {
  id: string;
  type: 'install' | 'upgrade' | 'uninstall' | 'test' | 'lint' | 'package';
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  
  // Context
  chartId: string;
  version?: string;
  clusterId?: string;
  namespace?: string;
  releaseName?: string;
  
  // Progress
  progress: number; // 0-100
  currentStep?: string;
  totalSteps?: number;
  
  // Results
  output?: string;
  error?: string;
  warnings?: string[];
  
  // Metadata
  initiatedBy?: string;
  
  // Timestamps
  startedAt: string;
  completedAt?: string;
  
  // Options
  options?: Record<string, any>;
}

// === API Response Types ===

export interface ChartApiResponse<T = any> {
  data: T;
  success: boolean;
  message?: string;
  errors?: string[];
  warnings?: string[];
  metadata?: {
    chartId?: string;
    version?: string;
    repository?: string;
    timestamp: string;
    requestId?: string;
  };
}

// === Utility Types ===

export type ChartId = string;
export type ChartName = string;
export type ChartVersionString = string;
export type RepositoryName = string;

// === Type Guards ===

export function isChartStatus(value: any): value is ChartStatus {
  return typeof value === 'string' && [
    'available', 'deprecated', 'unavailable', 'loading', 'error', 'unknown'
  ].includes(value);
}

export function isRepositoryType(value: any): value is RepositoryType {
  return typeof value === 'string' && [
    'helm', 'oci', 'git', 'local'
  ].includes(value);
}

export function isRepositoryStatus(value: any): value is RepositoryStatus {
  return typeof value === 'string' && [
    'ready', 'syncing', 'error', 'unknown'
  ].includes(value);
}

export function isValueType(value: any): value is ValueType {
  return typeof value === 'string' && [
    'string', 'number', 'boolean', 'array', 'object', 
    'select', 'multiselect', 'password', 'textarea', 'json', 'yaml'
  ].includes(value);
}

export function hasChartStats(chart: ChartSummary): chart is ChartSummary & { stats: NonNullable<ChartSummary['stats']> } {
  return !!chart.stats;
}

export function hasChartQuality(chart: ChartSummary): chart is ChartSummary & { quality: NonNullable<ChartSummary['quality']> } {
  return !!chart.quality;
}

export function isAvailable(chart: ChartSummary): boolean {
  return chart.status === 'available' && !chart.deprecated;
}

export function isPopular(chart: ChartSummary): boolean {
  return chart.flags.isPopular || (chart.stats?.popularity || 0) > 75;
}

export function isWellMaintained(chart: ChartSummary): boolean {
  const monthsOld = (Date.now() - new Date(chart.updated).getTime()) / (1000 * 60 * 60 * 24 * 30);
  return monthsOld < 6 && chart.flags.isWellMaintained;
}

export function isCompatible(chart: ChartSummary, kubernetesVersion?: string): boolean {
  // This would need actual version compatibility logic
  return chart.flags.isCompatible;
}