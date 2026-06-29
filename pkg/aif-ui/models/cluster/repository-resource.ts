/**
 * Repository Resource Model - Manages Helm/OCI chart repositories
 * Provides repository-specific functionality following resource management patterns
 */

import SuseaiResource, { Action, ActionOpts } from '../base/suseai-resource';
import { StateMixin, MetadataMixin } from '../base/resource-mixin';

export type RepositoryType = 'helm' | 'oci' | 'git';

export interface RepositoryCredentials {
  username?: string;
  password?: string;
  certificateAuthority?: string;
  insecureSkipTLS?: boolean;
}

export interface RepositoryStats {
  chartCount: number;
  lastIndexed?: string;
  indexSize?: number;
  popularCharts: Array<{
    name: string;
    downloadCount?: number;
    version: string;
  }>;
}

export interface RepositoryHealth {
  healthy: boolean;
  lastChecked: string;
  responseTime?: number;
  error?: string;
  uptime?: number; // percentage
}

export interface RepositoryResourceData {
  name: string;
  displayName?: string;
  description?: string;
  url: string;
  type: RepositoryType;
  enabled: boolean;
  
  // Repository state
  ready: boolean;
  transitioning?: boolean;
  error?: string;
  
  // Repository details
  credentials?: RepositoryCredentials;
  stats?: RepositoryStats;
  health?: RepositoryHealth;
  
  // Metadata
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  createdAt?: string;
  updatedAt?: string;
  
  // Conditions from Kubernetes
  conditions?: Array<{
    type: string;
    status: string;
    message?: string;
    lastTransitionTime?: string;
  }>;
}

/**
 * Repository Resource class for managing chart repositories
 */
export default class RepositoryResource extends SuseaiResource {
  // Repository-specific properties
  public repoName!: string;
  public displayName?: string;
  public description?: string;
  public url!: string;
  public type!: RepositoryType;
  public enabled!: boolean;
  
  // State properties
  public ready!: boolean;
  public transitioning?: boolean;
  public error?: string;
  
  // Details
  public credentials?: RepositoryCredentials;
  public stats?: RepositoryStats;
  public health?: RepositoryHealth;
  public repoLabels?: Record<string, string>;
  public repoAnnotations?: Record<string, string>;
  public createdAt?: string;
  public updatedAt?: string;
  public conditions?: Array<{
    type: string;
    status: string;
    message?: string;
    lastTransitionTime?: string;
  }>;

  // Mixins
  private stateMixin: StateMixin;
  private metadataMixin: MetadataMixin;

  constructor(data: RepositoryResourceData, store?: any, router?: any, route?: any) {
    super(data, store, router, route);
    
    // Map data properties
    this.repoName = data.name;
    Object.assign(this, data);
    this.repoLabels = data.labels;
    this.repoAnnotations = data.annotations;
    
    // Initialize mixins
    this.stateMixin = new StateMixin();
    this.metadataMixin = new MetadataMixin();
    
    // Map state for mixins
    if (data.error || data.transitioning) {
      this.stateMixin.status = {
        error: data.error,
        transitioning: data.transitioning,
        conditions: data.conditions
      };
    }
  }

  // === Basic Properties ===

  get id(): string {
    return this.repoName;
  }

  get name(): string {
    return this.displayName || this.repoName;
  }

  get shortName(): string {
    return this.repoName;
  }

  get isEnabled(): boolean {
    return this.enabled && this.ready;
  }

  get isHealthy(): boolean {
    return this.health?.healthy ?? this.isReady;
  }

  get isReady(): boolean {
    return this.ready && !this.hasError;
  }

  // === Type Properties ===

  get isHelmRepository(): boolean {
    return this.type === 'helm';
  }

  get isOCIRepository(): boolean {
    return this.type === 'oci';
  }

  get isGitRepository(): boolean {
    return this.type === 'git';
  }

  get typeDisplay(): string {
    switch (this.type) {
      case 'helm':
        return 'Helm Repository';
      case 'oci':
        return 'OCI Registry';
      case 'git':
        return 'Git Repository';
      default:
        return 'Repository';
    }
  }

