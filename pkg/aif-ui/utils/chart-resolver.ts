/**
 * Chart Resolution Logic for SUSE AI Extension
 * Handles Helm chart discovery, version resolution, and dependency management
 * Following Rancher UI patterns for chart handling
 */

import { REPOSITORY_TYPE, APP_CATEGORIES, BUILT_IN_REPOSITORIES } from './constants';
import { retry, isRetryableError } from './promise';
import type { RepositoryType, AppCategory } from './constants';

// === Chart Information Types ===
export interface ChartInfo {
  name: string;
  displayName?: string;
  description?: string;
  version: string;
  appVersion?: string;
  home?: string;
  icon?: string;
  keywords?: string[];
  sources?: string[];
  maintainers?: ChartMaintainer[];
  repository: ChartRepository;
  category?: AppCategory;
  deprecated?: boolean;
  annotations?: Record<string, string>;
  created?: string;
  updated?: string;
}

export interface ChartMaintainer {
  name: string;
  email?: string;
  url?: string;
}

export interface ChartRepository {
  name: string;
  url: string;
  type: RepositoryType;
  branch?: string;
  path?: string;
  credentials?: ChartCredentials;
}

export interface ChartCredentials {
  username?: string;
  password?: string;
  token?: string;
  keyFile?: string;
}

export interface ChartVersion {
  version: string;
  appVersion?: string;
  description?: string;
  created: string;
  digest?: string;
  urls: string[];
  deprecated?: boolean;
  dependencies?: ChartDependency[];
  annotations?: Record<string, string>;
}

export interface ChartDependency {
  name: string;
  version: string;
  repository?: string;
  condition?: string;
  tags?: string[];
  enabled?: boolean;
  alias?: string;
}

export interface ChartValues {
  raw: string;
  parsed: Record<string, any>;
  schema?: ChartValuesSchema;
}

export interface ChartValuesSchema {
  [key: string]: ChartValueField;
}

export interface ChartValueField {
  type: 'string' | 'number' | 'boolean' | 'array' | 'object';
  description?: string;
  default?: any;
  required?: boolean;
  enum?: any[];
  minimum?: number;
  maximum?: number;
  pattern?: string;
  items?: ChartValueField;
  properties?: ChartValuesSchema;
  examples?: any[];
}

// === Chart Resolution Options ===
export interface ChartSearchOptions {
  query?: string;
  repositories?: string[];
  categories?: AppCategory[];
  keywords?: string[];
  deprecated?: boolean;
  limit?: number;
  offset?: number;
  sortBy?: 'name' | 'version' | 'created' | 'popularity';
  sortDirection?: 'asc' | 'desc';
}

export interface ChartResolveOptions {
  version?: string;
  includeDeprecated?: boolean;
  includePrereleases?: boolean;
  maxAge?: number; // Cache max age in seconds
}

export interface VersionConstraint {
  constraint: string; // e.g., ">=1.2.0", "~1.2.0", "^1.2.0"
  satisfied: boolean;
  availableVersions: string[];
  recommendedVersion?: string;
}

// === Chart Resolution Results ===
export interface ChartSearchResult {
  charts: ChartInfo[];
  total: number;
  repositories: string[];
  categories: string[];
}

export interface ChartResolveResult {
  chart: ChartInfo;
  versions: ChartVersion[];
  latestVersion: ChartVersion;
  recommendedVersion: ChartVersion;
  dependencies: ResolvedDependency[];
  values: ChartValues;
}

export interface ResolvedDependency {
  name: string;
  version: string;
  repository?: string;
  resolved: boolean;
  chart?: ChartInfo;
  error?: string;
}

// === Chart Repository Management ===

/**
 * Discover charts in a repository
 */
