/**
 * Common computed properties and utilities for SUSE AI resources
 * Provides reusable functionality that can be mixed into resource classes
 */

export interface InstallationInfo {
  clusterId: string;
  namespace: string;
  releaseName: string;
  status: 'pending' | 'installing' | 'deployed' | 'upgrading' | 'uninstalling' | 'failed' | 'superseded' | 'unknown';
  version?: string;
  lastDeployed?: string;
  notes?: string;
}

export interface ClusterInfo {
  id: string;
  name: string;
  ready: boolean;
  version?: string;
}

/**
 * Mixin for installation-related functionality
 */
export class InstallationMixin {
  public installations?: InstallationInfo[];

  /**
   * Check if resource is installed anywhere
   */
  get isInstalled(): boolean {
    return !!this.installations && this.installations.length > 0;
  }

  /**
   * Get clusters where resource is installed
   */
  get installationClusters(): string[] {
    return this.installations?.map(i => i.clusterId) || [];
  }

  /**
   * Get installations by status
   */
  getInstallationsByStatus(status: InstallationInfo['status']): InstallationInfo[] {
    return this.installations?.filter(i => i.status === status) || [];
  }

  /**
   * Check if resource is running (deployed) anywhere
   */
  get isRunning(): boolean {
    return this.installations?.some(i => i.status === 'deployed') || false;
  }

  /**
   * Check if resource has any failed installations
   */
  get hasFailed(): boolean {
    return this.installations?.some(i => i.status === 'failed') || false;
  }

  /**
   * Check if resource is currently installing
   */
  get isInstalling(): boolean {
    return this.installations?.some(i => i.status === 'installing') || false;
  }

  /**
   * Check if resource is currently upgrading
   */
  get isUpgrading(): boolean {
    return this.installations?.some(i => i.status === 'upgrading') || false;
  }

  /**
   * Check if resource is currently uninstalling
   */
  get isUninstalling(): boolean {
    return this.installations?.some(i => i.status === 'uninstalling') || false;
  }

  /**
   * Get installation for specific cluster
   */
  getInstallationForCluster(clusterId: string): InstallationInfo | undefined {
    return this.installations?.find(i => i.clusterId === clusterId);
  }

  /**
   * Get latest installation (by lastDeployed)
   */
  get latestInstallation(): InstallationInfo | undefined {
    if (!this.installations || this.installations.length === 0) return undefined;
    
    return this.installations.reduce((latest, current) => {
      if (!latest) return current;
      if (!current.lastDeployed) return latest;
      if (!latest.lastDeployed) return current;
      
      return new Date(current.lastDeployed) > new Date(latest.lastDeployed) ? current : latest;
    });
  }
}

/**
 * Mixin for state management functionality
 */
export class StateMixin {
  public status?: {
    error?: string;
    transitioning?: boolean;
    message?: string;
    conditions?: Array<{
      type: string;
      status: string;
      message?: string;
      lastTransitionTime?: string;
    }>;
  };

  /**
   * Get overall state display
   */
  get stateDisplay(): string {
    if (this.hasError) return 'Error';
    if (this.isTransitioning) return 'Updating';
    return 'Active';
  }

  /**
   * Get state color for UI
   */
  get stateColor(): string {
    if (this.hasError) return 'error';
    if (this.isTransitioning) return 'info';
    return 'success';
  }

  /**
   * Check if resource has error
   */
  get hasError(): boolean {
    return !!this.status?.error || this.hasFailedCondition;
  }

  /**
   * Check if resource is transitioning
   */
  get isTransitioning(): boolean {
    return !!this.status?.transitioning;
  }

  /**
   * Get error message
   */
  get errorMessage(): string {
    return this.status?.error || this.getFailedConditionMessage() || '';
  }

  /**
   * Get status message
   */
  get statusMessage(): string {
    return this.status?.message || '';
  }

  /**
   * Check if has failed condition
   */
  get hasFailedCondition(): boolean {
    return this.status?.conditions?.some(c => c.status === 'False') || false;
  }

