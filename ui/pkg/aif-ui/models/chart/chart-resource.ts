/**
 * Chart Resource Model - Manages chart metadata, versions, and operations
 * Provides chart-specific functionality following Rancher UI patterns
 */

import SuseaiResource, { Action, ActionOpts } from '../base/suseai-resource';
import { StateMixin, MetadataMixin, ResourceUtils } from '../base/resource-mixin';

export interface ChartMaintainer {
  name: string;
  email?: string;
  url?: string;
}

export interface ChartKeyword {
  name: string;
  category?: string;
}

export interface ChartVersion {
  version: string;
  appVersion?: string;
  description?: string;
  created: string;
  digest?: string;
  urls: string[];
  deprecated?: boolean;
  maintainers?: ChartMaintainer[];
  sources?: string[];
  dependencies?: ChartDependency[];
  keywords?: string[];
  annotations?: Record<string, string>;
}

export interface ChartDependency {
  name: string;
  version: string;
  repository?: string;
  condition?: string;
  tags?: string[];
  enabled?: boolean;
  alias?: string;
}

export interface ChartStats {
  downloadCount: number;
  installCount: number;
  lastDownloaded?: string;
  popularityScore: number;
  averageRating?: number;
  ratingCount?: number;
}

export interface ChartCompatibility {
  kubernetesVersions: string[];
  helmVersions: string[];
  architectures: string[];
  operatingSystems: string[];
}

export interface ChartResourceData {
  name: string;
  displayName?: string;
  description?: string;
  home?: string;
  icon?: string;
  
  // Repository info
  repoName: string;
  repoUrl: string;
  repoType: 'helm' | 'oci' | 'git';
  
  // Chart metadata
  category?: string;
  keywords: string[];
  maintainers: ChartMaintainer[];
  sources: string[];
  
  // Versions
  versions: ChartVersion[];
  latestVersion?: string;
  recommendedVersion?: string;
  
  // Status
  deprecated?: boolean;
  verified?: boolean;
  official?: boolean;
  
  // Statistics
  stats?: ChartStats;
  compatibility?: ChartCompatibility;
  
  // Metadata from chart repository
  annotations?: Record<string, string>;
  labels?: Record<string, string>;
  
  // Timestamps
  created?: string;
  updated?: string;
}

/**
 * Chart Resource class with chart-specific operations
 */
export default class ChartResource extends SuseaiResource {
  // Chart-specific properties
  public chartName!: string;
  public displayName?: string;
  public description?: string;
  public home?: string;
  public icon?: string;
  
  // Repository info
  public repoName!: string;
  public repoUrl!: string;
  public repoType!: 'helm' | 'oci' | 'git';
  
  // Chart metadata
  public category?: string;
  public keywords!: string[];
  public maintainers!: ChartMaintainer[];
  public sources!: string[];
  
  // Versions
  public versions!: ChartVersion[];
  public latestVersion?: string;
  public recommendedVersion?: string;
  
  // Status
  public deprecated?: boolean;
  public verified?: boolean;
  public official?: boolean;
  
  // Statistics
  public stats?: ChartStats;
  public compatibility?: ChartCompatibility;
  
  // Metadata
  public chartAnnotations?: Record<string, string>;
  public chartLabels?: Record<string, string>;
  
  // Timestamps
  public created?: string;
  public updated?: string;

  // Mixins
  private stateMixin: StateMixin;
  private metadataMixin: MetadataMixin;

  constructor(data: ChartResourceData, store?: any, router?: any, route?: any) {
    super(data, store, router, route);
    
    // Map data properties
    this.chartName = data.name;
    Object.assign(this, data);
    
    // Rename conflicting properties
    this.chartAnnotations = data.annotations;
    this.chartLabels = data.labels;
    
    // Initialize mixins
    this.stateMixin = new StateMixin();
    this.metadataMixin = new MetadataMixin();
    
    // Set up metadata for mixin
    this.metadataMixin.metadata = {
      name: data.name,
      annotations: data.annotations,
      labels: data.labels,
      creationTimestamp: data.created
    };
  }

  // === Basic Properties ===

  get id(): string {
    return `${this.repoName}/${this.chartName}`;
  }

  get name(): string {
    return this.displayName || this.chartName;
  }

  get shortName(): string {
    return this.chartName;
  }

  get fullName(): string {
    return `${this.repoName}/${this.chartName}`;
  }

  // === Version Management ===

  get hasVersions(): boolean {
    return this.versions.length > 0;
  }

  get versionCount(): number {
    return this.versions.length;
  }