export async function discoverChartsInRepository(
  repository: ChartRepository,
  options: { useCache?: boolean; maxAge?: number } = {}
): Promise<ChartInfo[]> {
  const { useCache = true, maxAge = 3600 } = options; // 1 hour default cache
  
  try {
    // For Helm repositories
    if (repository.type === REPOSITORY_TYPE.HELM) {
      return await discoverHelmCharts(repository, { useCache, maxAge });
    }
    
    // For OCI repositories
    if (repository.type === REPOSITORY_TYPE.OCI) {
      return await discoverOCICharts(repository, { useCache, maxAge });
    }
    
    // For Git repositories
    if (repository.type === REPOSITORY_TYPE.GIT) {
      return await discoverGitCharts(repository, { useCache, maxAge });
    }
    
    throw new Error(`Unsupported repository type: ${repository.type}`);
    
  } catch (error) {
    console.error(`Failed to discover charts in repository ${repository.name}:`, error);
    throw error;
  }
}

/**
 * Discover Helm charts from HTTP repository
 */
async function discoverHelmCharts(
  repository: ChartRepository,
  options: { useCache: boolean; maxAge: number }
): Promise<ChartInfo[]> {
  const indexUrl = `${repository.url.replace(/\/$/, '')}/index.yaml`;
  
  try {
    // This would fetch the Helm repository index
    // For now, return mock data
    const charts: ChartInfo[] = [];
    
    // In a real implementation: fetch and parse Helm repository index
    
    return charts;
    
  } catch (error) {
    if (isRetryableError(error)) {
      return await retry(
        () => discoverHelmCharts(repository, options),
        { maxAttempts: 3, retryCondition: isRetryableError }
      );
    }
    throw error;
  }
}

/**
 * Discover OCI charts from registry
 */
async function discoverOCICharts(
  repository: ChartRepository,
  options: { useCache: boolean; maxAge: number }
): Promise<ChartInfo[]> {
  // Implementation for OCI registry chart discovery
  // This would use Docker registry API to discover charts
  return [];
}

/**
 * Discover Git charts from repository
 */
async function discoverGitCharts(
  repository: ChartRepository,
  options: { useCache: boolean; maxAge: number }
): Promise<ChartInfo[]> {
  // Implementation for Git repository chart discovery
  // This would clone/fetch the repository and scan for Chart.yaml files
  return [];
}

// === Chart Search and Filtering ===

/**
 * Search for charts across repositories
 */
export async function searchCharts(
  repositories: ChartRepository[],
  options: ChartSearchOptions = {}
): Promise<ChartSearchResult> {
  const {
    query,
    repositories: repoFilter,
    categories,
    keywords,
    deprecated = false,
    limit = 50,
    offset = 0,
    sortBy = 'name',
    sortDirection = 'asc'
  } = options;
  
  let allCharts: ChartInfo[] = [];
  const searchRepos = repoFilter ? 
    repositories.filter(r => repoFilter.includes(r.name)) : 
    repositories;
  
  // Discover charts from all repositories
  const chartPromises = searchRepos.map(repo => 
    discoverChartsInRepository(repo).catch(error => {
      console.warn(`Failed to search repository ${repo.name}:`, error);
      return [] as ChartInfo[];
    })
  );
  
  const chartResults = await Promise.all(chartPromises);
  allCharts = chartResults.flat();
  
  // Apply filters
  let filteredCharts = allCharts;
  
  // Query filter
  if (query) {
    const queryLower = query.toLowerCase();
    filteredCharts = filteredCharts.filter(chart =>
      chart.name.toLowerCase().includes(queryLower) ||
      chart.displayName?.toLowerCase().includes(queryLower) ||
      chart.description?.toLowerCase().includes(queryLower) ||
      chart.keywords?.some(k => k.toLowerCase().includes(queryLower))
    );
  }
  
  // Category filter
  if (categories && categories.length > 0) {
    filteredCharts = filteredCharts.filter(chart =>
      chart.category && categories.includes(chart.category)
    );
  }
  
  // Keywords filter
  if (keywords && keywords.length > 0) {
    filteredCharts = filteredCharts.filter(chart =>
      chart.keywords && keywords.some(k => 
        chart.keywords!.some(ck => ck.toLowerCase().includes(k.toLowerCase()))
      )
    );
  }
  
  // Deprecated filter
  if (!deprecated) {
    filteredCharts = filteredCharts.filter(chart => !chart.deprecated);
  }
  
  // Sort charts
  filteredCharts.sort((a, b) => {
    let comparison = 0;
    
    switch (sortBy) {
      case 'name':
        comparison = a.name.localeCompare(b.name);
        break;
      case 'version':
        comparison = compareVersions(a.version, b.version);
        break;
      case 'created':
        comparison = new Date(a.created || 0).getTime() - new Date(b.created || 0).getTime();
        break;
      default:
        comparison = a.name.localeCompare(b.name);
    }
    
    return sortDirection === 'desc' ? -comparison : comparison;
  });
  
  // Apply pagination
  const paginatedCharts = filteredCharts.slice(offset, offset + limit);
  
  return {
    charts: paginatedCharts,
    total: filteredCharts.length,
    repositories: [...new Set(allCharts.map(c => c.repository.name))],
    categories: [...new Set(allCharts.map(c => c.category).filter(Boolean))] as string[]
  };
}

