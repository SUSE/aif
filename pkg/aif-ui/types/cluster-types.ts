/**
 * Cluster-related TypeScript interfaces and types
 * Provides comprehensive type definitions for cluster domain
 */

// === Cluster Status and State Types ===

export type ClusterState = 
  | 'active'
  | 'pending'
  | 'updating'
  | 'error'
  | 'unavailable'
  | 'removed'
  | 'unknown';

export type ClusterConnectionState = 
  | 'connected'
  | 'connecting'
  | 'disconnected'
  | 'error'
  | 'unknown';

export type NodeState = 
  | 'Ready'
  | 'NotReady'
  | 'Unknown'
  | 'SchedulingDisabled';

// === Cluster Core Interfaces ===

export interface ClusterInfo {
  id: string;
  name: string;
  displayName?: string;
  description?: string;
  provider?: string;
  region?: string;
  zone?: string;
  state: ClusterState;
  connectionState: ClusterConnectionState;
  ready: boolean;
  transitioning: boolean;
  transitioningMessage?: string;
  version: ClusterVersion;
  capabilities: ClusterCapabilities;
  stats: ClusterStats;
  metadata: ClusterMetadata;
  conditions: ClusterCondition[];
  
  // Timestamps
  created: string;
  updated: string;
  lastSeen?: string;
  lastActivity?: string;
}

export interface ClusterVersion {
  kubernetes: string;
  rancher?: string;
  distribution: string; // RKE2, K3s, EKS, GKE, AKS, etc.
  patch?: string;
  buildDate?: string;
  gitVersion?: string;
  gitCommit?: string;
  platform?: string;
}

export interface ClusterCapabilities {
  // App and Helm capabilities
  canInstallApps: boolean;
  canManageNamespaces: boolean;
  canAccessSecrets: boolean;
  canCreateServiceAccounts: boolean;
  hasHelmSupport: boolean;
  hasRancherAppsSupport: boolean;
  
  // Kubernetes capabilities
  supportedApiVersions: string[];
  hasRBAC: boolean;
  hasPodSecurityPolicies: boolean;
  hasNetworkPolicies: boolean;
  hasStorageClasses: boolean;
  hasIngressControllers: boolean;
  hasLoadBalancer: boolean;
  hasMetricsServer: boolean;
  
  // Advanced features
  hasAutoscaling: boolean;
  hasLogging: boolean;
  hasMonitoring: boolean;
  hasBackup: boolean;
  hasMultitenancy: boolean;
  
  // Resource limits
  maxNamespaces?: number;
  maxNodes?: number;
  maxPodsPerNode?: number;
  
  // Networking
  networkProvider?: string;
  serviceSubnet?: string;
  podSubnet?: string;
  clusterDomain?: string;
}

export interface ClusterStats {
  // Node statistics
  totalNodes: number;
  readyNodes: number;
  masterNodes: number;
  workerNodes: number;
  
  // Resource statistics
  totalCPU: number; // in cores
  totalMemory: number; // in bytes
  totalStorage: number; // in bytes
  allocatedCPU: number;
  allocatedMemory: number;
  allocatedStorage: number;
  
  // Pod statistics
  totalPods: number;
  runningPods: number;
  pendingPods: number;
  failedPods: number;
  
  // Namespace statistics
  totalNamespaces: number;
  activeNamespaces: number;
  
  // App statistics
  totalApps: number;
  runningApps: number;
  failedApps: number;
  namespacesWithApps: number;
  lastAppActivity?: string;
  
  // Health metrics
  healthScore: number; // 0-100
  uptime: number; // in seconds
  lastHealthCheck?: string;
}

export interface ClusterMetadata {
  labels: Record<string, string>;
  annotations: Record<string, string>;
  finalizers?: string[];
  ownerReferences?: OwnerReference[];
  managedFields?: any[];
  resourceVersion?: string;
  selfLink?: string;
  uid?: string;
}

export interface OwnerReference {
  apiVersion: string;
  kind: string;
  name: string;
  uid: string;
  controller?: boolean;
  blockOwnerDeletion?: boolean;
}

export interface ClusterCondition {
  type: string;
  status: 'True' | 'False' | 'Unknown';
  lastTransitionTime: string;
  lastUpdateTime?: string;
  reason?: string;
  message?: string;
}

// === Node Information ===

export interface NodeInfo {
  name: string;
  displayName?: string;
  state: NodeState;
  ready: boolean;
  roles: string[];
  version: string;
  internalIP: string;
  externalIP?: string;
  hostname: string;
  
