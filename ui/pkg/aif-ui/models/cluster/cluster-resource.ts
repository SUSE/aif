/**
 * Cluster Resource Model - Manages cluster operations and state
 * Provides cluster-specific functionality following domain model patterns
 */

import SuseaiResource, { Action, ActionOpts } from '../base/suseai-resource';
import { StateMixin, MetadataMixin, ResourceUtils } from '../base/resource-mixin';

export interface ClusterCapabilities {
  canInstallApps: boolean;
  canManageNamespaces: boolean;
  canAccessSecrets: boolean;
  canCreateServiceAccounts: boolean;
  hasHelmSupport: boolean;
  hasRancherAppsSupport: boolean;
  supportedApiVersions: string[];
}

export interface ClusterStats {
  totalApps: number;
  runningApps: number;
  failedApps: number;
  namespacesWithApps: number;
  lastAppActivity?: string;
}

export interface ClusterVersion {
  kubernetes: string;
  rancher?: string;
  distribution?: string; // RKE2, K3s, etc.
}

export interface ClusterResourceData {
  id: string;
  name: string;
  displayName?: string;
  description?: string;
  provider?: string;
  ready: boolean;
  version?: ClusterVersion;
  capabilities?: ClusterCapabilities;
  stats?: ClusterStats;
  lastSeen?: string;
  
  // Cluster metadata from Rancher
  state?: string;
  transitioning?: boolean;
  transitioningMessage?: string;
  conditions?: Array<{
    type: string;
    status: string;
    message?: string;
    lastUpdateTime?: string;
  }>;
}

/**
 * Cluster Resource class with cluster-specific operations
 */
export default class ClusterResource extends SuseaiResource {
  // Cluster-specific properties
  public clusterId!: string;
  public clusterName!: string;
  public displayName?: string;
  public description?: string;
  public provider?: string;
  public ready!: boolean;
  public version?: ClusterVersion;
  public capabilities?: ClusterCapabilities;
  public stats?: ClusterStats;
  public lastSeen?: string;
  
  // Rancher-specific cluster state
  public state?: string;
  public transitioning?: boolean;
  public transitioningMessage?: string;
  public conditions?: Array<{
    type: string;
    status: string;
    message?: string;
    lastUpdateTime?: string;
  }>;

  // Mixins
  private stateMixin: StateMixin;
  private metadataMixin: MetadataMixin;

  constructor(data: ClusterResourceData, store?: any, router?: any, route?: any) {
    super(data, store, router, route);
    
    // Map data properties
    this.clusterId = data.id;
    this.clusterName = data.name;
    Object.assign(this, data);
    
    // Initialize mixins
    this.stateMixin = new StateMixin();
    this.metadataMixin = new MetadataMixin();
    
    // Map state for mixins
    if (data.state || data.transitioning) {
      this.stateMixin.status = {
        error: this.state === 'error' ? this.transitioningMessage : undefined,
        transitioning: data.transitioning,
        message: data.transitioningMessage,
        conditions: data.conditions
      };
    }
  }

  // === Basic Properties ===

  get id(): string {
    return this.clusterId;
  }

  get name(): string {
    return this.displayName || this.clusterName;
  }

  get shortName(): string {
    return this.clusterName;
  }

  get isLocal(): boolean {
    return this.clusterId === 'local';
  }

  get isReady(): boolean {
    return this.ready && this.state !== 'error';
  }

  // === State Management ===

  get stateDisplay(): string {
    if (this.state === 'error') return 'Error';
    if (this.transitioning) return 'Updating';
    if (!this.ready) return 'Not Ready';
    if (this.isReady) return 'Active';
    return 'Unknown';
  }

  get stateColor(): string {
    if (this.state === 'error') return 'error';
    if (this.transitioning) return 'info';
    if (!this.ready) return 'warning';
    if (this.isReady) return 'success';
    return 'inactive';
  }

  get hasError(): boolean {
    return this.state === 'error' || !this.ready;
  }

  get errorMessage(): string {
    return this.transitioningMessage || '';
  }

  get lastActivity(): string {
    if (this.stats?.lastAppActivity) {
      const date = new Date(this.stats.lastAppActivity);
      return date.toLocaleString();
    }
    return 'No recent activity';
  }

  // === Version Information ===

  get kubernetesVersion(): string {
    return this.version?.kubernetes || 'Unknown';
  }

  get rancherVersion(): string {
    return this.version?.rancher || 'Unknown';
  }

  get distributionDisplay(): string {
    const dist = this.version?.distribution;
    if (!dist) return 'Unknown';
    return dist.toUpperCase();
  }

  get versionDisplay(): string {
    const k8s = this.kubernetesVersion;
    const dist = this.version?.distribution;
    return dist ? `${dist.toUpperCase()} ${k8s}` : k8s;
  }

