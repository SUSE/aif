/**
 * App Collection Management - Handles collections of apps with filtering, sorting, and search
 * Provides centralized logic for managing app collections following domain-driven patterns
 */

import AppResource, { AppResourceData } from './app-resource';
import { ResourceUtils } from '../base/resource-mixin';

export type SortField = 'name' | 'updated' | 'status' | 'clusters';
export type SortDirection = 'asc' | 'desc';
export type PackagingFormat = 'HELM_CHART' | 'CONTAINER' | 'all';
export type AppStatus = 'available' | 'installed' | 'installing' | 'upgrading' | 'error' | 'all';

export interface CollectionFilters {
  search?: string;
  status?: AppStatus;
  packaging?: PackagingFormat;
  repository?: string;
  cluster?: string;
}

export interface SortOptions {
  field: SortField;
  direction: SortDirection;
}

/**
 * App Collection class for managing collections of AppResource instances
 */
export class AppCollection {
  private apps: AppResource[] = [];
  private store?: any;
  private router?: any;
  private route?: any;

  constructor(apps: AppResourceData[] = [], store?: any, router?: any, route?: any) {
    this.store = store;
    this.router = router;
    this.route = route;
    this.setApps(apps);
  }

  /**
   * Set apps in collection
   */
  setApps(apps: AppResourceData[]): void {
    this.apps = apps.map(appData => 
      new AppResource(appData, this.store, this.router, this.route)
    );
  }

  /**
   * Add app to collection
   */
  addApp(app: AppResourceData | AppResource): void {
    const appResource = app instanceof AppResource 
      ? app 
      : new AppResource(app, this.store, this.router, this.route);
    
    // Check if app already exists (by slug_name)
    const existingIndex = this.apps.findIndex(a => a.slug_name === appResource.slug_name);
    if (existingIndex >= 0) {
      this.apps[existingIndex] = appResource;
    } else {
      this.apps.push(appResource);
    }
  }

  /**
   * Remove app from collection
   */
  removeApp(slugName: string): void {
    this.apps = this.apps.filter(app => app.slug_name !== slugName);
  }

  /**
   * Get app by slug name
   */
  getApp(slugName: string): AppResource | undefined {
    return this.apps.find(app => app.slug_name === slugName);
  }

  /**
   * Get all apps
   */
  getAll(): AppResource[] {
    return [...this.apps];
  }

  /**
   * Get apps count
   */
  get count(): number {
    return this.apps.length;
  }

  /**
   * Check if collection is empty
   */
  get isEmpty(): boolean {
    return this.apps.length === 0;
  }

  // === Filtering Methods ===

  /**
   * Filter apps by search query
   */
  filterBySearch(query: string): AppResource[] {
    if (!query) return this.apps;
    return this.apps.filter(app => app.matchesSearch(query));
  }

  /**
   * Filter apps by status
   */
  filterByStatus(status: AppStatus): AppResource[] {
    if (status === 'all') return this.apps;
    
    return this.apps.filter(app => {
      switch (status) {
        case 'available':
          return !app.isInstalled;
        case 'installed':
          return app.isInstalled && !app.isInstalling && !app.isUpgrading;
        case 'installing':
          return app.isInstalling;
        case 'upgrading':
          return app.isUpgrading;
        case 'error':
          return app.hasFailed;
        default:
          return true;
      }
    });
  }

  /**
   * Filter apps by packaging format
   */
  filterByPackaging(format: PackagingFormat): AppResource[] {
    if (format === 'all') return this.apps;
    return this.apps.filter(app => app.packaging_format === format);
  }

  /**
   * Filter apps by repository
   */
  filterByRepository(repository: string): AppResource[] {
    if (!repository || repository === 'all') return this.apps;
    // This would need repository information added to AppResource
    // For now, return all apps
    return this.apps;
  }

  /**
   * Filter apps by cluster (apps installed on specific cluster)
   */
  filterByCluster(clusterId: string): AppResource[] {
    if (!clusterId || clusterId === 'all') return this.apps;
    return this.apps.filter(app => app.installationClusters.includes(clusterId));
  }