  // Resource information
  capacity: NodeResources;
  allocatable: NodeResources;
  allocated: NodeResources;
  
  // System information
  os: string;
  osImage: string;
  kernelVersion: string;
  architecture: string;
  containerRuntime: string;
  kubeletVersion: string;
  kubeProxyVersion: string;
  
  // Status
  conditions: NodeCondition[];
  taints: NodeTaint[];
  addresses: NodeAddress[];
  
  // Timestamps
  created: string;
  lastHeartbeat: string;
  uptime?: number; // in seconds
}

export interface NodeResources {
  cpu: string;
  memory: string;
  storage: string;
  pods: string;
  gpu?: string;
  hugepages?: Record<string, string>;
  ephemeralStorage?: string;
}

export interface NodeCondition {
  type: string;
  status: 'True' | 'False' | 'Unknown';
  lastTransitionTime: string;
  lastHeartbeatTime: string;
  reason?: string;
  message?: string;
}

export interface NodeTaint {
  key: string;
  value?: string;
  effect: 'NoSchedule' | 'PreferNoSchedule' | 'NoExecute';
  timeAdded?: string;
}

export interface NodeAddress {
  type: 'InternalIP' | 'ExternalIP' | 'Hostname' | 'InternalDNS' | 'ExternalDNS';
  address: string;
}

// === Resource Usage and Monitoring ===

export interface ResourceUsage {
  cpu: ResourceMetrics;
  memory: ResourceMetrics;
  storage: ResourceMetrics;
  network?: NetworkMetrics;
  pods: PodMetrics;
}

export interface ResourceMetrics {
  used: number;
  total: number;
  available: number;
  percentage: number;
  unit: string;
  trend?: 'up' | 'down' | 'stable';
  history?: TimeSeriesData[];
}

export interface NetworkMetrics {
  inbound: {
    bytes: number;
    packets: number;
    bytesPerSecond: number;
    packetsPerSecond: number;
  };
  outbound: {
    bytes: number;
    packets: number;
    bytesPerSecond: number;
    packetsPerSecond: number;
  };
}

export interface PodMetrics {
  running: number;
  pending: number;
  failed: number;
  succeeded: number;
  total: number;
  capacity: number;
  percentage: number;
}

export interface TimeSeriesData {
  timestamp: string;
  value: number;
}

// === Cluster Events and Monitoring ===

export interface ClusterEvent {
  id: string;
  type: 'Normal' | 'Warning' | 'Error';
  reason: string;
  message: string;
  source: {
    component: string;
    host?: string;
  };
  involvedObject?: {
    kind: string;
    name: string;
    namespace?: string;
    uid?: string;
    apiVersion?: string;
    resourceVersion?: string;
  };
  timestamp: string;
  firstTimestamp: string;
  lastTimestamp: string;
  count: number;
}

export interface ClusterAlert {
  id: string;
  name: string;
  severity: 'critical' | 'warning' | 'info';
  status: 'firing' | 'resolved' | 'suppressed';
  message: string;
  description?: string;
  source: string;
  labels: Record<string, string>;
  annotations: Record<string, string>;
  startsAt: string;
  endsAt?: string;
  generatorURL?: string;
  fingerprint: string;
}

// === Cluster Operations ===

export interface ClusterConnectionTest {
  connected: boolean;
  latency?: number; // in milliseconds
  capabilities: ClusterCapabilities;
  error?: string;
  warnings?: string[];
  timestamp: string;
}

export interface ClusterHealthCheck {
  overall: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  components: ClusterComponentHealth[];
  nodes: NodeHealthSummary[];
  score: number; // 0-100
  issues: ClusterHealthIssue[];
  lastCheck: string;
  checkDuration: number; // in milliseconds
}

export interface ClusterComponentHealth {
  name: string;
  status: 'healthy' | 'unhealthy' | 'unknown';
  message?: string;
  lastCheck: string;
}

export interface NodeHealthSummary {
  name: string;
  status: 'healthy' | 'unhealthy' | 'unknown';
  ready: boolean;
  issues: number;
}

export interface ClusterHealthIssue {
  type: 'error' | 'warning' | 'info';
  component: string;
  message: string;
  recommendation?: string;
  severity: 'critical' | 'high' | 'medium' | 'low';
  timestamp: string;
}

// === Namespace Management ===

export interface NamespaceInfo {
  name: string;
  displayName?: string;
  clusterId: string;
  clusterName?: string;
  status: 'Active' | 'Terminating';
  