  get protocolDisplay(): string {
    if (this.url.startsWith('https://')) return 'HTTPS';
    if (this.url.startsWith('http://')) return 'HTTP';
    if (this.url.startsWith('oci://')) return 'OCI';
    if (this.url.startsWith('git+https://')) return 'Git HTTPS';
    return 'Unknown';
  }

  // === State Management ===

  get stateDisplay(): string {
    if (this.hasError) return 'Error';
    if (this.transitioning) return 'Updating';
    if (!this.enabled) return 'Disabled';
    if (!this.ready) return 'Not Ready';
    if (this.isHealthy) return 'Active';
    return 'Unhealthy';
  }

  get stateColor(): string {
    if (this.hasError) return 'error';
    if (this.transitioning) return 'info';
    if (!this.enabled) return 'inactive';
    if (!this.ready) return 'warning';
    if (this.isHealthy) return 'success';
    return 'warning';
  }

  get hasError(): boolean {
    return !!this.error || this.health?.healthy === false;
  }

  get errorMessage(): string {
    return this.error || this.health?.error || '';
  }

  get statusMessage(): string {
    if (this.hasError) return this.errorMessage;
    if (this.transitioning) return 'Repository is updating';
    if (!this.enabled) return 'Repository is disabled';
    if (!this.ready) return 'Repository is not ready';
    if (this.health?.lastChecked) {
      const lastChecked = new Date(this.health.lastChecked);
      return `Last checked: ${lastChecked.toLocaleString()}`;
    }
    return '';
  }

  // === Statistics ===

  get chartCount(): number {
    return this.stats?.chartCount || 0;
  }

  get hasCharts(): boolean {
    return this.chartCount > 0;
  }

  get indexAge(): string {
    if (!this.stats?.lastIndexed) return 'Never indexed';
    
    const lastIndexed = new Date(this.stats.lastIndexed);
    const now = new Date();
    const diffMs = now.getTime() - lastIndexed.getTime();
    
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
    const diffHours = Math.floor((diffMs % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
    const diffMinutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));
    
    if (diffDays > 0) return `${diffDays}d ago`;
    if (diffHours > 0) return `${diffHours}h ago`;
    if (diffMinutes > 0) return `${diffMinutes}m ago`;
    return 'Just now';
  }

  get indexSizeDisplay(): string {
    if (!this.stats?.indexSize) return 'Unknown';
    
    const size = this.stats.indexSize;
    if (size < 1024) return `${size} B`;
    if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
    if (size < 1024 * 1024 * 1024) return `${(size / (1024 * 1024)).toFixed(1)} MB`;
    return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  }

  get popularCharts(): Array<{ name: string; downloadCount?: number; version: string }> {
    return this.stats?.popularCharts || [];
  }

  // === Health Properties ===

  get responseTime(): number | undefined {
    return this.health?.responseTime;
  }

  get responseTimeDisplay(): string {
    const time = this.responseTime;
    if (time === undefined) return 'Unknown';
    if (time < 1000) return `${time}ms`;
    return `${(time / 1000).toFixed(1)}s`;
  }

  get uptimeDisplay(): string {
    const uptime = this.health?.uptime;
    if (uptime === undefined) return 'Unknown';
    return `${uptime.toFixed(1)}%`;
  }

  // === Security Properties ===

  get requiresCredentials(): boolean {
    return !!this.credentials && (!!this.credentials.username || !!this.credentials.password);
  }

  get isSecure(): boolean {
    return this.url.startsWith('https://') || this.url.startsWith('oci://');
  }

  get hasInsecureConfig(): boolean {
    return this.credentials?.insecureSkipTLS === true;
  }

  // === Actions ===

  get availableActions(): Action[] {
    const actions: Action[] = [];

    if (this.hasCharts) {
      actions.push(BROWSE_CHARTS_ACTION);
    }

    if (this.canRefresh) {
      actions.push(REFRESH_INDEX_ACTION);
    }

    if (this.canEdit) {
      actions.push(EDIT_REPO_ACTION);
    }

    if (this.canToggle) {
      actions.push(this.enabled ? DISABLE_REPO_ACTION : ENABLE_REPO_ACTION);
    }

    actions.push(TEST_CONNECTION_ACTION);

    if (this.canDelete) {
      actions.push(DELETE_REPO_ACTION);
    }

    return actions;
  }

  // === Action Permissions ===