  get currentVersion(): string {
    return this.recommendedVersion || this.latestVersion || this.getLatestNonDeprecatedVersion() || '';
  }

  get availableVersions(): string[] {
    return this.versions.map(v => v.version);
  }

  get stableVersions(): ChartVersion[] {
    return this.versions.filter(v => !v.version.includes('-') && !v.deprecated);
  }

  get preReleaseVersions(): ChartVersion[] {
    return this.versions.filter(v => v.version.includes('-'));
  }

  get deprecatedVersions(): ChartVersion[] {
    return this.versions.filter(v => v.deprecated);
  }

  getLatestNonDeprecatedVersion(): string {
    const nonDeprecated = this.versions.filter(v => !v.deprecated);
    if (nonDeprecated.length === 0) return '';
    
    return ResourceUtils.getLatestVersion(nonDeprecated.map(v => v.version)) || '';
  }

  getVersion(version: string): ChartVersion | undefined {
    return this.versions.find(v => v.version === version);
  }

  getLatestVersion(): ChartVersion | undefined {
    if (this.versions.length === 0) return undefined;
    
    const latestVersionString = ResourceUtils.getLatestVersion(
      this.versions.map(v => v.version)
    );
    
    return this.getVersion(latestVersionString || '');
  }

  // === State Management ===

  get stateDisplay(): string {
    if (this.deprecated) return 'Deprecated';
    if (!this.hasVersions) return 'No Versions';
    if (this.verified) return 'Verified';
    if (this.official) return 'Official';
    return 'Available';
  }

  get stateColor(): string {
    if (this.deprecated) return 'warning';
    if (!this.hasVersions) return 'inactive';
    if (this.verified || this.official) return 'success';
    return 'info';
  }

  get isAvailable(): boolean {
    return this.hasVersions && !this.deprecated;
  }

  get isRecommended(): boolean {
    return this.verified || this.official || (this.stats?.popularityScore || 0) > 75;
  }

  // === Metadata Properties ===

  get categoryDisplay(): string {
    return this.category || 'Uncategorized';
  }

  get keywordDisplay(): string {
    return this.keywords.join(', ');
  }

  get maintainerDisplay(): string {
    return this.maintainers.map(m => m.name).join(', ');
  }

  get hasMaintainers(): boolean {
    return this.maintainers.length > 0;
  }

  get hasKeywords(): boolean {
    return this.keywords.length > 0;
  }

  get hasSources(): boolean {
    return this.sources.length > 0;
  }

  // === Statistics ===

  get downloadCount(): number {
    return this.stats?.downloadCount || 0;
  }

  get installCount(): number {
    return this.stats?.installCount || 0;
  }

  get popularityScore(): number {
    return this.stats?.popularityScore || 0;
  }

  get averageRating(): number {
    return this.stats?.averageRating || 0;
  }

  get ratingCount(): number {
    return this.stats?.ratingCount || 0;
  }

  get isPopular(): boolean {
    return this.popularityScore > 50 || this.downloadCount > 1000;
  }

  get popularityDisplay(): string {
    const score = this.popularityScore;
    if (score >= 90) return 'Very Popular';
    if (score >= 70) return 'Popular';
    if (score >= 40) return 'Moderate';
    if (score >= 20) return 'Growing';
    return 'New';
  }

  // === Compatibility ===

  get supportedKubernetesVersions(): string[] {
    return this.compatibility?.kubernetesVersions || [];
  }

  get supportedHelmVersions(): string[] {
    return this.compatibility?.helmVersions || [];
  }

  get supportedArchitectures(): string[] {
    return this.compatibility?.architectures || [];
  }

  supportsKubernetesVersion(version: string): boolean {
    if (!this.compatibility?.kubernetesVersions.length) return true; // Assume compatible if not specified
    return this.compatibility.kubernetesVersions.some(supported => 
      this.versionMatches(version, supported)
    );
  }

  supportsHelmVersion(version: string): boolean {
    if (!this.compatibility?.helmVersions.length) return true; // Assume compatible if not specified
    return this.compatibility.helmVersions.some(supported =>
      this.versionMatches(version, supported)
    );
  }

  private versionMatches(version: string, pattern: string): boolean {
    // Simple version matching - can be enhanced for semantic versioning
    if (pattern.includes('*')) {
      const regex = new RegExp(pattern.replace(/\*/g, '.*'));
      return regex.test(version);
    }
    return version === pattern;
  }

  // === Dependencies ===

  get hasDependencies(): boolean {
    const latestVersion = this.getLatestVersion();
    return (latestVersion?.dependencies?.length || 0) > 0;
  }