  /**
   * Get failed condition message
   */
  getFailedConditionMessage(): string {
    const failedCondition = this.status?.conditions?.find(c => c.status === 'False');
    return failedCondition?.message || '';
  }

  /**
   * Get condition by type
   */
  getCondition(type: string) {
    return this.status?.conditions?.find(c => c.type === type);
  }

  /**
   * Check if condition is true
   */
  hasCondition(type: string): boolean {
    const condition = this.getCondition(type);
    return condition?.status === 'True';
  }
}

/**
 * Mixin for metadata functionality
 */
export class MetadataMixin {
  public metadata?: {
    name?: string;
    namespace?: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
    creationTimestamp?: string;
    resourceVersion?: string;
  };

  /**
   * Get resource name
   */
  get name(): string {
    return this.metadata?.name || '';
  }

  /**
   * Get resource namespace
   */
  get namespace(): string {
    return this.metadata?.namespace || '';
  }

  /**
   * Get label value
   */
  getLabel(key: string): string {
    return this.metadata?.labels?.[key] || '';
  }

  /**
   * Get annotation value
   */
  getAnnotation(key: string): string {
    return this.metadata?.annotations?.[key] || '';
  }

  /**
   * Check if has label
   */
  hasLabel(key: string, value?: string): boolean {
    const labelValue = this.getLabel(key);
    if (!labelValue) return false;
    return value ? labelValue === value : true;
  }

  /**
   * Check if has annotation
   */
  hasAnnotation(key: string, value?: string): boolean {
    const annotationValue = this.getAnnotation(key);
    if (!annotationValue) return false;
    return value ? annotationValue === value : true;
  }

  /**
   * Get creation age in human readable format
   */
  get age(): string {
    if (!this.metadata?.creationTimestamp) return '';
    
    const created = new Date(this.metadata.creationTimestamp);
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
}

/**
 * Utility functions for common operations
 */
export class ResourceUtils {
  /**
   * Normalize name for comparison (lowercase, remove special chars)
   */
  static normalizeName(name?: string): string {
    return (name || '').toLowerCase().replace(/[^a-z0-9]+/g, '');
  }

  /**
   * Check if two names match (normalized comparison)
   */
  static namesMatch(name1?: string, name2?: string): boolean {
    return !!name1 && !!name2 && this.normalizeName(name1) === this.normalizeName(name2);
  }

  /**
   * Parse semver version
   */
  static parseVersion(version?: string): { major: number; minor: number; patch: number } | null {
    if (!version) return null;
    
    const match = version.match(/^(\d+)\.(\d+)\.(\d+)/);
    if (!match) return null;
    
    return {
      major: parseInt(match[1], 10),
      minor: parseInt(match[2], 10),
      patch: parseInt(match[3], 10)
    };
  }

  /**
   * Compare semver versions (-1: a < b, 0: a = b, 1: a > b)
   */
  static compareVersions(versionA?: string, versionB?: string): number {
    const a = this.parseVersion(versionA);
    const b = this.parseVersion(versionB);
    
    if (!a && !b) return 0;
    if (!a) return -1;
    if (!b) return 1;
    
    if (a.major !== b.major) return a.major - b.major;
    if (a.minor !== b.minor) return a.minor - b.minor;
    return a.patch - b.patch;
  }

  /**
   * Get latest version from array
   */
  static getLatestVersion(versions: string[]): string | undefined {
    if (!versions || versions.length === 0) return undefined;
    
    return versions
      .filter(v => this.parseVersion(v))
      .sort((a, b) => this.compareVersions(b, a))[0]; // Sort descending
  }

  /**
   * Format bytes to human readable
   */
  static formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  /**
   * Debounce function
   */
  static debounce<T extends (...args: any[]) => any>(
    func: T,
    wait: number
  ): (...args: Parameters<T>) => void {
    let timeout: NodeJS.Timeout;
    
    return (...args: Parameters<T>) => {
      clearTimeout(timeout);
      timeout = setTimeout(() => func(...args), wait);
    };
  }
}