  get canRefresh(): boolean {
    return this.isEnabled && !this.transitioning;
  }

  get canEdit(): boolean {
    return !this.transitioning;
  }

  get canToggle(): boolean {
    return !this.transitioning;
  }

  get canDelete(): boolean {
    return !this.transitioning;
  }

  get canTestConnection(): boolean {
    return true; // Can always test connection
  }

  // === Repository Operations ===

  /**
   * Refresh repository index
   */
  async refreshIndex(): Promise<void> {
    if (!this.canRefresh) {
      throw new Error(`Cannot refresh repository ${this.name}: ${this.stateDisplay}`);
    }

    return this.$dispatch('suseai/refreshRepository', {
      repositoryName: this.repoName
    });
  }

  /**
   * Test repository connection
   */
  async testConnection(): Promise<{
    success: boolean;
    responseTime?: number;
    error?: string;
    chartCount?: number;
  }> {
    return this.$dispatch('suseai/testRepositoryConnection', {
      repositoryName: this.repoName
    });
  }

  /**
   * Enable repository
   */
  async enable(): Promise<void> {
    if (this.enabled) return;

    return this.$dispatch('suseai/enableRepository', {
      repositoryName: this.repoName
    });
  }

  /**
   * Disable repository
   */
  async disable(): Promise<void> {
    if (!this.enabled) return;

    return this.$dispatch('suseai/disableRepository', {
      repositoryName: this.repoName
    });
  }

  /**
   * Update repository configuration
   */
  async update(config: Partial<RepositoryResourceData>): Promise<void> {
    return this.$dispatch('suseai/updateRepository', {
      repositoryName: this.repoName,
      config
    });
  }

  /**
   * Delete repository
   */
  async delete(): Promise<void> {
    return this.$dispatch('suseai/deleteRepository', {
      repositoryName: this.repoName
    });
  }

  /**
   * Browse charts in this repository
   */
  async browseCharts(): Promise<void> {
    await this.$push({
      name: 'c-cluster-suseai-apps',
      params: { 
        product: 'suseai', 
        cluster: this.$currentRoute?.params?.cluster || 'local'
      },
      query: { repository: this.repoName }
    });
  }

  /**
   * Navigate to repository edit page
   */
  async editRepository(): Promise<void> {
    await this.$push({
      name: 'c-cluster-suseai-repository-edit',
      params: { 
        product: 'suseai', 
        cluster: this.$currentRoute?.params?.cluster || 'local',
        id: this.repoName
      }
    });
  }

  // === Chart Operations ===

  /**
   * Search charts in this repository
   */
  async searchCharts(query: string): Promise<Array<{
    name: string;
    version: string;
    description?: string;
    appVersion?: string;
  }>> {
    if (!this.$store) return [];

    try {
      return await this.$store.dispatch('suseai/searchRepositoryCharts', {
        repositoryName: this.repoName,
        query
      });
    } catch (error) {
      console.warn(`Failed to search charts in ${this.repoName}:`, error);
      return [];
    }
  }

  /**
   * Get chart details from this repository
   */
  async getChart(chartName: string): Promise<{
    name: string;
    versions: string[];
    description?: string;
    home?: string;
    sources?: string[];
    maintainers?: Array<{ name: string; email?: string }>;
  } | null> {
    if (!this.$store) return null;

    try {
      return await this.$store.dispatch('suseai/getRepositoryChart', {
        repositoryName: this.repoName,
        chartName
      });
    } catch (error) {
      console.warn(`Failed to get chart ${chartName} from ${this.repoName}:`, error);
      return null;
    }
  }

  // === Utility Methods ===

  /**
   * Check if repository matches search query
   */
  matchesSearch(query: string): boolean {
    if (!query) return true;
    
    const searchText = query.toLowerCase();
    return (
      this.name.toLowerCase().includes(searchText) ||
      this.repoName.toLowerCase().includes(searchText) ||
      (this.description || '').toLowerCase().includes(searchText) ||
      this.url.toLowerCase().includes(searchText) ||
      this.typeDisplay.toLowerCase().includes(searchText)
    );
  }