  // Resource quotas and limits
  quotas?: ResourceQuota[];
  limits?: LimitRange[];
  
  // Statistics
  resourceCount: Record<string, number>;
  appCount: number;
  
  // Security
  podSecurityPolicy?: string;
  networkPolicies: string[];
  serviceAccounts: string[];
  
  // Metadata
  labels: Record<string, string>;
  annotations: Record<string, string>;
  
  // Timestamps
  created: string;
  lastActivity?: string;
}

export interface ResourceQuota {
  name: string;
  hard: Record<string, string>;
  used: Record<string, string>;
  scopes?: string[];
}

export interface LimitRange {
  name: string;
  limits: LimitRangeItem[];
}

export interface LimitRangeItem {
  type: string;
  max?: Record<string, string>;
  min?: Record<string, string>;
  default?: Record<string, string>;
  defaultRequest?: Record<string, string>;
  maxLimitRequestRatio?: Record<string, string>;
}

// === Cluster Configuration ===

export interface ClusterConfiguration {
  // Basic settings
  name: string;
  description?: string;
  
  // Network configuration
  serviceCIDR?: string;
  podCIDR?: string;
  dnsServiceIP?: string;
  clusterDomain?: string;
  
  // Feature gates
  featureGates?: Record<string, boolean>;
  
  // Component configurations
  etcd?: EtcdConfiguration;
  apiServer?: ApiServerConfiguration;
  controllerManager?: ControllerManagerConfiguration;
  scheduler?: SchedulerConfiguration;
  kubelet?: KubeletConfiguration;
  kubeProxy?: KubeProxyConfiguration;
  
  // Add-ons
  addons?: AddonConfiguration[];
  
  // Security
  podSecurityPolicy?: PodSecurityPolicyConfiguration;
  networkPolicy?: NetworkPolicyConfiguration;
  
  // Monitoring and logging
  monitoring?: MonitoringConfiguration;
  logging?: LoggingConfiguration;
}

export interface EtcdConfiguration {
  endpoints?: string[];
  caFile?: string;
  certFile?: string;
  keyFile?: string;
  snapshot?: {
    enabled: boolean;
    schedule?: string;
    retention?: number;
  };
}

export interface ApiServerConfiguration {
  extraArgs?: Record<string, string>;
  extraBinds?: string[];
  extraEnv?: Record<string, string>;
}

export interface ControllerManagerConfiguration {
  extraArgs?: Record<string, string>;
  extraBinds?: string[];
  extraEnv?: Record<string, string>;
}

export interface SchedulerConfiguration {
  extraArgs?: Record<string, string>;
  extraBinds?: string[];
  extraEnv?: Record<string, string>;
}

export interface KubeletConfiguration {
  extraArgs?: Record<string, string>;
  extraBinds?: string[];
  extraEnv?: Record<string, string>;
  clusterDNS?: string[];
  clusterDomain?: string;
  resolveConf?: string;
}

export interface KubeProxyConfiguration {
  extraArgs?: Record<string, string>;
  extraBinds?: string[];
  extraEnv?: Record<string, string>;
}

export interface AddonConfiguration {
  name: string;
  enabled: boolean;
  configuration?: Record<string, any>;
}

export interface PodSecurityPolicyConfiguration {
  enabled: boolean;
  defaultPolicy?: string;
  policies?: Record<string, any>;
}

export interface NetworkPolicyConfiguration {
  enabled: boolean;
  provider?: string;
  defaultDeny?: boolean;
}

export interface MonitoringConfiguration {
  enabled: boolean;
  prometheus?: {
    enabled: boolean;
    retention?: string;
    resources?: ResourceRequirements;
  };
  grafana?: {
    enabled: boolean;
    resources?: ResourceRequirements;
  };
  alertmanager?: {
    enabled: boolean;
    resources?: ResourceRequirements;
  };
}

export interface LoggingConfiguration {
  enabled: boolean;
  fluentd?: {
    enabled: boolean;
    resources?: ResourceRequirements;
  };
  elasticsearch?: {
    enabled: boolean;
    resources?: ResourceRequirements;
    retention?: string;
  };
  kibana?: {
    enabled: boolean;
    resources?: ResourceRequirements;
  };
}

export interface ResourceRequirements {
  requests?: {
    cpu?: string;
    memory?: string;
    storage?: string;
  };
  limits?: {
    cpu?: string;
    memory?: string;
    storage?: string;
  };
}

