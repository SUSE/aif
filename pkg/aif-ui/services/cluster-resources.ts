// Cluster resource metrics and compatibility checking service
import type { RancherStore, ClusterResource, ClusterInfo, NodeResource, NodeMetric } from '../types/rancher-types';
import { createErrorHandler, handleSimpleError } from '../utils/error-handler';

export interface ResourceRequirements {
  cpu: number;        // CPU cores
  memory: number;     // Memory in GB
  gpu?: number;       // GPU memory in GB
  storage: number;    // Storage in GB
}

export interface NodeResourceInfo {
  nodeId: string;
  nodeName: string;
  cpu: { used: number; total: number };
  memory: { used: number; total: number };  // in GB
  gpu?: { used: number; total: number; type?: string };  // GPU memory in GB
}

export interface ClusterResourceSummary {
  clusterId: string;
  name: string;
  nodeCount: number;
  resources: {
    cpu: { used: number; total: number };
    memory: { used: number; total: number };  // in GB
    gpu?: { used: number; total: number; type?: string };  // GPU memory in GB
  };
  status: 'compatible' | 'limited' | 'insufficient' | 'checking' | 'error';
  statusMessage?: string;
  storageClasses: string[];
  lastUpdated: Date;
  nodes: NodeResourceInfo[];
}

export interface AppResourceProfile {
  slug: string;
  name: string;
  requirements: ResourceRequirements;
}

// Static app resource requirements - will be expanded as we add more apps
const APP_RESOURCE_PROFILES: Record<string, AppResourceProfile> = {
  'ollama': {
    slug: 'ollama',
    name: 'Ollama',
    requirements: { cpu: 4, memory: 16, gpu: 8, storage: 100 }
  },
  'pytorch': {
    slug: 'pytorch',
    name: 'PyTorch',
    requirements: { cpu: 4, memory: 16, gpu: 8, storage: 100 }
  },
  'open-webui': {
    slug: 'open-webui',
    name: 'Open WebUI',
    requirements: { cpu: 2, memory: 4, storage: 10 }
  },
  'open-webui-mcpo': {
    slug: 'open-webui-mcpo',
    name: 'Open WebUI mcpo',
    requirements: { cpu: 2, memory: 4, storage: 2 }
  },
  'open-webui-pipelines': {
    slug: 'open-webui-pipelines',
    name: 'Open WebUI Pipelines',
    requirements: { cpu: 2, memory: 4, storage: 2 }
  },
  'milvus': {
    slug: 'milvus',
    name: 'Milvus',
    requirements: { cpu: 4, memory: 8, storage: 100 }
  },
  'suse-ai-deployer': {
    slug: 'suse-ai-deployer',
    name: 'SUSE AI Deployer',
    requirements: { cpu: 4, memory: 16, gpu: 12, storage: 100 }
  },
  'suse-ai-observability-extension': {
    slug: 'suse-ai-observability-extension',
    name: 'SUSE AI Observability Extension',
    requirements: { cpu: 1, memory: 2, storage: 2 }
  },
  'vllm': {
    slug: 'vllm',
    name: 'vLLM',
    requirements: { cpu: 4, memory: 16, gpu: 12, storage: 100 }
  },
  'qdrant': {
    slug: 'qdrant',
    name: 'Qdrant',
    requirements: { cpu: 2, memory: 4, storage: 10 }
  },
  'litellm': {
    slug: 'litellm',
    name: 'LiteLLM',
    requirements: { cpu: 4, memory: 8, storage: 10 }
  }
};

export function getAppResourceRequirements(slug: string): AppResourceProfile | null {
  return APP_RESOURCE_PROFILES[slug] || null;
}

// Fallback resource requirements for unknown apps
export function getDefaultAppResourceRequirements(slug: string, appName?: string): AppResourceProfile {
  return {
    slug,
    name: appName || slug,
    requirements: {
      cpu: 1,      // Conservative default: 1 CPU core
      memory: 2,   // Conservative default: 2GB RAM
      storage: 10  // Conservative default: 10GB storage
      // No GPU requirement by default
    }
  };
}

