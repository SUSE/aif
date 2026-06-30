/**
 * App Resource Model - Rich domain model for SUSE AI applications
 * Features computed properties and actions for comprehensive app management
 */

import SuseaiResource, { Action, ActionOpts } from '../base/suseai-resource';
import { InstallationMixin, StateMixin, MetadataMixin, ResourceUtils, InstallationInfo } from '../base/resource-mixin';

export interface AppResourceData {
  name: string;
  slug_name: string;
  description?: string;
  project_url?: string;
  documentation_url?: string;
  reference_guide_url?: string;
  source_code_url?: string;
  logo_url?: string;
  changelog_url?: string;
  last_updated_at?: string;
  packaging_format?: 'HELM_CHART' | 'CONTAINER';
  installations?: InstallationInfo[];
}

export interface InstallOptions {
  clusterId: string;
  namespace: string;
  releaseName: string;
  chartRepo?: string;
  chartName?: string;
  chartVersion?: string;
  values?: Record<string, any>;
}

export interface UpgradeOptions {
  clusterId: string;
  namespace: string;
  releaseName: string;
  chartVersion?: string;
  values?: Record<string, any>;
}

/**
 * App Resource class with rich computed properties and actions
 * Provides comprehensive application lifecycle management
 */
export default class AppResource extends SuseaiResource {
  // App-specific properties
  public appName!: string;
  public slug_name!: string;
  public description?: string;
  public project_url?: string;
  public documentation_url?: string;
  public reference_guide_url?: string;
  public source_code_url?: string;
  public logo_url?: string;
  public changelog_url?: string;
  public last_updated_at?: string;
  public packaging_format?: 'HELM_CHART' | 'CONTAINER';
  public installations?: InstallationInfo[];

  // Mixins
  private installationMixin: InstallationMixin;
  private stateMixin: StateMixin;
  private metadataMixin: MetadataMixin;

  constructor(data: AppResourceData, store?: any, router?: any, route?: any) {
    super(data, store, router, route);
    Object.assign(this, data);
    this.appName = data.name;
    
    // Initialize mixins
    this.installationMixin = new InstallationMixin();
    this.stateMixin = new StateMixin();
    this.metadataMixin = new MetadataMixin();
    
    // Copy installation data to mixin
    if (data.installations) {
      this.installationMixin.installations = data.installations;
    }
  }

  // === Installation Properties (delegated to mixin) ===

  get isInstalled(): boolean {
    return this.installationMixin.isInstalled;
  }

  get installationClusters(): string[] {
    return this.installationMixin.installationClusters;
  }

  get isRunning(): boolean {
    return this.installationMixin.isRunning;
  }

  get hasFailed(): boolean {
    return this.installationMixin.hasFailed;
  }

  get hasErrors(): boolean {
    return this.hasFailed;
  }

  get isInstalling(): boolean {
    return this.installationMixin.isInstalling;
  }

  get isUpgrading(): boolean {
    return this.installationMixin.isUpgrading;
  }

  get isUninstalling(): boolean {
    return this.installationMixin.isUninstalling;
  }

  get latestInstallation(): InstallationInfo | undefined {
    return this.installationMixin.latestInstallation;
  }

  // === Installation Cluster Tracking ===

  /**
   * Get all cluster IDs where this app is installed
   */
  get installedClusters(): string[] {
    return this.installationMixin.installationClusters;
  }

  /**
   * Get count of clusters where app is installed
   */
  get installationCount(): number {
    return this.installedClusters.length;
  }

  /**
   * Check if app is installed on specific cluster
   */
  isInstalledOnCluster(clusterId: string): boolean {
    return this.installedClusters.includes(clusterId);
  }

  /**
   * Get installation info for specific cluster
   */
  getInstallationForCluster(clusterId: string): InstallationInfo | undefined {
    return this.installationMixin.getInstallationForCluster(clusterId);
  }

  /**
   * Check if app is installed on multiple clusters
   */
  get isMultiCluster(): boolean {
    return this.installationCount > 1;
  }

  /**
   * Get cluster names where app is installed (if available)
   */
  get installedClusterNames(): string[] {
    // This would need cluster information from store
    // For now, return cluster IDs
    return this.installedClusters;
  }

  // === App-specific computed properties ===

  /**
   * Get app identifier (slug name)
   */
  get id(): string {
    return this.slug_name;
  }

  /**
   * Override base name getter to return app name
   */
  get name(): string {
    return this.appName || this.slug_name;
  }

  /**
   * Get display name
   */
  get displayName(): string {
    return this.appName || this.slug_name;
  }

  /**
   * Get app logo URL or placeholder
   */
  get logoUrl(): string {
    return this.logo_url || this.getPlaceholderLogo();
  }

