/**
 * ResourceTable Headers Configuration
 * Following Rancher UI conventions for table configuration
 * Defines column configurations for ResourceTable components
 */

// === Column Types ===
export type ColumnType = 'name' | 'badge' | 'age' | 'state' | 'actions' | 'custom';

export interface TableColumn {
  name: string;
  labelKey: string;
  value?: string;
  getValue?: (row: any) => any;
  sort?: string[] | string | boolean;
  search?: string[] | string | boolean;
  width?: number;
  minWidth?: number;
  maxWidth?: number;
  align?: 'left' | 'center' | 'right';
  formatter?: string;
  formatterOpts?: Record<string, any>;
  dashIfEmpty?: boolean;
  canBeVariable?: boolean;
  type?: ColumnType;
  priority?: number;
  breakpoint?: 'mobile' | 'tablet' | 'desktop';
}

// === Apps Table Headers ===
export const APPS_TABLE_HEADERS: TableColumn[] = [
  {
    name: 'name',
    labelKey: 'tableHeaders.name',
    value: 'name',
    sort: ['name', 'displayName'],
    search: ['name', 'displayName', 'description'],
    type: 'name',
    priority: 1
  },
  {
    name: 'status',
    labelKey: 'tableHeaders.status',
    value: 'status',
    sort: 'status',
    width: 120,
    type: 'badge',
    priority: 2,
    formatter: 'AppStatus'
  },
  {
    name: 'version',
    labelKey: 'tableHeaders.version',
    value: 'version',
    sort: 'version',
    width: 100,
    priority: 3
  },
  {
    name: 'clusters',
    labelKey: 'tableHeaders.clusters',
    getValue: (row: any) => row.installationCount || 0,
    sort: false,
    width: 80,
    priority: 4,
    formatter: 'Number',
    align: 'center'
  },
  {
    name: 'category',
    labelKey: 'tableHeaders.category',
    value: 'category',
    sort: 'category',
    width: 120,
    priority: 5,
    breakpoint: 'tablet'
  },
  {
    name: 'repository',
    labelKey: 'tableHeaders.repository',
    value: 'repository',
    sort: 'repository',
    width: 150,
    priority: 6,
    breakpoint: 'desktop'
  },
  {
    name: 'updated',
    labelKey: 'tableHeaders.updated',
    value: 'lastUpdated',
    sort: 'lastUpdated',
    width: 120,
    type: 'age',
    priority: 7,
    breakpoint: 'desktop'
  },
  {
    name: 'actions',
    labelKey: 'tableHeaders.actions',
    value: '',
    sort: false,
    search: false,
    width: 100,
    type: 'actions',
    priority: 999,
    align: 'right'
  }
];

// === Installations Table Headers ===
export const INSTALLATIONS_TABLE_HEADERS: TableColumn[] = [
  {
    name: 'name',
    labelKey: 'tableHeaders.name',
    value: 'releaseName',
    sort: ['releaseName', 'appName'],
    search: ['releaseName', 'appName'],
    type: 'name',
    priority: 1
  },
  {
    name: 'status',
    labelKey: 'tableHeaders.status',
    value: 'status',
    sort: 'status',
    width: 120,
    type: 'badge',
    priority: 2,
    formatter: 'InstallationStatus'
  },
  {
    name: 'app',
    labelKey: 'tableHeaders.app',
    value: 'appName',
    sort: 'appName',
    search: 'appName',
    width: 150,
    priority: 3
  },
  {
    name: 'version',
    labelKey: 'tableHeaders.version',
    value: 'chartVersion',
    sort: 'chartVersion',
    width: 100,
    priority: 4
  },
  {
    name: 'cluster',
    labelKey: 'tableHeaders.cluster',
    value: 'clusterName',
    sort: 'clusterName',
    search: 'clusterName',
    width: 120,
    priority: 5
  },
  {
    name: 'namespace',
    labelKey: 'tableHeaders.namespace',
    value: 'namespace',
    sort: 'namespace',
    search: 'namespace',
    width: 120,
    priority: 6,
    breakpoint: 'tablet'
  },
  {
    name: 'installed',
    labelKey: 'tableHeaders.installed',
    value: 'installedAt',
    sort: 'installedAt',
    width: 120,
    type: 'age',
    priority: 7,
    breakpoint: 'desktop'
  },
  {
    name: 'actions',
    labelKey: 'tableHeaders.actions',
    value: '',
    sort: false,
    search: false,
    width: 120,
    type: 'actions',
    priority: 999,
    align: 'right'
  }
];

// === Repositories Table Headers ===
export const REPOSITORIES_TABLE_HEADERS: TableColumn[] = [
  {
    name: 'name',
    labelKey: 'tableHeaders.name',
    value: 'name',
    sort: ['name', 'displayName'],
    search: ['name', 'displayName', 'description'],
    type: 'name',
    priority: 1
  },
  {
    name: 'status',
    labelKey: 'tableHeaders.status',
    getValue: (row: any) => row.enabled && row.ready ? 'active' : 'pending',
    sort: false,
    width: 100,
    type: 'badge',
    priority: 2,
    formatter: 'RepositoryStatus'
  },
  {
    name: 'url',
    labelKey: 'tableHeaders.url',
    value: 'url',
    sort: 'url',
    search: 'url',
    width: 250,
    priority: 3,
    formatter: 'LinkWithIcon'
  },
  {
    name: 'type',
    labelKey: 'tableHeaders.type',
    value: 'type',
    sort: 'type',
    width: 80,
    priority: 4,
    formatter: 'Capitalize'
  },
  {
    name: 'charts',
    labelKey: 'tableHeaders.charts',
    getValue: (row: any) => row.stats?.chartCount || 0,
    sort: false,
    width: 80,
    priority: 5,
    formatter: 'Number',
    align: 'center'
  },
  {
    name: 'lastSync',
    labelKey: 'tableHeaders.lastSync',
    getValue: (row: any) => row.stats?.lastIndexed,
    sort: false,
    width: 120,
    type: 'age',
    priority: 6,
    breakpoint: 'desktop',
    dashIfEmpty: true
  },
  {
    name: 'actions',
    labelKey: 'tableHeaders.actions',
    value: '',
    sort: false,
    search: false,
    width: 100,
    type: 'actions',
    priority: 999,
    align: 'right'
  }
];