export async function getClusterResourceMetrics(store: RancherStore, clusterId: string): Promise<ClusterResourceSummary> {
  console.log(`[SUSE-AI] getClusterResourceMetrics: Starting for cluster ${clusterId}`);
  
  try {
    // Get cluster basic info first using the same approach as getClusters
    let clusterName = clusterId;
    try {
      const clusters = await Promise.race([
        store.dispatch('management/findAll', { type: 'cluster' }),
        new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), 20000))
      ]) as ClusterResource[];
      const cluster = clusters.find((c: ClusterResource) => c.id === clusterId);
      clusterName = cluster?.metadata?.name || clusterId;
    } catch {
      // Fallback to API call if store doesn't work
      const res = await store.dispatch('rancher/request', { url: '/v1/management.cattle.io.clusters?limit=2000', timeout: 20000 });
      const items = res?.data?.data || res?.data || [];
      const cluster = items.find((c: ClusterResource) =>
        (c?.metadata?.name === clusterId) || (c?.id === clusterId) || (c?.spec?.displayName === clusterId)
      );
      clusterName = cluster?.spec?.displayName || cluster?.metadata?.name || cluster?.name || clusterId;
    }

    console.log(`[SUSE-AI] getClusterResourceMetrics: Found cluster ${clusterName}`);

    // Get node information using simplified API pattern
    let nodes: NodeResource[] = [];
    let nodeMetrics: NodeMetric[] = [];
    let storageClasses: string[] = [];
    
    nodes = await fetchNodes(store, clusterId);

    // Get node metrics using simplified API pattern
    nodeMetrics = await fetchNodeMetrics(store, clusterId);


    // Get storage classes using the same API that works in Rancher

    storageClasses = await fetchStorageClasses(store, clusterId);

    // Process nodes and calculate resources
    const nodeInfos: NodeResourceInfo[] = [];
    let totalCpu = 0, usedCpu = 0, totalMemory = 0, usedMemory = 0;
    let totalGpu = 0;
    const usedGpu = 0;
    let gpuType = '';
    
    // If no nodes are available, try to provide reasonable defaults or mark as unavailable
    if (nodes.length === 0) {
      console.log(`[SUSE-AI] getClusterResourceMetrics: No nodes found for ${clusterId}, using fallback approach`);
      
      // For imported/managed clusters that we can't access directly, provide a status that indicates unknown
      const summary: ClusterResourceSummary = {
        clusterId,
        name: clusterName,
        nodeCount: 0,
        resources: {
          cpu: { used: 0, total: 0 },
          memory: { used: 0, total: 0 }
        },
        status: 'error',
        statusMessage: 'Unable to access cluster node information',
        storageClasses,
        lastUpdated: new Date(),
        nodes: []
      };

      console.log(`[SUSE-AI] getClusterResourceMetrics: Returning fallback summary for ${clusterId}`);
      return summary;
    }
    
    for (const node of nodes) {
      // Handle different data formats: global API vs cluster-specific API
      const nodeName = node.metadata?.name || (node as any).id || (node as unknown as { name?: string }).name || '';
      const nodeMetric = nodeMetrics.find((m: NodeMetric) =>
        (m.metadata?.name === nodeName) || (m as unknown as { name?: string }).name === nodeName
      );
      
      // Kubernetes v1.Node provides status.allocatable and status.capacity
      const allocatable = node.status?.allocatable ?? {};
      const capacity = node.status?.capacity ?? {};

      // Prefer allocatable (accounts for system reservations), fallback to capacity if allocatable is empty
      const resourceData = Object.keys(allocatable).length > 0 ? allocatable : capacity;
      
      const nodeTotalCpu = parseFloat(resourceData.cpu || '0');
      const nodeTotalMemory = parseK8sMemory(resourceData.memory || '0Ki');
      
      // Parse usage from metrics - handle different formats
      const usage = nodeMetric?.usage || nodeMetric?.metrics || {};
      const nodeUsedCpu = parseFloat(usage.cpu?.replace?.('n', '') || '0') / 1000000000; // nanocores to cores
      const nodeUsedMemory = parseK8sMemory(usage.memory || '0Ki');
      
      // Check for GPU resources (NVIDIA GPU Operator) - handle different formats
      let nodeGpu: { used: number; total: number; type?: string } = { used: 0, total: 0 };
      const gpuCapacity = resourceData['nvidia.com/gpu'] || capacity['nvidia.com/gpu'];
      if (gpuCapacity) {
        const gpuCount = parseInt(gpuCapacity);
        // For GPU memory, we'll need to query GPU metrics or make assumptions
        // For now, assume common GPU memory sizes based on GPU count
        const estimatedGpuMemory = estimateGpuMemory(node, gpuCount);
        nodeGpu = { used: 0, total: estimatedGpuMemory, type: 'NVIDIA' };
        totalGpu += estimatedGpuMemory;
        gpuType = 'NVIDIA';
      }
      
      nodeInfos.push({
        nodeId: nodeName,
        nodeName: nodeName,
        cpu: { used: nodeUsedCpu, total: nodeTotalCpu },
        memory: { used: nodeUsedMemory, total: nodeTotalMemory },
        gpu: nodeGpu.total > 0 ? nodeGpu : undefined
      });
      
      totalCpu += nodeTotalCpu;
      usedCpu += nodeUsedCpu;
      totalMemory += nodeTotalMemory;
      usedMemory += nodeUsedMemory;
    }

    const summary: ClusterResourceSummary = {
      clusterId,
      name: clusterName,
      nodeCount: nodes.length,
      resources: {
        cpu: { used: Math.round(usedCpu * 10) / 10, total: Math.round(totalCpu * 10) / 10 },
        memory: { used: Math.round(usedMemory), total: Math.round(totalMemory) },
        gpu: totalGpu > 0 ? { used: usedGpu, total: totalGpu, type: gpuType } : undefined
      },
      status: 'compatible', // Will be updated by checkCompatibility
      storageClasses,
      lastUpdated: new Date(),
      nodes: nodeInfos
    };

    console.log(`[SUSE-AI] getClusterResourceMetrics: Completed for ${clusterId}:`, {
      cpu: summary.resources.cpu,
      memory: summary.resources.memory,
      gpu: summary.resources.gpu,
      nodes: summary.nodeCount
    });

    return summary;
  } catch (error: unknown) {
    const err = error as { message?: string };
    console.error(`[SUSE-AI] getClusterResourceMetrics: Failed for cluster ${clusterId}:`, error);
    
    // Try to get basic cluster info even if metrics fail
    let clusterName = clusterId;
    try {
      const { getClusters } = await import('./rancher-apps');
      const clusters = await getClusters(store);
      const cluster = clusters.find((c: ClusterInfo) => c.id === clusterId);
      clusterName = cluster?.name || clusterId;
      
      return {
        clusterId,
        name: clusterName,
        nodeCount: 0,
        resources: { cpu: { used: 0, total: 0 }, memory: { used: 0, total: 0 } },
        status: 'error',
        statusMessage: err.message || 'Failed to retrieve cluster metrics',
        storageClasses: [],
        lastUpdated: new Date(),
        nodes: []
      };
    } catch {
      return {
        clusterId,
        name: clusterId,
        nodeCount: 0,
        resources: { cpu: { used: 0, total: 0 }, memory: { used: 0, total: 0 } },
        status: 'error',
        statusMessage: 'Failed to retrieve cluster information',
        storageClasses: [],
        lastUpdated: new Date(),
        nodes: []
      };
    }
  }
}

