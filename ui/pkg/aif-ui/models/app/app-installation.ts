/**
 * App Installation State Tracking - Manages installation state across clusters
 * Provides detailed tracking of app installations with status, progress, and history
 */

import { InstallationInfo } from '../base/resource-mixin';

export type InstallationPhase = 
  | 'pending' 
  | 'installing' 
  | 'deployed' 
  | 'upgrading' 
  | 'uninstalling' 
  | 'failed' 
  | 'superseded' 
  | 'unknown';

export interface InstallationEvent {
  timestamp: string;
  phase: InstallationPhase;
  message: string;
  type: 'info' | 'warning' | 'error';
}

export interface InstallationResource {
  kind: string;
  name: string;
  namespace: string;
  ready: boolean;
  status: string;
  message?: string;
}

export interface InstallationProgress {
  phase: InstallationPhase;
  progress: number; // 0-100
  message: string;
  startedAt?: string;
  completedAt?: string;
  estimatedDuration?: number; // in seconds
}

export interface InstallationDetails extends InstallationInfo {
  // Chart information
  chartRepo: string;
  chartName: string;
  chartVersion: string;
  appVersion?: string;
  
  // Values and configuration
  values: Record<string, any>;
  userValues: Record<string, any>; // User-provided values only
  
  // Installation metadata  
  installedBy?: string;
  installationNotes?: string;
  
  // Progress tracking
  progress?: InstallationProgress;
  events: InstallationEvent[];
  resources: InstallationResource[];
  
  // Error details
  error?: {
    code: string;
    message: string;
    details?: any;
    retryable: boolean;
  };
  
  // Timestamps
  createdAt: string;
  updatedAt: string;
  lastHealthCheck?: string;
}

/**
 * App Installation class for tracking individual installations
 */
export class AppInstallation {
  public details: InstallationDetails;
  private store?: any;
  
  constructor(details: InstallationDetails, store?: any) {
    this.details = details;
    this.store = store;
  }

  // === Basic Properties ===
  
  get clusterId(): string {
    return this.details.clusterId;
  }
  
  get namespace(): string {
    return this.details.namespace;
  }
  
  get releaseName(): string {
    return this.details.releaseName;
  }
  
  get status(): InstallationPhase {
    return this.details.status as InstallationPhase;
  }
  
  get version(): string {
    return this.details.version || this.details.chartVersion;
  }
  
  get appVersion(): string {
    return this.details.appVersion || this.details.version || '';
  }

  // === State Checks ===
  
  get isDeployed(): boolean {
    return this.details.status === 'deployed';
  }
  
  get isFailed(): boolean {
    return this.details.status === 'failed';
  }
  
  get isInstalling(): boolean {
    return this.details.status === 'installing';
  }
  
  get isUpgrading(): boolean {
    return this.details.status === 'upgrading';
  }
  
  get isUninstalling(): boolean {
    return this.details.status === 'uninstalling';
  }
  
  get isPending(): boolean {
    return this.details.status === 'pending';
  }
  
  get isHealthy(): boolean {
    return this.isDeployed && this.details.resources.every(r => r.ready);
  }
  
  get hasError(): boolean {
    return !!this.details.error || this.isFailed;
  }
  
  get isTransitioning(): boolean {
    return this.isInstalling || this.isUpgrading || this.isUninstalling || this.isPending;
  }

  // === Display Properties ===
  
  get stateDisplay(): string {
    const status = this.details.status;
    
    switch (status) {
      case 'pending':
        return 'Pending';
      case 'installing':
        return 'Installing';
      case 'deployed':
        return this.isHealthy ? 'Running' : 'Deployed';
      case 'upgrading':
        return 'Upgrading';
      case 'uninstalling':
        return 'Uninstalling';
      case 'failed':
        return 'Failed';
      case 'superseded':
        return 'Superseded';
      default:
        return 'Unknown';
    }
  }
  
  get stateColor(): string {
    if (this.isFailed || this.hasError) return 'error';
    if (this.isTransitioning) return 'info';
    if (this.isDeployed) return this.isHealthy ? 'success' : 'warning';
    return 'inactive';
  }
  
  get progressDisplay(): string {
    const progress = this.details.progress;
    if (!progress) return '';
    
    return `${progress.progress}%`;
  }
  
  get statusMessage(): string {
    if (this.details.error) {
      return this.details.error.message;
    }
    
    if (this.details.progress) {
      return this.details.progress.message;
    }
    
    if (this.details.lastDeployed) {
      const date = new Date(this.details.lastDeployed);
      return `Last deployed: ${date.toLocaleString()}`;
    }
    
    return '';
  }

  // === Progress Tracking ===
  
  get currentProgress(): number {
    return this.details.progress?.progress || 0;
  }
  
  get estimatedTimeRemaining(): number | null {
    const progress = this.details.progress;
    if (!progress || !progress.estimatedDuration || !progress.startedAt) {
      return null;
    }
    
    const elapsed = Date.now() - new Date(progress.startedAt).getTime();
    const elapsedSeconds = elapsed / 1000;
    const progressRatio = progress.progress / 100;
    
    if (progressRatio <= 0) return null;
    
    const totalEstimated = progress.estimatedDuration;
    const remaining = totalEstimated - elapsedSeconds;
    
    return Math.max(0, remaining);
  }
  