// === Version Resolution ===

/**
 * Resolve chart versions and dependencies
 */
export async function resolveChart(
  chartName: string,
  repository: ChartRepository,
  options: ChartResolveOptions = {}
): Promise<ChartResolveResult> {
  const {
    version,
    includeDeprecated = false,
    includePrereleases = false
  } = options;
  
  try {
    // Discover chart info
    const charts = await discoverChartsInRepository(repository);
    const chart = charts.find(c => c.name === chartName);
    
    if (!chart) {
      throw new Error(`Chart ${chartName} not found in repository ${repository.name}`);
    }
    
    // Get available versions
    const versions = await getChartVersions(chartName, repository, {
      includeDeprecated,
      includePrereleases
    });
    
    if (versions.length === 0) {
      throw new Error(`No versions available for chart ${chartName}`);
    }
    
    // Resolve target version
    let targetVersion: ChartVersion;
    if (version) {
      targetVersion = resolveVersionConstraint(versions, version);
    } else {
      targetVersion = versions[0]; // Latest version
    }
    
    // Resolve dependencies
    const dependencies = await resolveDependencies(
      targetVersion.dependencies || [],
      repository
    );
    
    // Get chart values
    const values = await getChartValues(chartName, targetVersion.version, repository);
    
    return {
      chart,
      versions,
      latestVersion: versions[0],
      recommendedVersion: findRecommendedVersion(versions),
      dependencies,
      values
    };
    
  } catch (error) {
    console.error(`Failed to resolve chart ${chartName}:`, error);
    throw error;
  }
}

/**
 * Get available versions for a chart
 */
export async function getChartVersions(
  chartName: string,
  repository: ChartRepository,
  options: {
    includeDeprecated?: boolean;
    includePrereleases?: boolean;
  } = {}
): Promise<ChartVersion[]> {
  const { includeDeprecated = false, includePrereleases = false } = options;
  
  // This would fetch versions from the repository
  // For now, return mock data
  const versions: ChartVersion[] = [];
  
  // Filter versions based on options
  let filteredVersions = versions;
  
  if (!includeDeprecated) {
    filteredVersions = filteredVersions.filter(v => !v.deprecated);
  }
  
  if (!includePrereleases) {
    filteredVersions = filteredVersions.filter(v => 
      !isPrereleaseVersion(v.version)
    );
  }
  
  // Sort versions in descending order (latest first)
  filteredVersions.sort((a, b) => compareVersions(b.version, a.version));
  
  return filteredVersions;
}

/**
 * Resolve version constraint to specific version
 */