export async function getAllClusterResourceMetrics(store: RancherStore): Promise<ClusterResourceSummary[]> {
  console.log('[SUSE-AI] getAllClusterResourceMetrics: Starting...');
  
  try {
    // Import and use the existing getClusters function
    const { getClusters } = await import('./rancher-apps');
    const clusters = await getClusters(store);
    console.log(`[SUSE-AI] getAllClusterResourceMetrics: Found ${clusters.length} clusters`);
    
    // Get metrics for all clusters in parallel
    const results = await Promise.all(
      clusters.map((cluster: ClusterInfo) => getClusterResourceMetrics(store, cluster.id))
    );
    
    console.log(`[SUSE-AI] getAllClusterResourceMetrics: Completed for ${results.length} clusters`);
    return results;
  } catch (error) {
    console.error('[SUSE-AI] getAllClusterResourceMetrics: Failed:', error);
    return [];
  }
}

export function checkAppCompatibility(
  appSlug: string, 
  clusterSummary: ClusterResourceSummary,
  appName?: string
): ClusterResourceSummary {
  let appProfile = getAppResourceRequirements(appSlug);
  let usingDefaults = false;
  
  if (!appProfile) {
    // Use conservative defaults for unknown apps
    appProfile = getDefaultAppResourceRequirements(appSlug, appName);
    usingDefaults = true;
    console.log(`[SUSE-AI] checkAppCompatibility: Using default requirements for unknown app ${appSlug}:`, appProfile.requirements);
  }

  // If cluster already has an error status (like can't access node info), preserve it
  if (clusterSummary.status === 'error') {
    return {
      ...clusterSummary,
      statusMessage: clusterSummary.statusMessage || 'Unable to determine compatibility - cluster information unavailable'
    };
  }

  const req = appProfile.requirements;
  const available = clusterSummary.resources;
  
  // If we don't have resource information (all zeros), we can't determine compatibility
  if (available.cpu.total === 0 && available.memory.total === 0) {
    return {
      ...clusterSummary,
      status: 'error',
      statusMessage: 'Resource information unavailable - cannot determine compatibility'
    };
  }
  
  // Check each resource requirement
  const cpuOk = (available.cpu.total - available.cpu.used) >= req.cpu;
  const memoryOk = (available.memory.total - available.memory.used) >= req.memory;
  const storageOk = req.storage <= 0 || clusterSummary.storageClasses.length > 0;
  
  let gpuOk = true;
  if (req.gpu && req.gpu > 0) {
    if (!available.gpu) {
      gpuOk = false;
    } else {
      gpuOk = (available.gpu.total - available.gpu.used) >= req.gpu;
    }
  }
  
  // Determine status and message
  let status: ClusterResourceSummary['status'] = 'compatible';
  let statusMessage = '';
  
  const issues: string[] = [];
  if (!cpuOk) issues.push(`need ${req.cpu} CPU (${available.cpu.total - available.cpu.used} available)`);
  if (!memoryOk) issues.push(`need ${req.memory}GB RAM (${available.memory.total - available.memory.used}GB available)`);
  if (!gpuOk && req.gpu) issues.push(`need ${req.gpu}GB GPU (${available.gpu?.total || 0}GB available)`);
  if (!storageOk) issues.push('no storage classes available');
  
  if (issues.length > 0) {
    status = 'insufficient';
    statusMessage = issues.join(', ');
    if (usingDefaults) {
      statusMessage += ' (using estimated requirements)';
    }
  } else {
    // Check for tight resources (warn if less than 20% headroom)
    const warnings: string[] = [];
    if (available.cpu.total > 0) {
      const cpuHeadroom = ((available.cpu.total - available.cpu.used - req.cpu) / available.cpu.total) * 100;
      if (cpuHeadroom < 20) warnings.push('low CPU headroom');
    }
    if (available.memory.total > 0) {
      const memoryHeadroom = ((available.memory.total - available.memory.used - req.memory) / available.memory.total) * 100;
      if (memoryHeadroom < 20) warnings.push('low memory headroom');
    }
    
    if (usingDefaults) {
      warnings.push('using estimated requirements');
    }
    
    if (warnings.length > 0) {
      status = usingDefaults && warnings.length === 1 ? 'compatible' : 'limited';
      statusMessage = warnings.join(', ');
    }
  }
  
  return {
    ...clusterSummary,
    status,
    statusMessage
  };
}