// === Cluster Collection and Filtering ===

export interface ClusterFilter {
  states?: ClusterState[];
  connectionStates?: ClusterConnectionState[];
  providers?: string[];
  regions?: string[];
  versions?: string[];
  hasApps?: boolean;
  healthScore?: {
    min?: number;
    max?: number;
  };
  nodeCount?: {
    min?: number;
    max?: number;
  };
  searchText?: string;
  labels?: Record<string, string>;
  createdAfter?: string;
  createdBefore?: string;
  lastSeenAfter?: string;
  lastSeenBefore?: string;
}

export interface ClusterSortOptions {
  field: 'name' | 'state' | 'version' | 'nodeCount' | 'appCount' | 'healthScore' | 'created' | 'lastSeen';
  direction: 'asc' | 'desc';
  secondary?: {
    field: ClusterSortOptions['field'];
    direction: 'asc' | 'desc';
  };
}

export interface ClusterQueryOptions {
  filter?: ClusterFilter;
  sort?: ClusterSortOptions;
  pagination?: {
    page: number;
    limit: number;
  };
  include?: ('stats' | 'health' | 'nodes' | 'apps' | 'events')[];
}

export interface ClusterQueryResult {
  clusters: ClusterSummary[];
  total: number;
  page: number;
  limit: number;
  hasMore: boolean;
  filters: ClusterFilter;
  sort: ClusterSortOptions;
}

// === Cluster Summary and List Types ===

export interface ClusterSummary {
  id: string;
  name: string;
  displayName?: string;
  description?: string;
  provider?: string;
  region?: string;
  state: ClusterState;
  connectionState: ClusterConnectionState;
  ready: boolean;
  version: ClusterVersionSummary;
  nodeCount: number;
  readyNodes: number;
  appCount: number;
  healthScore: number;
  uptime?: number;
  created: string;
  lastSeen?: string;
  flags: ClusterFlags;
}

export interface ClusterVersionSummary {
  kubernetes: string;
  distribution: string;
  rancher?: string;
}

export interface ClusterFlags {
  isLocal: boolean;
  isReady: boolean;
  isHealthy: boolean;
  hasApps: boolean;
  hasIssues: boolean;
  isTransitioning: boolean;
  isManaged: boolean;
  isImported: boolean;
  needsAttention: boolean;
}

// === API Response Types ===

export interface ClusterApiResponse<T = any> {
  data: T;
  success: boolean;
  message?: string;
  errors?: string[];
  warnings?: string[];
  metadata?: {
    clusterId?: string;
    clusterName?: string;
    timestamp: string;
    requestId?: string;
  };
}

export interface ClusterApiError {
  code: string;
  message: string;
  details?: any;
  clusterId?: string;
  timestamp: string;
  requestId?: string;
}

// === Utility Types ===

export type ClusterId = string;
export type NodeName = string;
export type NamespaceName = string;

// === Type Guards ===

export function isClusterState(value: any): value is ClusterState {
  return typeof value === 'string' && [
    'active', 'pending', 'updating', 'error', 'unavailable', 'removed', 'unknown'
  ].includes(value);
}

export function isConnectionState(value: any): value is ClusterConnectionState {
  return typeof value === 'string' && [
    'connected', 'connecting', 'disconnected', 'error', 'unknown'
  ].includes(value);
}

export function isNodeState(value: any): value is NodeState {
  return typeof value === 'string' && [
    'Ready', 'NotReady', 'Unknown', 'SchedulingDisabled'
  ].includes(value);
}

export function hasClusterStats(cluster: ClusterSummary | ClusterInfo): cluster is ClusterSummary & { stats: ClusterStats } {
  return 'stats' in cluster && !!cluster.stats;
}

export function hasClusterCapabilities(cluster: ClusterSummary | ClusterInfo): cluster is ClusterSummary & { capabilities: ClusterCapabilities } {
  return 'capabilities' in cluster && !!cluster.capabilities;
}

export function isHealthyCluster(cluster: ClusterSummary | ClusterInfo): boolean {
  return cluster.ready && cluster.state === 'active' && cluster.connectionState === 'connected';
}

export function isLocalCluster(cluster: ClusterSummary | ClusterInfo): boolean {
  return cluster.id === 'local' || cluster.name === 'local';
}

export function canInstallApps(cluster: ClusterInfo): boolean {
  return cluster.ready && 
    cluster.state === 'active' && 
    cluster.connectionState === 'connected' &&
    cluster.capabilities.canInstallApps;
}