  getDependencies(version?: string): ChartDependency[] {
    const chartVersion = version ? this.getVersion(version) : this.getLatestVersion();
    return chartVersion?.dependencies || [];
  }

  getDependencyNames(version?: string): string[] {
    return this.getDependencies(version).map(dep => dep.name);
  }

  // === Actions ===

  get availableActions(): Action[] {
    const actions: Action[] = [];

    if (this.isAvailable) {
      actions.push(INSTALL_CHART_ACTION);
      actions.push(VIEW_CHART_ACTION);
    }

    if (this.hasVersions) {
      actions.push(VIEW_VERSIONS_ACTION);
    }

    if (this.home) {
      actions.push(VIEW_HOMEPAGE_ACTION);
    }

    if (this.hasSources) {
      actions.push(VIEW_SOURCE_ACTION);
    }

    actions.push(COPY_INSTALL_COMMAND_ACTION);

    return actions;
  }

  // === Chart Operations ===

  /**
   * Navigate to chart installation page
   */
  async install(): Promise<void> {
    await this.$push({
      name: 'c-cluster-suseai-install',
      params: { 
        product: 'suseai',
        cluster: this.$route?.params?.cluster || 'local'
      },
      query: { 
        chart: this.chartName,
        repo: this.repoName,
        version: this.currentVersion
      }
    });
  }

  /**
   * Navigate to chart details page
   */
  async viewDetails(): Promise<void> {
    await this.$push({
      name: 'c-cluster-suseai-chart-detail',
      params: {
        product: 'suseai',
        cluster: this.$route?.params?.cluster || 'local',
        chart: this.id
      }
    });
  }

  /**
   * Navigate to chart versions page
   */
  async viewVersions(): Promise<void> {
    await this.$push({
      name: 'c-cluster-suseai-chart-versions',
      params: {
        product: 'suseai',
        cluster: this.$route?.params?.cluster || 'local',
        chart: this.id
      }
    });
  }

  /**
   * Open chart homepage in new tab
   */
  async viewHomepage(): Promise<void> {
    if (this.home) {
      window.open(this.home, '_blank', 'noopener,noreferrer');
    }
  }

  /**
   * Open chart source in new tab
   */
  async viewSource(): Promise<void> {
    const source = this.sources[0];
    if (source) {
      window.open(source, '_blank', 'noopener,noreferrer');
    }
  }

  /**
   * Copy helm install command to clipboard
   */
  async copyInstallCommand(): Promise<void> {
    const command = `helm install ${this.chartName} ${this.repoName}/${this.chartName} --version ${this.currentVersion}`;
    
    try {
      await navigator.clipboard.writeText(command);
      // Show success notification
      if (this.$store) {
        this.$store.dispatch('growl/success', {
          title: 'Copied to Clipboard',
          message: 'Helm install command copied to clipboard'
        });
      }
    } catch (error) {
      console.warn('Failed to copy to clipboard:', error);
      // Fallback: show the command in a modal or alert
      alert(`Copy this command:\n${command}`);
    }
  }

  /**
   * Refresh chart metadata from repository
   */
  async refresh(): Promise<void> {
    if (!this.$store) return;
    
    try {
      const updated = await this.$store.dispatch('suseai/refreshChart', {
        repoName: this.repoName,
        chartName: this.chartName
      });
      
      if (updated) {
        Object.assign(this, updated);
      }
    } catch (error) {
      console.warn('Failed to refresh chart:', error);
    }
  }

  /**
   * Get chart values for specific version
   */
  async getValues(version?: string): Promise<Record<string, any>> {
    if (!this.$store) return {};
    
    try {
      return await this.$store.dispatch('suseai/getChartValues', {
        repoName: this.repoName,
        chartName: this.chartName,
        version: version || this.currentVersion
      });
    } catch (error) {
      console.warn('Failed to get chart values:', error);
      return {};
    }
  }

  /**
   * Get chart readme
   */
  async getReadme(version?: string): Promise<string> {
    if (!this.$store) return '';
    
    try {
      return await this.$store.dispatch('suseai/getChartReadme', {
        repoName: this.repoName,
        chartName: this.chartName,
        version: version || this.currentVersion
      });
    } catch (error) {
      console.warn('Failed to get chart readme:', error);
      return '';
    }
  }

  // === Search and Filtering ===

  /**
   * Check if chart matches search query
   */
  matchesSearch(query: string): boolean {
    if (!query) return true;
    
    const searchText = query.toLowerCase();
    return (
      this.chartName.toLowerCase().includes(searchText) ||
      (this.displayName || '').toLowerCase().includes(searchText) ||
      (this.description || '').toLowerCase().includes(searchText) ||
      this.keywords.some(k => k.toLowerCase().includes(searchText)) ||
      this.maintainers.some(m => m.name.toLowerCase().includes(searchText))
    );
  }