export function resolveVersionConstraint(
  availableVersions: ChartVersion[],
  constraint: string
): ChartVersion {
  // Simple version constraint resolution
  // In a full implementation, this would support semver constraints
  
  if (constraint === 'latest' || constraint === '*') {
    return availableVersions[0];
  }
  
  // Exact version match
  const exactMatch = availableVersions.find(v => v.version === constraint);
  if (exactMatch) {
    return exactMatch;
  }
  
  // Range matching (simplified)
  if (constraint.startsWith('>=')) {
    const minVersion = constraint.slice(2);
    const candidates = availableVersions.filter(v => 
      compareVersions(v.version, minVersion) >= 0
    );
    return candidates[0] || availableVersions[0];
  }
  
  if (constraint.startsWith('^')) {
    const baseVersion = constraint.slice(1);
    const [major] = baseVersion.split('.');
    const candidates = availableVersions.filter(v => 
      v.version.startsWith(major + '.')
    );
    return candidates[0] || availableVersions[0];
  }
  
  if (constraint.startsWith('~')) {
    const baseVersion = constraint.slice(1);
    const [major, minor] = baseVersion.split('.');
    const candidates = availableVersions.filter(v => 
      v.version.startsWith(`${major}.${minor}.`)
    );
    return candidates[0] || availableVersions[0];
  }
  
  // Default to latest if constraint not understood
  return availableVersions[0];
}

// === Dependency Resolution ===

/**
 * Resolve chart dependencies
 */
export async function resolveDependencies(
  dependencies: ChartDependency[],
  parentRepository: ChartRepository
): Promise<ResolvedDependency[]> {
  const resolved: ResolvedDependency[] = [];
  
  for (const dep of dependencies) {
    try {
      // Skip disabled dependencies
      if (dep.enabled === false) {
        continue;
      }
      
      const depRepository = parentRepository;
      
      // If dependency specifies a different repository
      if (dep.repository) {
        // This would resolve the repository URL/name to actual repository
        // For now, use parent repository
      }
      
      const depChart = await resolveChart(dep.name, depRepository, {
        version: dep.version
      });
      
      resolved.push({
        name: dep.name,
        version: depChart.recommendedVersion.version,
        repository: depRepository.name,
        resolved: true,
        chart: depChart.chart
      });
      
    } catch (error) {
      resolved.push({
        name: dep.name,
        version: dep.version,
        repository: dep.repository || parentRepository.name,
        resolved: false,
        error: (error as Error)?.message || 'Failed to resolve dependency'
      });
    }
  }
  
  return resolved;
}

// === Chart Values Handling ===

/**
 * Get default values for a chart
 */
export async function getChartValues(
  chartName: string,
  version: string,
  repository: ChartRepository
): Promise<ChartValues> {
  try {
    // This would fetch the values.yaml file from the chart
    // For now, return mock data
    const raw = `# Default values for ${chartName}
replicaCount: 1

image:
  repository: nginx
  pullPolicy: IfNotPresent
  tag: ""

service:
  type: ClusterIP
  port: 80`;
    
    const parsed = parseYamlValues(raw);
    
    return {
      raw,
      parsed,
      schema: await generateValuesSchema(parsed)
    };
    
  } catch (error) {
    console.error(`Failed to get chart values for ${chartName}:`, error);
    return {
      raw: '',
      parsed: {},
      schema: {}
    };
  }
}

/**
 * Generate schema from values
 */
async function generateValuesSchema(values: Record<string, any>): Promise<ChartValuesSchema> {
  const schema: ChartValuesSchema = {};
  
  function inferType(value: any): ChartValueField {
    if (typeof value === 'string') {
      return { type: 'string', default: value };
    } else if (typeof value === 'number') {
      return { type: 'number', default: value };
    } else if (typeof value === 'boolean') {
      return { type: 'boolean', default: value };
    } else if (Array.isArray(value)) {
      return {
        type: 'array',
        default: value,
        items: value.length > 0 ? inferType(value[0]) : { type: 'string' }
      };
    } else if (typeof value === 'object' && value !== null) {
      const properties: ChartValuesSchema = {};
      for (const [key, val] of Object.entries(value)) {
        properties[key] = inferType(val);
      }
      return {
        type: 'object',
        default: value,
        properties
      };
    } else {
      return { type: 'string', default: '' };
    }
  }
  
  for (const [key, value] of Object.entries(values)) {
    schema[key] = inferType(value);
  }
  
  return schema;
}