  get installDuration(): number | null {
    const progress = this.details.progress;
    if (!progress || !progress.startedAt) return null;
    
    const endTime = progress.completedAt 
      ? new Date(progress.completedAt).getTime()
      : Date.now();
      
    const startTime = new Date(progress.startedAt).getTime();
    return (endTime - startTime) / 1000; // in seconds
  }

  // === Resource Management ===
  
  get resourceCount(): number {
    return this.details.resources.length;
  }
  
  get readyResourceCount(): number {
    return this.details.resources.filter(r => r.ready).length;
  }
  
  get resourcesReady(): boolean {
    return this.resourceCount > 0 && this.readyResourceCount === this.resourceCount;
  }
  
  getResourcesByKind(kind: string): InstallationResource[] {
    return this.details.resources.filter(r => r.kind === kind);
  }
  
  getUnhealthyResources(): InstallationResource[] {
    return this.details.resources.filter(r => !r.ready);
  }

  // === Event Management ===
  
  get latestEvent(): InstallationEvent | undefined {
    if (this.details.events.length === 0) return undefined;
    
    return this.details.events.sort(
      (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
    )[0];
  }
  
  get errorEvents(): InstallationEvent[] {
    return this.details.events.filter(e => e.type === 'error');
  }
  
  get warningEvents(): InstallationEvent[] {
    return this.details.events.filter(e => e.type === 'warning');
  }
  
  addEvent(event: Omit<InstallationEvent, 'timestamp'>): void {
    this.details.events.push({
      ...event,
      timestamp: new Date().toISOString()
    });
    
    this.details.updatedAt = new Date().toISOString();
  }

  // === Configuration Management ===
  
  get hasUserValues(): boolean {
    return Object.keys(this.details.userValues).length > 0;
  }
  
  get valuesChanged(): boolean {
    // Compare current values with user values to detect changes
    return JSON.stringify(this.details.values) !== JSON.stringify(this.details.userValues);
  }
  
  updateUserValues(newValues: Record<string, any>): void {
    this.details.userValues = { ...newValues };
    this.details.updatedAt = new Date().toISOString();
  }
  
  mergeValues(defaultValues: Record<string, any>): Record<string, any> {
    return {
      ...defaultValues,
      ...this.details.userValues
    };
  }

  // === Health and Monitoring ===
  
  get needsHealthCheck(): boolean {
    if (!this.details.lastHealthCheck) return true;
    
    const lastCheck = new Date(this.details.lastHealthCheck).getTime();
    const fiveMinutesAgo = Date.now() - (5 * 60 * 1000);
    
    return lastCheck < fiveMinutesAgo;
  }
  
  updateHealth(resources: InstallationResource[]): void {
    this.details.resources = resources;
    this.details.lastHealthCheck = new Date().toISOString();
    this.details.updatedAt = new Date().toISOString();
    
    // Add health event if status changed
    const wasHealthy = this.isHealthy;
    const isHealthyNow = resources.every(r => r.ready);
    
    if (wasHealthy !== isHealthyNow) {
      this.addEvent({
        phase: this.status,
        message: isHealthyNow ? 'All resources are healthy' : 'Some resources are not ready',
        type: isHealthyNow ? 'info' : 'warning'
      });
    }
  }

  // === Actions ===
  
  async refresh(): Promise<void> {
    if (!this.store) return;
    
    try {
      const updated = await this.store.dispatch('suseai/refreshInstallation', {
        clusterId: this.clusterId,
        namespace: this.namespace,
        releaseName: this.releaseName
      });
      
      if (updated) {
        Object.assign(this.details, updated);
      }
    } catch (error) {
      console.warn('Failed to refresh installation:', error);
    }
  }
  
  async upgrade(chartVersion: string, values?: Record<string, any>): Promise<void> {
    if (!this.store) {
      throw new Error('Store not available for upgrade operation');
    }
    
    await this.store.dispatch('suseai/upgradeInstallation', {
      clusterId: this.clusterId,
      namespace: this.namespace,
      releaseName: this.releaseName,
      chartVersion,
      values: values || this.details.userValues
    });
  }
  
  async uninstall(): Promise<void> {
    if (!this.store) {
      throw new Error('Store not available for uninstall operation');
    }
    
    await this.store.dispatch('suseai/uninstallApp', {
      clusterId: this.clusterId,
      namespace: this.namespace,
      releaseName: this.releaseName
    });
  }
  
  async rollback(revision?: number): Promise<void> {
    if (!this.store) {
      throw new Error('Store not available for rollback operation');
    }
    
    await this.store.dispatch('suseai/rollbackInstallation', {
      clusterId: this.clusterId,
      namespace: this.namespace,
      releaseName: this.releaseName,
      revision
    });
  }

  // === Utility Methods ===
  
  toJSON(): InstallationDetails {
    return { ...this.details };
  }
  
  clone(): AppInstallation {
    return new AppInstallation({ ...this.details }, this.store);
  }
  
  equals(other: AppInstallation): boolean {
    return (
      this.clusterId === other.clusterId &&
      this.namespace === other.namespace &&
      this.releaseName === other.releaseName
    );
  }
  
  toString(): string {
    return `${this.clusterId}/${this.namespace}/${this.releaseName}`;
  }
}