// Helper functions

function parseK8sMemory(memoryStr: string): number {
  // Parse Kubernetes memory strings like "4Gi", "1024Mi", "1073741824" (bytes)
  const str = memoryStr.trim();
  
  if (str.endsWith('Gi')) {
    return parseFloat(str.slice(0, -2));
  } else if (str.endsWith('Mi')) {
    return parseFloat(str.slice(0, -2)) / 1024;
  } else if (str.endsWith('Ki')) {
    return parseFloat(str.slice(0, -2)) / (1024 * 1024);
  } else if (str.endsWith('G')) {
    return parseFloat(str.slice(0, -1));
  } else if (str.endsWith('M')) {
    return parseFloat(str.slice(0, -1)) / 1024;
  } else if (str.endsWith('K')) {
    return parseFloat(str.slice(0, -1)) / (1024 * 1024);
  } else {
    // Assume bytes
    return parseFloat(str) / (1024 * 1024 * 1024);
  }
}

async function fetchNodes(store: RancherStore, clusterId: string): Promise<NodeResource[]> {
  return fetchClusterData<NodeResource>(store, clusterId, 'nodes', 'nodes');
}

async function fetchNodeMetrics(store: RancherStore, clusterId: string): Promise<NodeMetric[]> {
  return fetchClusterData<NodeMetric>(store, clusterId, 'metrics.k8s.io.nodes', 'node metrics');
}