  /**
   * Apply multiple filters
   */
  filter(filters: CollectionFilters): AppResource[] {
    let filtered = this.apps;

    if (filters.search) {
      filtered = filtered.filter(app => app.matchesSearch(filters.search!));
    }

    if (filters.status && filters.status !== 'all') {
      filtered = this.filterByStatusFromArray(filtered, filters.status);
    }

    if (filters.packaging && filters.packaging !== 'all') {
      filtered = filtered.filter(app => app.packaging_format === filters.packaging);
    }

    if (filters.repository && filters.repository !== 'all') {
      // TODO: Repository filtering would need additional implementation
    }

    if (filters.cluster && filters.cluster !== 'all') {
      filtered = filtered.filter(app => app.installationClusters.includes(filters.cluster!));
    }

    return filtered;
  }

  /**
   * Helper to filter array by status
   */
  private filterByStatusFromArray(apps: AppResource[], status: AppStatus): AppResource[] {
    return apps.filter(app => {
      switch (status) {
        case 'available':
          return !app.isInstalled;
        case 'installed':
          return app.isInstalled && !app.isInstalling && !app.isUpgrading;
        case 'installing':
          return app.isInstalling;
        case 'upgrading':
          return app.isUpgrading;
        case 'error':
          return app.hasFailed;
        default:
          return true;
      }
    });
  }

  // === Sorting Methods ===

  /**
   * Sort apps by field and direction
   */
  sort(options: SortOptions): AppResource[] {
    const sorted = [...this.apps];
    
    sorted.sort((a, b) => {
      let comparison = 0;
      
      switch (options.field) {
        case 'name':
          comparison = a.name.localeCompare(b.name);
          break;
        case 'updated':
          comparison = this.compareDates(a.last_updated_at, b.last_updated_at);
          break;
        case 'status':
          comparison = a.stateDisplay.localeCompare(b.stateDisplay);
          break;
        case 'clusters':
          comparison = a.installationClusters.length - b.installationClusters.length;
          break;
        default:
          return 0;
      }
      
      return options.direction === 'desc' ? -comparison : comparison;
    });
    
    return sorted;
  }

  /**
   * Sort and filter apps
   */
  sortAndFilter(filters: CollectionFilters, sort: SortOptions): AppResource[] {
    const filtered = this.filter(filters);
    return this.sortArray(filtered, sort);
  }

  /**
   * Sort array of apps
   */
  private sortArray(apps: AppResource[], options: SortOptions): AppResource[] {
    const sorted = [...apps];
    
    sorted.sort((a, b) => {
      let comparison = 0;
      
      switch (options.field) {
        case 'name':
          comparison = a.name.localeCompare(b.name);
          break;
        case 'updated':
          comparison = this.compareDates(a.last_updated_at, b.last_updated_at);
          break;
        case 'status':
          comparison = a.stateDisplay.localeCompare(b.stateDisplay);
          break;
        case 'clusters':
          comparison = a.installationClusters.length - b.installationClusters.length;
          break;
        default:
          return 0;
      }
      
      return options.direction === 'desc' ? -comparison : comparison;
    });
    
    return sorted;
  }

  /**
   * Compare dates for sorting
   */
  private compareDates(dateA?: string, dateB?: string): number {
    if (!dateA && !dateB) return 0;
    if (!dateA) return -1;
    if (!dateB) return 1;
    
    const timeA = new Date(dateA).getTime();
    const timeB = new Date(dateB).getTime();
    return timeA - timeB;
  }

  // === Statistics Methods ===

  /**
   * Get collection statistics
   */
  getStats() {
    const stats = {
      total: this.apps.length,
      installed: 0,
      available: 0,
      installing: 0,
      upgrading: 0,
      error: 0,
      helmCharts: 0,
      containers: 0,
      clustersWithApps: new Set<string>()
    };

    this.apps.forEach(app => {
      if (app.isInstalled) stats.installed++;
      else stats.available++;
      
      if (app.isInstalling) stats.installing++;
      if (app.isUpgrading) stats.upgrading++;
      if (app.hasFailed) stats.error++;
      
      if (app.packaging_format === 'HELM_CHART') stats.helmCharts++;
      else if (app.packaging_format === 'CONTAINER') stats.containers++;
      
      app.installationClusters.forEach(cluster => {
        stats.clustersWithApps.add(cluster);
      });
    });

    return {
      ...stats,
      clustersWithApps: stats.clustersWithApps.size
    };
  }