// === Clusters Table Headers ===
export const CLUSTERS_TABLE_HEADERS: TableColumn[] = [
  {
    name: 'name',
    labelKey: 'tableHeaders.name',
    value: 'name',
    sort: ['name', 'displayName'],
    search: ['name', 'displayName'],
    type: 'name',
    priority: 1
  },
  {
    name: 'state',
    labelKey: 'tableHeaders.state',
    value: 'state',
    sort: 'state',
    width: 100,
    type: 'state',
    priority: 2
  },
  {
    name: 'version',
    labelKey: 'tableHeaders.kubernetesVersion',
    value: 'kubernetesVersion',
    sort: 'kubernetesVersion',
    width: 120,
    priority: 3
  },
  {
    name: 'provider',
    labelKey: 'tableHeaders.provider',
    value: 'provider',
    sort: 'provider',
    width: 100,
    priority: 4,
    breakpoint: 'tablet'
  },
  {
    name: 'nodes',
    labelKey: 'tableHeaders.nodes',
    getValue: (row: any) => `${row.readyNodes || 0}/${row.totalNodes || 0}`,
    sort: false,
    width: 80,
    priority: 5,
    align: 'center'
  },
  {
    name: 'apps',
    labelKey: 'tableHeaders.apps',
    getValue: (row: any) => row.appCount || 0,
    sort: false,
    width: 60,
    priority: 6,
    formatter: 'Number',
    align: 'center'
  },
  {
    name: 'created',
    labelKey: 'tableHeaders.created',
    value: 'createdAt',
    sort: 'createdAt',
    width: 120,
    type: 'age',
    priority: 7,
    breakpoint: 'desktop'
  },
  {
    name: 'actions',
    labelKey: 'tableHeaders.actions',
    value: '',
    sort: false,
    search: false,
    width: 100,
    type: 'actions',
    priority: 999,
    align: 'right'
  }
];

// === Table Configuration Sets ===
export const TABLE_CONFIGS = {
  apps: {
    headers: APPS_TABLE_HEADERS,
    defaultSort: { field: 'name', direction: 'asc' },
    searchFields: ['name', 'displayName', 'description', 'category'],
    filterFields: ['status', 'category', 'repository']
  },
  installations: {
    headers: INSTALLATIONS_TABLE_HEADERS,
    defaultSort: { field: 'installedAt', direction: 'desc' },
    searchFields: ['releaseName', 'appName', 'clusterName', 'namespace'],
    filterFields: ['status', 'clusterId', 'namespace']
  },
  repositories: {
    headers: REPOSITORIES_TABLE_HEADERS,
    defaultSort: { field: 'name', direction: 'asc' },
    searchFields: ['name', 'url', 'description'],
    filterFields: ['type', 'enabled']
  },
  clusters: {
    headers: CLUSTERS_TABLE_HEADERS,
    defaultSort: { field: 'name', direction: 'asc' },
    searchFields: ['name', 'displayName', 'provider'],
    filterFields: ['state', 'provider', 'kubernetesVersion']
  }
} as const;

export type TableConfigKey = keyof typeof TABLE_CONFIGS;

// === Helper Functions ===

/**
 * Get table headers by table type
 */
export function getTableHeaders(tableType: TableConfigKey): TableColumn[] {
  return TABLE_CONFIGS[tableType]?.headers || [];
}

/**
 * Get table configuration
 */
export function getTableConfig(tableType: TableConfigKey) {
  return TABLE_CONFIGS[tableType];
}

/**
 * Get columns by priority (for responsive display)
 */
export function getColumnsByPriority(tableType: TableConfigKey, maxColumns?: number): TableColumn[] {
  const headers = getTableHeaders(tableType);
  const sorted = headers.slice().sort((a, b) => (a.priority || 999) - (b.priority || 999));
  
  return maxColumns ? sorted.slice(0, maxColumns) : sorted;
}

/**
 * Get columns by breakpoint
 */
export function getColumnsByBreakpoint(
  tableType: TableConfigKey, 
  breakpoint: 'mobile' | 'tablet' | 'desktop' = 'desktop'
): TableColumn[] {
  const headers = getTableHeaders(tableType);
  
  return headers.filter(col => {
    if (!col.breakpoint) return true; // Always show if no breakpoint specified
    
    switch (breakpoint) {
      case 'mobile':
        return !col.breakpoint || col.breakpoint === 'mobile';
      case 'tablet':
        return !col.breakpoint || ['mobile', 'tablet'].includes(col.breakpoint);
      case 'desktop':
        return true; // Show all columns on desktop
      default:
        return true;
    }
  });
}

export default TABLE_CONFIGS;