  /**
   * Get repository age in human readable format
   */
  get age(): string {
    if (!this.createdAt) return 'Unknown';
    
    const created = new Date(this.createdAt);
    const now = new Date();
    const diffMs = now.getTime() - created.getTime();
    
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
    const diffHours = Math.floor((diffMs % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
    const diffMinutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));
    
    if (diffDays > 0) return `${diffDays}d`;
    if (diffHours > 0) return `${diffHours}h`;
    if (diffMinutes > 0) return `${diffMinutes}m`;
    return '< 1m';
  }

  /**
   * Update repository statistics
   */
  updateStats(stats: Partial<RepositoryStats>): void {
    this.stats = {
      chartCount: 0,
      popularCharts: [],
      ...this.stats,
      ...stats
    };
  }

  /**
   * Update repository health
   */
  updateHealth(health: Partial<RepositoryHealth>): void {
    this.health = {
      healthy: false,
      lastChecked: new Date().toISOString(),
      ...this.health,
      ...health
    };
  }

  /**
   * Override base class labels getter to return repo-specific labels
   */
  get labels(): Record<string, string> {
    return this.repoLabels || {};
  }

  /**
   * Override base class annotations getter to return repo-specific annotations
   */
  get annotations(): Record<string, string> {
    return this.repoAnnotations || {};
  }

  /**
   * Convert to plain object
   */
  toJSON(): RepositoryResourceData {
    return {
      name: this.repoName,
      displayName: this.displayName,
      description: this.description,
      url: this.url,
      type: this.type,
      enabled: this.enabled,
      ready: this.ready,
      transitioning: this.transitioning,
      error: this.error,
      credentials: this.credentials,
      stats: this.stats,
      health: this.health,
      labels: this.repoLabels,
      annotations: this.repoAnnotations,
      createdAt: this.createdAt,
      updatedAt: this.updatedAt,
      conditions: this.conditions
    };
  }
}

// === Action Definitions ===

const BROWSE_CHARTS_ACTION: Action = {
  action: 'browseCharts',
  label: 'Browse Charts',
  icon: 'icon-apps',
  invoke: async (opts: ActionOpts) => {
    const repo = opts.resource as RepositoryResource;
    await repo.browseCharts();
  }
};

const REFRESH_INDEX_ACTION: Action = {
  action: 'refreshIndex',
  label: 'Refresh Index',
  icon: 'icon-refresh',
  invoke: async (opts: ActionOpts) => {
    const repo = opts.resource as RepositoryResource;
    await repo.refreshIndex();
  }
};

const EDIT_REPO_ACTION: Action = {
  action: 'editRepository',
  label: 'Edit',
  icon: 'icon-edit',
  invoke: async (opts: ActionOpts) => {
    const repo = opts.resource as RepositoryResource;
    await repo.editRepository();
  }
};

const ENABLE_REPO_ACTION: Action = {
  action: 'enableRepository',
  label: 'Enable',
  icon: 'icon-play',
  invoke: async (opts: ActionOpts) => {
    const repo = opts.resource as RepositoryResource;
    await repo.enable();
  }
};

const DISABLE_REPO_ACTION: Action = {
  action: 'disableRepository',
  label: 'Disable',
  icon: 'icon-pause',
  invoke: async (opts: ActionOpts) => {
    const repo = opts.resource as RepositoryResource;
    await repo.disable();
  }
};

const TEST_CONNECTION_ACTION: Action = {
  action: 'testConnection',
  label: 'Test Connection',
  icon: 'icon-checkmark',
  invoke: async (opts: ActionOpts) => {
    const repo = opts.resource as RepositoryResource;
    const result = await repo.testConnection();
    
    const message = result.success 
      ? `Connection successful! Found ${result.chartCount || 0} charts (${result.responseTime}ms)`
      : `Connection failed: ${result.error}`;
      
    // Show notification (this would need to be implemented in the store)
    opts.$store?.dispatch('growl/success', { 
      title: 'Repository Test',
      message 
    });
  }
};

const DELETE_REPO_ACTION: Action = {
  action: 'deleteRepository',
  label: 'Delete',
  icon: 'icon-delete',
  invoke: async (opts: ActionOpts) => {
    const repo = opts.resource as RepositoryResource;
    const confirmMessage = `Are you sure you want to delete repository "${repo.name}"? This action cannot be undone.`;
    
    if (confirm(confirmMessage)) {
      await repo.delete();
    }
  }
};