  /**
   * Get packaging format display
   */
  get packagingDisplay(): string {
    return this.packaging_format === 'HELM_CHART' ? 'Helm Chart' : 'Container';
  }

  /**
   * Get last updated display
   */
  get lastUpdatedDisplay(): string {
    if (!this.last_updated_at) return '';
    const date = new Date(this.last_updated_at);
    return date.toLocaleDateString();
  }

  /**
   * Check if app has documentation
   */
  get hasDocumentation(): boolean {
    return !!(this.documentation_url || this.reference_guide_url);
  }

  /**
   * Check if app has source code
   */
  get hasSourceCode(): boolean {
    return !!this.source_code_url;
  }

  /**
   * Check if app has project URL
   */
  get hasProjectUrl(): boolean {
    return !!this.project_url;
  }

  // === State Management ===

  /**
   * Override state display with app-specific logic
   */
  get stateDisplay(): string {
    if (this.isInstalling) return 'Installing';
    if (this.isUpgrading) return 'Upgrading';
    if (this.isUninstalling) return 'Uninstalling';
    if (this.hasFailed) return 'Error';
    if (this.isInstalled) return this.isRunning ? 'Running' : 'Installed';
    return 'Available';
  }

  /**
   * Get health status of the app
   */
  get healthStatus(): 'healthy' | 'degraded' | 'unhealthy' | 'unknown' {
    if (!this.isInstalled) return 'unknown';
    if (this.hasFailed) return 'unhealthy';
    if (this.isRunning) return 'healthy';
    return 'degraded';
  }

  /**
   * Check if app needs attention (failed, errors, etc.)
   */
  get needsAttention(): boolean {
    return this.hasFailed || this.hasErrors;
  }

  /**
   * Get overall app status summary
   */
  get statusSummary(): string {
    if (this.needsAttention) {
      return `${this.stateDisplay} - Needs attention`;
    }
    if (this.isMultiCluster) {
      return `${this.stateDisplay} on ${this.installationCount} clusters`;
    }
    return this.stateDisplay;
  }

  /**
   * Override state color with app-specific logic
   */
  get stateColor(): string {
    if (this.hasFailed) return 'error';
    if (this.isInstalling || this.isUpgrading || this.isUninstalling) return 'info';
    if (this.isInstalled) return 'success';
    return 'inactive';
  }

  // === Actions ===

  /**
   * Get available actions based on current state
   */
  get availableActions(): Action[] {
    const actions: Action[] = [];

    // Install action - available if not installed
    if (this.canInstall) {
      actions.push(INSTALL_ACTION);
    }

    // Manage action - available if installed
    if (this.canManage) {
      actions.push(MANAGE_ACTION);
    }

    // Upgrade action - available if installed and not transitioning
    if (this.canUpgrade) {
      actions.push(UPGRADE_ACTION);
    }

    // Uninstall action - available if installed
    if (this.canUninstall) {
      actions.push(UNINSTALL_ACTION);
    }

    // Documentation action - always available if docs exist
    if (this.hasDocumentation) {
      actions.push(VIEW_DOCS_ACTION);
    }

    // Source code action - always available if source exists
    if (this.hasSourceCode) {
      actions.push(VIEW_SOURCE_ACTION);
    }

    // Restart action - available if installed and not transitioning
    if (this.isInstalled && !this.isInstalling && !this.isUpgrading && !this.isUninstalling) {
      actions.push(RESTART_ACTION);
    }

    // Rollback action - available if installed and has previous versions
    if (this.isInstalled && this.installationCount > 0) {
      actions.push(ROLLBACK_ACTION);
    }

    // View logs action - available if installed
    if (this.isInstalled) {
      actions.push(VIEW_LOGS_ACTION);
    }

    // View resources action - available if installed  
    if (this.isInstalled) {
      actions.push(VIEW_RESOURCES_ACTION);
    }

    // Test health action - available if installed
    if (this.isInstalled) {
      actions.push(TEST_HEALTH_ACTION);
    }

    // Refresh action - always available
    actions.push(REFRESH_ACTION);

    return actions;
  }

  // === Action Permissions ===

  get canInstall(): boolean {
    return !this.isInstalled && !this.isInstalling;
  }

  get canManage(): boolean {
    return this.isInstalled && !this.isUninstalling;
  }

  get canUpgrade(): boolean {
    return this.isInstalled && !this.isInstalling && !this.isUpgrading && !this.isUninstalling;
  }

  get canUninstall(): boolean {
    return this.isInstalled && !this.isUninstalling;
  }

  // === Action Methods ===

  /**
   * Install app on specified cluster
   */
  async install(options: InstallOptions): Promise<void> {
    if (!this.canInstall) {
      throw new Error(`Cannot install ${this.appName}: ${this.stateDisplay}`);
    }

    return this.$dispatch('suseai/install', {
      app: this,
      ...options
    });
  }

