/**
 * Cluster Operations Utilities
 * Provides cluster-related helper functions and operations
 * Following standard patterns for cluster management
 */

import { CONNECTION_STATUS, TIMEOUT_VALUES, RETRY_CONFIG } from './constants';
import type { ConnectionStatus } from './constants';
import { retryWithBackoff } from './promise';
import { getClusters } from '../services/rancher-apps';
import logger from './logger';

// === Cluster Information Types ===
export interface ClusterInfo {
  id: string;
  name: string;
  displayName?: string;
  description?: string;
  ready: boolean;
  connectionStatus: ConnectionStatus;
  version: {
    kubernetes: string;
    rancher: string;
    distribution?: string;
  };
  provider?: string;
  region?: string;
  nodeCount?: number;
  lastHealthCheck?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface ClusterCapabilities {
  canInstallApps: boolean;
  canManageNamespaces: boolean;
  canAccessSecrets: boolean;
  canCreateServiceAccounts: boolean;
  hasHelmSupport: boolean;
  hasRancherAppsSupport: boolean;
  supportedApiVersions: string[];
  maxHelmVersion?: string;
  installedOperators: string[];
  featureGates: string[];
  storageClasses: string[];
  ingressClasses: string[];
}

export interface ClusterStats {
  totalApps: number;
  runningApps: number;
  failedApps: number;
  namespacesWithApps: number;
  lastAppActivity?: string;
  resourceUsage?: {
    cpu: { used: number; total: number; percentage: number };
    memory: { used: number; total: number; percentage: number };
    storage: { used: number; total: number; percentage: number };
  };
}

export interface ClusterHealth {
  overall: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  components: ClusterComponent[];
  issues: ClusterIssue[];
  lastCheck: string;
  nextCheck?: string;
}

export interface ClusterComponent {
  name: string;
  status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  message?: string;
  lastCheck: string;
}

export interface ClusterIssue {
  severity: 'critical' | 'warning' | 'info';
  message: string;
  component?: string;
  timestamp: string;
  resolved?: boolean;
  resolutionSteps?: string[];
}

// === Cluster Selection and Filtering ===
export interface ClusterFilter {
  ready?: boolean;
  connectionStatus?: ConnectionStatus[];
  hasApps?: boolean;
  provider?: string[];
  version?: {
    kubernetes?: { min?: string; max?: string };
    rancher?: { min?: string; max?: string };
  };
  capabilities?: string[];
  labels?: Record<string, string>;
  searchText?: string;
}

export interface ClusterSort {
  field: 'name' | 'ready' | 'apps' | 'lastActivity' | 'version';
  direction: 'asc' | 'desc';
}

// === Cluster Operation Results ===
export interface ClusterOperationResult {
  success: boolean;
  clusterId: string;
  operation: string;
  message?: string;
  error?: string;
  duration?: number;
  timestamp: string;
}

export interface BulkClusterOperationResult {
  totalClusters: number;
  successfulClusters: number;
  failedClusters: number;
  results: ClusterOperationResult[];
  summary: string;
}

// === Cluster Validation Functions ===

/**
 * Check if cluster is ready for app operations
 */
export function isClusterReady(cluster: ClusterInfo): boolean {
  return cluster.ready && 
         cluster.connectionStatus === CONNECTION_STATUS.CONNECTED &&
         cluster.version.kubernetes !== 'Unknown';
}

/**
 * Check if cluster supports specific capability
 */
export function hasClusterCapability(
  capabilities: ClusterCapabilities, 
  capability: keyof ClusterCapabilities
): boolean {
  return Boolean(capabilities[capability]);
}

/**
 * Check if cluster supports specific API version
 */
export function supportsApiVersion(
  capabilities: ClusterCapabilities,
  apiVersion: string
): boolean {
  return capabilities.supportedApiVersions.includes(apiVersion);
}

/**
 * Check if cluster has required operator installed
 */
export function hasOperatorInstalled(
  capabilities: ClusterCapabilities,
  operatorName: string
): boolean {
  return capabilities.installedOperators.includes(operatorName);
}

/**
 * Validate cluster for app installation
 */
export function validateClusterForAppInstall(
  cluster: ClusterInfo,
  capabilities: ClusterCapabilities
): { valid: boolean; reasons: string[] } {
  const reasons: string[] = [];
  
  if (!isClusterReady(cluster)) {
    reasons.push('Cluster is not ready or connected');
  }
  
  if (!capabilities.canInstallApps) {
    reasons.push('Cluster does not support app installation');
  }
  
  if (!capabilities.hasHelmSupport) {
    reasons.push('Cluster does not have Helm support');
  }
  
  if (!capabilities.canManageNamespaces) {
    reasons.push('Cannot manage namespaces on this cluster');
  }
  
  return {
    valid: reasons.length === 0,
    reasons
  };
}

// === Cluster Discovery and Connection ===

/**
 * Discover available clusters
 */
export async function discoverClusters(store: any): Promise<ClusterInfo[]> {
  try {
    // Get clusters from Rancher management store
    const clusters: ClusterInfo[] = [];
    
    // TODO: Replace with actual store integration for cluster discovery
    
    return clusters;
  } catch (error) {
    console.error('Failed to discover clusters:', error);
    throw error;
  }
}

/**
 * Test cluster connection
 */
export async function testClusterConnection(
  clusterId: string,
  store: any
): Promise<ClusterOperationResult> {
  const startTime = Date.now();
  
  try {
    // Test basic connectivity
    await retryWithBackoff(
      async () => {
        // TODO: Implement actual cluster connection test
      },
      RETRY_CONFIG.MAX_ATTEMPTS,
      RETRY_CONFIG.BASE_DELAY
    );
    
    return {
      success: true,
      clusterId,
      operation: 'connection-test',
      message: 'Cluster connection successful',
      duration: Date.now() - startTime,
      timestamp: new Date().toISOString()
    };
  } catch (error: any) {
    return {
      success: false,
      clusterId,
      operation: 'connection-test',
      error: error.message || 'Connection test failed',
      duration: Date.now() - startTime,
      timestamp: new Date().toISOString()
    };
  }
}

/**
 * Get cluster capabilities
 */
export async function getClusterCapabilities(
  clusterId: string,
  store: any
): Promise<ClusterCapabilities> {
  try {
    const capabilities: ClusterCapabilities = {
      canInstallApps: true,
      canManageNamespaces: true,
      canAccessSecrets: false,
      canCreateServiceAccounts: false,
      hasHelmSupport: false,
      hasRancherAppsSupport: true,
      supportedApiVersions: ['v1'],
      installedOperators: [],
      featureGates: [],
      storageClasses: [],
      ingressClasses: []
    };
    
    // Check for Helm support by looking for CRDs
    try {
      // const helmCRDs = await store.dispatch('cluster/findAll', {
      //   type: 'apiextensions.k8s.io.customresourcedefinition',
      //   opt: {
      //     filter: {
      //       'metadata.name': 'releases.helm.sh'
      //     }
      //   }
      // });
      // capabilities.hasHelmSupport = helmCRDs.length > 0;
      capabilities.hasHelmSupport = true; // Placeholder
    } catch (error) {
      // Helm CRDs not available
    }
    
    // Check permissions
    try {
      // const canListSecrets = await checkPermission(clusterId, 'secrets', 'list');
      // capabilities.canAccessSecrets = canListSecrets;
      capabilities.canAccessSecrets = true; // Placeholder
    } catch (error) {
      // Permission check failed
    }
    
    return capabilities;
  } catch (error) {
    console.error(`Failed to get cluster capabilities for ${clusterId}:`, error);
    throw error;
  }
}

export async function getClusterContext(
  store: any,
  opts?: { repoName?: string }
) {
  let cluster: any = null;
  let clusterId = 'local';
  let isLocalCluster = true;
  let baseApi = '/v1';
  const repoName = opts?.repoName;

  try {
    const clusters = await getClusters(store);

    if (!clusters.length) {
      logger.warn('[SUSE-AI] No clusters found — defaulting to local.');
      return { cluster: null, clusterId, isLocalCluster, baseApi, repo: null };
    }

    if (repoName) {
      for (const c of clusters) {
        const cid = c.id;
        const api = cid === 'local'
          ? '/v1'
          : `/k8s/clusters/${encodeURIComponent(cid)}/v1`;

        try {
          const repo = await store.dispatch('rancher/request', {
            url: `${api}/catalog.cattle.io.clusterrepos/${encodeURIComponent(repoName)}`
          });

          if (repo) {
            logger.info('Found repo', {
              component: 'getClusterContext',
              data: { repoName }
            });

            return {
              cluster: c,
              clusterId: cid,
              isLocalCluster: cid === 'local',
              baseApi: api,
              repo
            };
          }
        } catch (err) {
          logger.debug(`[SUSE-AI] Failed to fetch repo "${repoName}" in cluster ${cid}`);
        }
      }

      logger.warn(`Repo "${repoName}" not found in any accessible cluster`);
      return { cluster: null, clusterId: null, isLocalCluster: null, baseApi: null, repo: null };
    }

    cluster = clusters.find((c: any) => c.id === 'local') || clusters[0];
    clusterId = cluster.id;
    isLocalCluster = cluster.id === 'local';
    baseApi = isLocalCluster
      ? '/v1'
      : `/k8s/clusters/${encodeURIComponent(clusterId)}/v1`;

    logger.debug(`[SUSE-AI] Selected cluster: ${cluster.id} (${cluster.spec?.displayName || 'no name'})`);

    return { cluster, clusterId, isLocalCluster, baseApi, repo: null };

  } catch (error) {
    logger.error('Failed to enumerate clusters', error, {
      component: 'getClusterContext'
    });
    return { cluster: null, clusterId: null, isLocalCluster: null, baseApi: null, repo: null };
  }
}


// === Cluster Health Monitoring ===

/**
 * Check cluster health
 */
export async function checkClusterHealth(
  clusterId: string,
  store: any
): Promise<ClusterHealth> {
  const components: ClusterComponent[] = [];
  const issues: ClusterIssue[] = [];
  
  try {
    // Check core components
    const coreComponents = [
      { name: 'api-server', endpoint: '/api/v1' },
      { name: 'etcd', endpoint: '/api/v1/componentstatuses/etcd-0' },
      { name: 'controller-manager', endpoint: '/api/v1/componentstatuses/controller-manager' },
      { name: 'scheduler', endpoint: '/api/v1/componentstatuses/scheduler' }
    ];
    
    for (const component of coreComponents) {
      try {
        // This would check actual component health
        // const status = await checkComponentHealth(clusterId, component.endpoint);
        components.push({
          name: component.name,
          status: 'healthy', // Placeholder
          lastCheck: new Date().toISOString()
        });
      } catch (error) {
        components.push({
          name: component.name,
          status: 'unhealthy',
          message: 'Component check failed',
          lastCheck: new Date().toISOString()
        });
        
        issues.push({
          severity: 'critical',
          message: `${component.name} is unhealthy`,
          component: component.name,
          timestamp: new Date().toISOString()
        });
      }
    }
    
    // Calculate overall health
    const healthyComponents = components.filter(c => c.status === 'healthy').length;
    const totalComponents = components.length;
    
    let overall: ClusterHealth['overall'];
    if (healthyComponents === totalComponents) {
      overall = 'healthy';
    } else if (healthyComponents >= totalComponents * 0.5) {
      overall = 'degraded';
    } else {
      overall = 'unhealthy';
    }
    
    return {
      overall,
      components,
      issues,
      lastCheck: new Date().toISOString()
    };
    
  } catch (error) {
    return {
      overall: 'unknown',
      components,
      issues: [{
        severity: 'critical',
        message: `Health check failed: ${(error as Error)?.message || 'Unknown error'}`,
        timestamp: new Date().toISOString()
      }],
      lastCheck: new Date().toISOString()
    };
  }
}

// === Cluster Filtering and Sorting ===

/**
 * Filter clusters based on criteria
 */
export function filterClusters(
  clusters: ClusterInfo[],
  filter: ClusterFilter
): ClusterInfo[] {
  return clusters.filter(cluster => {
    // Ready status filter
    if (filter.ready !== undefined && cluster.ready !== filter.ready) {
      return false;
    }
    
    // Connection status filter
    if (filter.connectionStatus && !filter.connectionStatus.includes(cluster.connectionStatus)) {
      return false;
    }
    
    // Provider filter
    if (filter.provider && filter.provider.length > 0) {
      if (!cluster.provider || !filter.provider.includes(cluster.provider)) {
        return false;
      }
    }
    
    // Search text filter
    if (filter.searchText) {
      const searchLower = filter.searchText.toLowerCase();
      const searchableText = [
        cluster.name,
        cluster.displayName,
        cluster.description,
        cluster.id
      ].filter(Boolean).join(' ').toLowerCase();
      
      if (!searchableText.includes(searchLower)) {
        return false;
      }
    }
    
    // Label filters
    if (filter.labels) {
      for (const [key, value] of Object.entries(filter.labels)) {
        if (!cluster.labels || cluster.labels[key] !== value) {
          return false;
        }
      }
    }
    
    return true;
  });
}

/**
 * Sort clusters based on criteria
 */
export function sortClusters(
  clusters: ClusterInfo[],
  sort: ClusterSort
): ClusterInfo[] {
  return [...clusters].sort((a, b) => {
    let comparison = 0;
    
    switch (sort.field) {
      case 'name':
        comparison = a.name.localeCompare(b.name);
        break;
      case 'ready':
        comparison = (b.ready ? 1 : 0) - (a.ready ? 1 : 0);
        break;
      case 'lastActivity': {
        const aTime = new Date(a.lastHealthCheck || 0).getTime();
        const bTime = new Date(b.lastHealthCheck || 0).getTime();
        comparison = bTime - aTime;
        break;
      }
      case 'version':
        comparison = a.version.kubernetes.localeCompare(b.version.kubernetes);
        break;
      default:
        comparison = a.name.localeCompare(b.name);
    }
    
    return sort.direction === 'desc' ? -comparison : comparison;
  });
}

// === Bulk Operations ===

/**
 * Perform operation on multiple clusters
 */
export async function performBulkClusterOperation(
  clusterIds: string[],
  operation: 'health-check' | 'connection-test' | 'capabilities-refresh',
  store: any,
  options: { parallel?: boolean; batchSize?: number } = {}
): Promise<BulkClusterOperationResult> {
  const { parallel = true, batchSize = 5 } = options;
  const results: ClusterOperationResult[] = [];
  
  if (parallel) {
    // Process in batches to avoid overwhelming the system
    for (let i = 0; i < clusterIds.length; i += batchSize) {
      const batch = clusterIds.slice(i, i + batchSize);
      const batchPromises = batch.map(async clusterId => {
        try {
          switch (operation) {
            case 'connection-test':
              return await testClusterConnection(clusterId, store);
            case 'health-check': {
              const health = await checkClusterHealth(clusterId, store);
              return {
                success: health.overall !== 'unhealthy',
                clusterId,
                operation: 'health-check',
                message: `Health status: ${health.overall}`,
                timestamp: new Date().toISOString()
              };
            }
            case 'capabilities-refresh':
              await getClusterCapabilities(clusterId, store);
              return {
                success: true,
                clusterId,
                operation: 'capabilities-refresh',
                message: 'Capabilities refreshed',
                timestamp: new Date().toISOString()
              };
            default:
              throw new Error(`Unknown operation: ${operation}`);
          }
        } catch (error: any) {
          return {
            success: false,
            clusterId,
            operation,
            error: error.message || 'Operation failed',
            timestamp: new Date().toISOString()
          };
        }
      });
      
      const batchResults = await Promise.allSettled(batchPromises);
      results.push(...batchResults.map(result => 
        result.status === 'fulfilled' ? result.value : {
          success: false,
          clusterId: 'unknown',
          operation,
          error: 'Promise rejected',
          timestamp: new Date().toISOString()
        }
      ));
    }
  } else {
    // Process sequentially
    for (const clusterId of clusterIds) {
      try {
        const result = await testClusterConnection(clusterId, store);
        results.push(result);
      } catch (error: any) {
        results.push({
          success: false,
          clusterId,
          operation,
          error: error.message || 'Operation failed',
          timestamp: new Date().toISOString()
        });
      }
    }
  }
  
  const successfulClusters = results.filter(r => r.success).length;
  const failedClusters = results.length - successfulClusters;
  
  return {
    totalClusters: clusterIds.length,
    successfulClusters,
    failedClusters,
    results,
    summary: `${successfulClusters}/${clusterIds.length} clusters successful`
  };
}

// === Utility Functions ===

/**
 * Get cluster display name
 */
export function getClusterDisplayName(cluster: ClusterInfo): string {
  return cluster.displayName || cluster.name || cluster.id;
}

/**
 * Get cluster status summary
 */
export function getClusterStatusSummary(clusters: ClusterInfo[]): {
  total: number;
  ready: number;
  connected: number;
  hasIssues: number;
} {
  return {
    total: clusters.length,
    ready: clusters.filter(c => c.ready).length,
    connected: clusters.filter(c => c.connectionStatus === CONNECTION_STATUS.CONNECTED).length,
    hasIssues: clusters.filter(c => 
      !c.ready || c.connectionStatus === CONNECTION_STATUS.ERROR
    ).length
  };
}

/**
 * Format cluster version for display
 */
export function formatClusterVersion(cluster: ClusterInfo): string {
  const { kubernetes, rancher, distribution } = cluster.version;
  let version = `Kubernetes ${kubernetes}`;
  
  if (distribution && distribution !== 'Unknown') {
    version += ` (${distribution})`;
  }
  
  if (rancher && rancher !== 'Unknown') {
    version += `, Rancher ${rancher}`;
  }
  
  return version;
}

/**
 * Check if cluster needs attention
 */
export function clusterNeedsAttention(
  cluster: ClusterInfo,
  health?: ClusterHealth
): boolean {
  // Not ready or not connected
  if (!cluster.ready || cluster.connectionStatus !== CONNECTION_STATUS.CONNECTED) {
    return true;
  }
  
  // Health issues
  if (health && (health.overall === 'unhealthy' || health.overall === 'degraded')) {
    return true;
  }
  
  // No recent health check
  if (cluster.lastHealthCheck) {
    const lastCheck = new Date(cluster.lastHealthCheck);
    const now = new Date();
    const hoursSinceLastCheck = (now.getTime() - lastCheck.getTime()) / (1000 * 60 * 60);
    
    if (hoursSinceLastCheck > 24) {
      return true;
    }
  }
  
  return false;
}