async function fetchClusterData<T>(
  store: RancherStore,
  clusterId: string,
  resourcePath: string,
  label: string
): Promise<T[]> {
  const isLocalCluster = clusterId === 'local';
  const baseUrl = isLocalCluster
    ? `/v1/${resourcePath}?exclude=metadata.managedFields`
    : `/k8s/clusters/${encodeURIComponent(clusterId)}/v1/${resourcePath}?exclude=metadata.managedFields`;

  try {
    const res = await store.dispatch('rancher/request', { url: baseUrl, timeout: 20000 });
    const data = res?.data?.data || res?.data || [];
    console.log(`[SUSE-AI] getClusterResourceMetrics: Got ${data.length} ${label} from ${isLocalCluster ? 'global' : 'cluster-specific'} API`);
    return Array.isArray(data) ? data : [];
  } catch (error) {
    console.warn(`[SUSE-AI] getClusterResourceMetrics: ${label} API failed for ${clusterId}:`, handleSimpleError(error));
    return [];
  }
}

async function fetchStorageClasses(store: RancherStore, clusterId: string): Promise<string[]> {
  const storageClasses = await fetchClusterData<any>(
    store,
    clusterId,
    'storage.k8s.io.storageclasses',
    'storage classes'
  );

  return storageClasses
    .map((sc: any) => sc.metadata?.name || sc.name || sc.id)
    .filter(Boolean);
}

function estimateGpuMemory(node: NodeResource, gpuCount: number): number {
  // Try to detect GPU type from node labels or annotations
  const labels = node.metadata?.labels || {};
  const annotations = node.metadata?.annotations || {};
  
  // Look for common GPU indicators
  const gpuInfo = JSON.stringify({ labels, annotations }).toLowerCase();
  
  if (gpuInfo.includes('a100') || gpuInfo.includes('a40')) {
    return gpuCount * 40; // A100 = 40GB, A40 = 48GB (use conservative)
  } else if (gpuInfo.includes('v100')) {
    return gpuCount * 16; // V100 = 16GB or 32GB (use conservative)
  } else if (gpuInfo.includes('t4')) {
    return gpuCount * 16; // T4 = 16GB
  } else if (gpuInfo.includes('rtx') || gpuInfo.includes('4090')) {
    return gpuCount * 24; // RTX 4090 = 24GB
  } else if (gpuInfo.includes('3090')) {
    return gpuCount * 24; // RTX 3090 = 24GB
  } else {
    // Default assumption for consumer/prosumer GPUs
    return gpuCount * 8; // Conservative estimate
  }
}