  // === Capabilities ===

  get canInstallApps(): boolean {
    return this.capabilities?.canInstallApps ?? this.isReady;
  }

  get canManageNamespaces(): boolean {
    return this.capabilities?.canManageNamespaces ?? this.isReady;
  }

  get canAccessSecrets(): boolean {
    return this.capabilities?.canAccessSecrets ?? this.isReady;
  }

  get hasHelmSupport(): boolean {
    return this.capabilities?.hasHelmSupport ?? true;
  }

  get hasRancherAppsSupport(): boolean {
    return this.capabilities?.hasRancherAppsSupport ?? true;
  }

  get supportedApiVersions(): string[] {
    return this.capabilities?.supportedApiVersions || [];
  }

  // === Statistics ===

  get appCount(): number {
    return this.stats?.totalApps || 0;
  }

  get runningAppCount(): number {
    return this.stats?.runningApps || 0;
  }

  get failedAppCount(): number {
    return this.stats?.failedApps || 0;
  }

  get namespacesWithApps(): number {
    return this.stats?.namespacesWithApps || 0;
  }

  get hasApps(): boolean {
    return this.appCount > 0;
  }

  get healthyAppsRatio(): number {
    if (this.appCount === 0) return 1;
    return this.runningAppCount / this.appCount;
  }

  // === Actions ===

  get availableActions(): Action[] {
    const actions: Action[] = [];

    if (this.canInstallApps) {
      actions.push(BROWSE_APPS_ACTION);
    }

    if (this.hasApps) {
      actions.push(VIEW_APPS_ACTION);
    }

    if (this.canManageNamespaces) {
      actions.push(MANAGE_NAMESPACES_ACTION);
    }

    actions.push(VIEW_CLUSTER_ACTION);

    if (!this.isLocal) {
      actions.push(DOWNLOAD_KUBECONFIG_ACTION);
    }

    return actions;
  }

  // === Cluster Operations ===

  /**
   * Navigate to browse apps page for this cluster
   */
  async browseApps(): Promise<void> {
    await this.$push({
      name: 'c-cluster-suseai-apps',
      params: { 
        product: 'suseai', 
        cluster: this.clusterId 
      }
    });
  }

  /**
   * Navigate to view installed apps on this cluster
   */
  async viewApps(): Promise<void> {
    await this.$push({
      name: 'c-cluster-suseai-apps',
      params: { 
        product: 'suseai', 
        cluster: this.clusterId 
      },
      query: { status: 'installed' }
    });
  }

  /**
   * Navigate to cluster management
   */
  async viewCluster(): Promise<void> {
    await this.$push({
      name: 'c-cluster-explorer',
      params: { cluster: this.clusterId }
    });
  }

  /**
   * Navigate to namespace management
   */
  async manageNamespaces(): Promise<void> {
    await this.$push({
      name: 'c-cluster-explorer-namespaces',
      params: { cluster: this.clusterId }
    });
  }

  /**
   * Download kubeconfig for this cluster
   */
  async downloadKubeconfig(): Promise<void> {
    if (!this.$store) return;
    
    try {
      await this.$store.dispatch('rancher/request', {
        url: `/v3/clusters/${this.clusterId}?action=generateKubeconfig`,
        method: 'POST'
      });
    } catch (error) {
      console.error('Failed to download kubeconfig:', error);
    }
  }

  /**
   * Refresh cluster state and capabilities
   */
  async refresh(): Promise<void> {
    if (!this.$store) return;
    
    try {
      const updated = await this.$store.dispatch('suseai/refreshCluster', {
        clusterId: this.clusterId
      });
      
      if (updated) {
        Object.assign(this, updated);
      }
    } catch (error) {
      console.warn('Failed to refresh cluster:', error);
    }
  }

  /**
   * Test cluster connectivity and capabilities
   */
  async testConnection(): Promise<{
    connected: boolean;
    capabilities: ClusterCapabilities;
    error?: string;
  }> {
    if (!this.$store) {
      throw new Error('Store not available for connection test');
    }
    
    try {
      const result = await this.$store.dispatch('suseai/testClusterConnection', {
        clusterId: this.clusterId
      });
      
      return result;
    } catch (error: any) {
      return {
        connected: false,
        capabilities: {
          canInstallApps: false,
          canManageNamespaces: false,
          canAccessSecrets: false,
          canCreateServiceAccounts: false,
          hasHelmSupport: false,
          hasRancherAppsSupport: false,
          supportedApiVersions: []
        },
        error: error?.message || 'Connection test failed'
      };
    }
  }