  /**
   * Get apps by status counts
   */
  getStatusCounts(): Record<AppStatus | 'all', number> {
    const stats = this.getStats();
    
    return {
      all: stats.total,
      available: stats.available,
      installed: stats.installed,
      installing: stats.installing,
      upgrading: stats.upgrading,
      error: stats.error
    };
  }

  /**
   * Get apps by packaging format counts
   */
  getPackagingCounts(): Record<PackagingFormat, number> {
    const stats = this.getStats();
    
    return {
      all: stats.total,
      HELM_CHART: stats.helmCharts,
      CONTAINER: stats.containers
    };
  }

  // === Bulk Operations ===

  /**
   * Get apps that can be installed
   */
  getInstallableApps(): AppResource[] {
    return this.apps.filter(app => app.canInstall);
  }

  /**
   * Get apps that can be upgraded
   */
  getUpgradableApps(): AppResource[] {
    return this.apps.filter(app => app.canUpgrade);
  }

  /**
   * Get apps that can be uninstalled
   */
  getUninstallableApps(): AppResource[] {
    return this.apps.filter(app => app.canUninstall);
  }

  /**
   * Perform bulk action on multiple apps
   */
  async performBulkAction(action: string, appSlugs: string[], options: any = {}): Promise<void> {
    const apps = appSlugs
      .map(slug => this.getApp(slug))
      .filter((app): app is AppResource => !!app);
    
    const promises = apps.map(app => {
      if (app.canAction(action)) {
        return app.performAction(action, options);
      }
      return Promise.resolve();
    });
    
    await Promise.all(promises);
  }

  // === Search and Discovery ===

  /**
   * Search apps with advanced options
   */
  search(query: string, options: { fields?: string[] } = {}): AppResource[] {
    if (!query) return this.apps;
    
    const searchFields = options.fields || ['name', 'description', 'slug_name'];
    const searchText = query.toLowerCase();
    
    return this.apps.filter(app => {
      return searchFields.some(field => {
        const value = (app as any)[field];
        return value && String(value).toLowerCase().includes(searchText);
      });
    });
  }

  /**
   * Find apps similar to given app (by description, packaging format, etc.)
   */
  findSimilar(app: AppResource, limit = 5): AppResource[] {
    const similar = this.apps
      .filter(a => a.slug_name !== app.slug_name)
      .map(a => ({
        app: a,
        score: this.calculateSimilarityScore(app, a)
      }))
      .filter(({ score }) => score > 0)
      .sort((a, b) => b.score - a.score)
      .slice(0, limit)
      .map(({ app: a }) => a);
    
    return similar;
  }

  /**
   * Calculate similarity score between two apps
   */
  private calculateSimilarityScore(appA: AppResource, appB: AppResource): number {
    let score = 0;
    
    // Same packaging format
    if (appA.packaging_format === appB.packaging_format) {
      score += 2;
    }
    
    // Similar descriptions (basic keyword matching)
    if (appA.description && appB.description) {
      const wordsA = appA.description.toLowerCase().split(/\s+/);
      const wordsB = appB.description.toLowerCase().split(/\s+/);
      const commonWords = wordsA.filter(word => 
        word.length > 3 && wordsB.includes(word)
      );
      score += commonWords.length;
    }
    
    return score;
  }

  /**
   * Update installation info for all apps
   */
  async refreshInstallations(): Promise<void> {
    if (!this.store) return;
    
    // This would dispatch to store to refresh installation data
    // Implementation depends on store setup
    await this.store.dispatch('suseai/discoverInstallations');
  }

  /**
   * Convert collection to plain objects
   */
  toJSON(): AppResourceData[] {
    return this.apps.map(app => app.toJSON());
  }
}