  /**
   * Upgrade app installation
   */
  async upgrade(options: UpgradeOptions): Promise<void> {
    if (!this.canUpgrade) {
      throw new Error(`Cannot upgrade ${this.appName}: ${this.stateDisplay}`);
    }

    return this.$dispatch('suseai/upgrade', {
      app: this,
      ...options
    });
  }

  /**
   * Uninstall app from cluster(s)
   */
  async uninstall(clusterId?: string): Promise<void> {
    if (!this.canUninstall) {
      throw new Error(`Cannot uninstall ${this.appName}: ${this.stateDisplay}`);
    }

    const clusters = clusterId ? [clusterId] : this.installationClusters;
    
    return this.$dispatch('suseai/uninstall', {
      app: this,
      clusters
    });
  }

  /**
   * Restart app (uninstall then reinstall with same configuration)
   */
  async restart(clusterId?: string): Promise<void> {
    if (!this.isInstalled) {
      throw new Error(`Cannot restart ${this.appName}: not installed`);
    }

    const clusters = clusterId ? [clusterId] : this.installationClusters;
    
    return this.$dispatch('suseai/restart', {
      app: this,
      clusters
    });
  }

  /**
   * Rollback to previous version
   */
  async rollback(clusterId: string, revision?: number): Promise<void> {
    if (!this.isInstalled) {
      throw new Error(`Cannot rollback ${this.appName}: not installed`);
    }

    return this.$dispatch('suseai/rollback', {
      app: this,
      clusterId,
      revision
    });
  }

  /**
   * Refresh app state and installation info
   */
  async refresh(): Promise<void> {
    return this.$dispatch('suseai/refreshApp', {
      app: this
    });
  }

  /**
   * Get app logs from specific cluster
   */
  async getLogs(clusterId: string, options?: { 
    namespace?: string; 
    releaseName?: string; 
    lines?: number; 
  }): Promise<string[]> {
    const installation = this.getInstallationForCluster(clusterId);
    if (!installation) {
      throw new Error(`App ${this.appName} not installed on cluster ${clusterId}`);
    }

    return this.$dispatch('suseai/getAppLogs', {
      app: this,
      clusterId,
      namespace: options?.namespace || installation.namespace,
      releaseName: options?.releaseName || installation.releaseName,
      lines: options?.lines || 100
    });
  }

  /**
   * Get app resources from specific cluster
   */
  async getResources(clusterId: string): Promise<any[]> {
    const installation = this.getInstallationForCluster(clusterId);
    if (!installation) {
      throw new Error(`App ${this.appName} not installed on cluster ${clusterId}`);
    }

    return this.$dispatch('suseai/getAppResources', {
      app: this,
      clusterId,
      namespace: installation.namespace,
      releaseName: installation.releaseName
    });
  }

  /**
   * Test app connectivity/health
   */
  async testHealth(clusterId: string): Promise<{
    healthy: boolean;
    message: string;
    details?: any;
  }> {
    const installation = this.getInstallationForCluster(clusterId);
    if (!installation) {
      throw new Error(`App ${this.appName} not installed on cluster ${clusterId}`);
    }

    return this.$dispatch('suseai/testAppHealth', {
      app: this,
      clusterId,
      namespace: installation.namespace,
      releaseName: installation.releaseName
    });
  }

  /**
   * Navigate to install page
   */
  async goToInstall(clusterId?: string): Promise<void> {
    const cluster = clusterId || this.$currentRoute?.params?.cluster || 'local';
    
    await this.$push({
      name: 'c-cluster-suseai-install',
      params: { 
        product: 'suseai', 
        cluster, 
        slug: this.slug_name 
      },
      query: { n: this.appName }
    });
  }

  /**
   * Navigate to manage page
   */
  async goToManage(clusterId?: string): Promise<void> {
    const cluster = clusterId || this.installationClusters[0] || this.$currentRoute?.params?.cluster || 'local';
    
    await this.$push({
      name: 'c-cluster-suseai-manage',
      params: { 
        product: 'suseai', 
        cluster, 
        slug: this.slug_name 
      },
      query: { n: this.appName }
    });
  }

  /**
   * Open documentation in new tab
   */
  openDocumentation(): void {
    const url = this.documentation_url || this.reference_guide_url;
    if (url) {
      window.open(url, '_blank', 'noopener,noreferrer');
    }
  }

  /**
   * Open source code in new tab
   */
  openSourceCode(): void {
    if (this.source_code_url) {
      window.open(this.source_code_url, '_blank', 'noopener,noreferrer');
    }
  }

  /**
   * Open project URL in new tab
   */
  openProjectUrl(): void {
    if (this.project_url) {
      window.open(this.project_url, '_blank', 'noopener,noreferrer');
    }
  }