  /**
   * Get cluster resource usage (if available)
   */
  async getResourceUsage(): Promise<{
    cpu: { used: number; total: number; percentage: number };
    memory: { used: number; total: number; percentage: number };
    pods: { used: number; total: number; percentage: number };
  } | null> {
    if (!this.$store) return null;
    
    try {
      return await this.$store.dispatch('suseai/getClusterResourceUsage', {
        clusterId: this.clusterId
      });
    } catch (error) {
      console.warn('Failed to get resource usage:', error);
      return null;
    }
  }

  // === Utility Methods ===

  /**
   * Check if cluster supports specific API version
   */
  supportsApiVersion(apiVersion: string): boolean {
    return this.supportedApiVersions.includes(apiVersion);
  }

  /**
   * Get cluster age in human readable format
   */
  get age(): string {
    if (!this.lastSeen) return 'Unknown';
    
    const lastSeen = new Date(this.lastSeen);
    const now = new Date();
    const diffMs = now.getTime() - lastSeen.getTime();
    
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
    const diffHours = Math.floor((diffMs % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
    const diffMinutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));
    
    if (diffDays > 0) return `${diffDays}d`;
    if (diffHours > 0) return `${diffHours}h`;
    if (diffMinutes > 0) return `${diffMinutes}m`;
    return '< 1m';
  }

  /**
   * Compare clusters by name
   */
  compareTo(other: ClusterResource): number {
    // Local cluster always comes first
    if (this.isLocal && !other.isLocal) return -1;
    if (!this.isLocal && other.isLocal) return 1;
    
    // Then by ready state
    if (this.isReady && !other.isReady) return -1;
    if (!this.isReady && other.isReady) return 1;
    
    // Finally by name
    return this.name.localeCompare(other.name);
  }

  /**
   * Check if cluster matches search query
   */
  matchesSearch(query: string): boolean {
    if (!query) return true;
    
    const searchText = query.toLowerCase();
    return (
      this.name.toLowerCase().includes(searchText) ||
      this.clusterId.toLowerCase().includes(searchText) ||
      (this.description || '').toLowerCase().includes(searchText) ||
      (this.provider || '').toLowerCase().includes(searchText)
    );
  }

  /**
   * Update cluster statistics
   */
  updateStats(stats: Partial<ClusterStats>): void {
    this.stats = {
      totalApps: 0,
      runningApps: 0,
      failedApps: 0,
      namespacesWithApps: 0,
      ...this.stats,
      ...stats
    };
  }

  /**
   * Update cluster capabilities
   */
  updateCapabilities(capabilities: Partial<ClusterCapabilities>): void {
    this.capabilities = {
      canInstallApps: false,
      canManageNamespaces: false,
      canAccessSecrets: false,
      canCreateServiceAccounts: false,
      hasHelmSupport: false,
      hasRancherAppsSupport: false,
      supportedApiVersions: [],
      ...this.capabilities,
      ...capabilities
    };
  }

  /**
   * Convert to plain object
   */
  toJSON(): ClusterResourceData {
    return {
      id: this.clusterId,
      name: this.clusterName,
      displayName: this.displayName,
      description: this.description,
      provider: this.provider,
      ready: this.ready,
      version: this.version,
      capabilities: this.capabilities,
      stats: this.stats,
      lastSeen: this.lastSeen,
      state: this.state,
      transitioning: this.transitioning,
      transitioningMessage: this.transitioningMessage,
      conditions: this.conditions
    };
  }
}

// === Action Definitions ===

const BROWSE_APPS_ACTION: Action = {
  action: 'browseApps',
  label: 'Browse Apps',
  icon: 'icon-apps',
  invoke: async (opts: ActionOpts) => {
    const cluster = opts.resource as ClusterResource;
    await cluster.browseApps();
  }
};

const VIEW_APPS_ACTION: Action = {
  action: 'viewApps',
  label: 'View Installed Apps',
  icon: 'icon-view',
  invoke: async (opts: ActionOpts) => {
    const cluster = opts.resource as ClusterResource;
    await cluster.viewApps();
  }
};

const VIEW_CLUSTER_ACTION: Action = {
  action: 'viewCluster',
  label: 'View Cluster',
  icon: 'icon-cluster',
  invoke: async (opts: ActionOpts) => {
    const cluster = opts.resource as ClusterResource;
    await cluster.viewCluster();
  }
};

const MANAGE_NAMESPACES_ACTION: Action = {
  action: 'manageNamespaces',
  label: 'Manage Namespaces',
  icon: 'icon-namespace',
  invoke: async (opts: ActionOpts) => {
    const cluster = opts.resource as ClusterResource;
    await cluster.manageNamespaces();
  }
};

const DOWNLOAD_KUBECONFIG_ACTION: Action = {
  action: 'downloadKubeconfig',
  label: 'Download Kubeconfig',
  icon: 'icon-download',
  invoke: async (opts: ActionOpts) => {
    const cluster = opts.resource as ClusterResource;
    await cluster.downloadKubeconfig();
  }
};