// === Utility Functions ===

/**
 * Compare two semantic versions
 */
function compareVersions(a: string, b: string): number {
  const aParts = a.split('.').map(Number);
  const bParts = b.split('.').map(Number);
  
  const maxLength = Math.max(aParts.length, bParts.length);
  
  for (let i = 0; i < maxLength; i++) {
    const aPart = aParts[i] || 0;
    const bPart = bParts[i] || 0;
    
    if (aPart > bPart) return 1;
    if (aPart < bPart) return -1;
  }
  
  return 0;
}

/**
 * Check if version is a prerelease
 */
function isPrereleaseVersion(version: string): boolean {
  return /-(alpha|beta|rc|pre|dev)/.test(version);
}

/**
 * Find recommended version (latest stable)
 */
function findRecommendedVersion(versions: ChartVersion[]): ChartVersion {
  const stableVersions = versions.filter(v => 
    !v.deprecated && !isPrereleaseVersion(v.version)
  );
  
  return stableVersions[0] || versions[0];
}

/**
 * Parse YAML values (simplified)
 */
function parseYamlValues(yaml: string): Record<string, any> {
  // This would use a proper YAML parser
  // For now, return simple object
  try {
    // Simplified YAML parsing - in reality would use js-yaml or similar
    return {};
  } catch (error) {
    console.warn('Failed to parse YAML values:', error);
    return {};
  }
}

/**
 * Categorize chart based on keywords and metadata
 */
export function categorizeChart(chart: ChartInfo): AppCategory {
  const keywords = (chart.keywords || []).map(k => k.toLowerCase());
  const name = chart.name.toLowerCase();
  const description = (chart.description || '').toLowerCase();
  const searchText = [name, description, ...keywords].join(' ');
  
  // AI/ML category
  if (searchText.match(/\b(ai|ml|machine.*learning|tensorflow|pytorch|jupyter|notebook|model|prediction|training)\b/)) {
    return APP_CATEGORIES.AI_ML;
  }
  
  // Database category
  if (searchText.match(/\b(database|db|mysql|postgres|mongodb|redis|cassandra|elastic|search)\b/)) {
    return APP_CATEGORIES.DATABASE;
  }
  
  // Monitoring category
  if (searchText.match(/\b(monitor|metrics|prometheus|grafana|alert|observability|logs|tracing)\b/)) {
    return APP_CATEGORIES.MONITORING;
  }
  
  // Analytics category
  if (searchText.match(/\b(analytic|spark|hadoop|kafka|stream|data|etl|warehouse)\b/)) {
    return APP_CATEGORIES.ANALYTICS;
  }
  
  // Security category
  if (searchText.match(/\b(security|auth|vault|cert|ssl|tls|oauth|rbac|policy)\b/)) {
    return APP_CATEGORIES.SECURITY;
  }
  
  // Networking category
  if (searchText.match(/\b(network|ingress|load.*balancer|proxy|gateway|mesh|istio|linkerd)\b/)) {
    return APP_CATEGORIES.NETWORKING;
  }
  
  // Storage category
  if (searchText.match(/\b(storage|volume|pv|csi|backup|restore|sync)\b/)) {
    return APP_CATEGORIES.STORAGE;
  }
  
  // Web category
  if (searchText.match(/\b(web|http|nginx|apache|server|cms|wordpress|drupal)\b/)) {
    return APP_CATEGORIES.WEB;
  }
  
  // Tools category
  if (searchText.match(/\b(tool|cli|util|admin|manage|deploy|ci|cd|jenkins|gitlab|git)\b/)) {
    return APP_CATEGORIES.TOOLS;
  }
  
  return APP_CATEGORIES.OTHER;
}

/**
 * Get built-in repository configurations
 */
export function getBuiltInRepositories(): ChartRepository[] {
  return Object.values(BUILT_IN_REPOSITORIES).map(repo => ({
    name: repo.name,
    url: repo.url,
    type: repo.type,
    credentials: undefined // Built-in repos typically don't need auth
  }));
}