  // === Helper Methods ===

  /**
   * Check if matches search query
   */
  matchesSearch(query: string): boolean {
    if (!query) return true;
    
    const searchText = query.toLowerCase();
    return (
      this.appName.toLowerCase().includes(searchText) ||
      this.slug_name.toLowerCase().includes(searchText) ||
      (this.description || '').toLowerCase().includes(searchText)
    );
  }

  /**
   * Get placeholder logo SVG
   */
  private getPlaceholderLogo(): string {
    return 'data:image/svg+xml;utf8,' +
      encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="36" height="36">
        <rect width="100%" height="100%" fill="#eef2f7"/>
        <text x="50%" y="55%" dominant-baseline="middle" text-anchor="middle" font-size="10" fill="#8ea0b3">app</text>
      </svg>`);
  }

  /**
   * Update installation info
   */
  updateInstallations(installations: InstallationInfo[]): void {
    this.installations = installations;
    this.installationMixin.installations = installations;
  }

  /**
   * Convert to plain object for API calls
   */
  toJSON(): any {
    return {
      name: this.appName,
      slug_name: this.slug_name,
      description: this.description,
      project_url: this.project_url,
      documentation_url: this.documentation_url,
      reference_guide_url: this.reference_guide_url,
      source_code_url: this.source_code_url,
      logo_url: this.logo_url,
      changelog_url: this.changelog_url,
      last_updated_at: this.last_updated_at,
      packaging_format: this.packaging_format,
      installations: this.installations
    };
  }
}

// === Action Definitions ===

const INSTALL_ACTION: Action = {
  action: 'install',
  label: 'Install',
  icon: 'icon-plus',
  bulkable: true,
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    await app.goToInstall(opts.cluster);
  }
};

const MANAGE_ACTION: Action = {
  action: 'manage',
  label: 'Manage', 
  icon: 'icon-edit',
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    await app.goToManage(opts.cluster);
  }
};

const UPGRADE_ACTION: Action = {
  action: 'upgrade',
  label: 'Upgrade',
  icon: 'icon-upgrade',
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    await app.goToManage(opts.cluster);
  }
};

const UNINSTALL_ACTION: Action = {
  action: 'uninstall',
  label: 'Uninstall',
  icon: 'icon-delete',
  bulkable: true,
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    const confirmMessage = `Are you sure you want to uninstall "${app.appName}"? This action cannot be undone.`;
    
    if (confirm(confirmMessage)) {
      await app.uninstall(opts.cluster);
    }
  }
};

const VIEW_DOCS_ACTION: Action = {
  action: 'viewDocs',
  label: 'Documentation',
  icon: 'icon-file',
  invoke: (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    app.openDocumentation();
  }
};

const VIEW_SOURCE_ACTION: Action = {
  action: 'viewSource',
  label: 'Source Code',
  icon: 'icon-github',
  invoke: (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    app.openSourceCode();
  }
};

const RESTART_ACTION: Action = {
  action: 'restart',
  label: 'Restart',
  icon: 'icon-refresh',
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    await app.restart();
  }
};

const ROLLBACK_ACTION: Action = {
  action: 'rollback',
  label: 'Rollback',
  icon: 'icon-history',
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    // This would need cluster selection in a real implementation
    const clusterId = app.installedClusters[0]; // Use first cluster for now
    if (clusterId) {
      await app.rollback(clusterId);
    }
  }
};

const VIEW_LOGS_ACTION: Action = {
  action: 'viewLogs',
  label: 'View Logs',
  icon: 'icon-file',
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    // This would need cluster selection in a real implementation
    const clusterId = app.installedClusters[0]; // Use first cluster for now
    if (clusterId) {
      await app.getLogs(clusterId);
    }
  }
};

const VIEW_RESOURCES_ACTION: Action = {
  action: 'viewResources',
  label: 'View Resources',
  icon: 'icon-cluster',
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    // This would need cluster selection in a real implementation  
    const clusterId = app.installedClusters[0]; // Use first cluster for now
    if (clusterId) {
      await app.getResources(clusterId);
    }
  }
};

const TEST_HEALTH_ACTION: Action = {
  action: 'testHealth',
  label: 'Test Health',
  icon: 'icon-checkmark',
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    // This would need cluster selection in a real implementation
    const clusterId = app.installedClusters[0]; // Use first cluster for now
    if (clusterId) {
      await app.testHealth(clusterId);
    }
  }
};

const REFRESH_ACTION: Action = {
  action: 'refresh',
  label: 'Refresh',
  icon: 'icon-refresh',
  invoke: async (opts: ActionOpts) => {
    const app = opts.resource as AppResource;
    await app.refresh();
  }
};