  /**
   * Check if chart matches category filter
   */
  matchesCategory(category: string): boolean {
    if (!category || category === 'all') return true;
    return this.category === category;
  }

  /**
   * Check if chart matches keyword filter
   */
  matchesKeyword(keyword: string): boolean {
    if (!keyword) return true;
    return this.keywords.includes(keyword);
  }

  // === Utility Methods ===

  /**
   * Compare charts for sorting
   */
  compareTo(other: ChartResource): number {
    // Official and verified charts first
    if (this.official && !other.official) return -1;
    if (!this.official && other.official) return 1;
    if (this.verified && !other.verified) return -1;
    if (!this.verified && other.verified) return 1;
    
    // Then by popularity score
    const scoreDiff = (other.popularityScore || 0) - (this.popularityScore || 0);
    if (scoreDiff !== 0) return scoreDiff;
    
    // Finally by name
    return this.chartName.localeCompare(other.chartName);
  }

  /**
   * Get age since last update
   */
  get age(): string {
    if (!this.updated) return 'Unknown';
    
    const updatedDate = new Date(this.updated);
    const now = new Date();
    const diffMs = now.getTime() - updatedDate.getTime();
    
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
    const diffMonths = Math.floor(diffDays / 30);
    const diffYears = Math.floor(diffDays / 365);
    
    if (diffYears > 0) return `${diffYears}y`;
    if (diffMonths > 0) return `${diffMonths}mo`;
    if (diffDays > 0) return `${diffDays}d`;
    return '< 1d';
  }

  /**
   * Update chart statistics
   */
  updateStats(stats: Partial<ChartStats>): void {
    this.stats = {
      downloadCount: 0,
      installCount: 0,
      popularityScore: 0,
      ...this.stats,
      ...stats
    };
  }

  /**
   * Convert to plain object
   */
  toJSON(): ChartResourceData {
    return {
      name: this.chartName,
      displayName: this.displayName,
      description: this.description,
      home: this.home,
      icon: this.icon,
      repoName: this.repoName,
      repoUrl: this.repoUrl,
      repoType: this.repoType,
      category: this.category,
      keywords: this.keywords,
      maintainers: this.maintainers,
      sources: this.sources,
      versions: this.versions,
      latestVersion: this.latestVersion,
      recommendedVersion: this.recommendedVersion,
      deprecated: this.deprecated,
      verified: this.verified,
      official: this.official,
      stats: this.stats,
      compatibility: this.compatibility,
      annotations: this.chartAnnotations,
      labels: this.chartLabels,
      created: this.created,
      updated: this.updated
    };
  }
}

// === Action Definitions ===

const INSTALL_CHART_ACTION: Action = {
  action: 'install',
  label: 'Install Chart',
  icon: 'icon-plus',
  invoke: async (opts: ActionOpts) => {
    const chart = opts.resource as ChartResource;
    await chart.install();
  }
};

const VIEW_CHART_ACTION: Action = {
  action: 'viewDetails',
  label: 'View Details',
  icon: 'icon-info',
  invoke: async (opts: ActionOpts) => {
    const chart = opts.resource as ChartResource;
    await chart.viewDetails();
  }
};

const VIEW_VERSIONS_ACTION: Action = {
  action: 'viewVersions',
  label: 'View Versions',
  icon: 'icon-history',
  invoke: async (opts: ActionOpts) => {
    const chart = opts.resource as ChartResource;
    await chart.viewVersions();
  }
};

const VIEW_HOMEPAGE_ACTION: Action = {
  action: 'viewHomepage',
  label: 'View Homepage',
  icon: 'icon-external-link',
  invoke: async (opts: ActionOpts) => {
    const chart = opts.resource as ChartResource;
    await chart.viewHomepage();
  }
};

const VIEW_SOURCE_ACTION: Action = {
  action: 'viewSource',
  label: 'View Source',
  icon: 'icon-github',
  invoke: async (opts: ActionOpts) => {
    const chart = opts.resource as ChartResource;
    await chart.viewSource();
  }
};

const COPY_INSTALL_COMMAND_ACTION: Action = {
  action: 'copyInstallCommand',
  label: 'Copy Install Command',
  icon: 'icon-copy',
  invoke: async (opts: ActionOpts) => {
    const chart = opts.resource as ChartResource;
    await chart.copyInstallCommand();
  }
};