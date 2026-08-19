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
 * Check if cluster supports specific API version
 */
export function supportsApiVersion(
  capabilities: ClusterCapabilities,
  apiVersion: string
): boolean {
  return capabilities.supportedApiVersions.includes(apiVersion);
}

// === Cluster Discovery and Connection ===


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


// === Bulk Operations ===



/**
 * Get cluster display name
 */
export function getClusterDisplayName(cluster: ClusterInfo): string {
  return cluster.displayName || cluster.name || cluster.id;
}