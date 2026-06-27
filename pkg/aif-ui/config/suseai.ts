/**
 * Main SUSE AI Product Configuration
 * Following standard patterns for product configuration
 * Centralizes product-specific constants and configurations
 */

import { PRODUCT_NAME, PRODUCT_SLUG, EXTENSION_VERSION } from '../utils/constants';

// === Product Constants ===
export const PRODUCT = PRODUCT_SLUG;
export const MANAGEMENT_CLUSTER = 'local';
export const BLANK_CLUSTER = '_';

// === Product Definition ===
export interface ProductConfig {
  name: string;
  slug: string;
  version: string;
  category: string;
  weight: number;
  icon: string;
  svg?: string;
  inStore: string;
  supportRoute?: string;
  docsRoute?: string;
}

export const SUSEAI_PRODUCT: ProductConfig = {
  name: PRODUCT_NAME,
  slug: PRODUCT_SLUG,
  version: EXTENSION_VERSION,
  category: 'global',
  weight: 80,
  icon: 'extension',
  inStore: 'management',
  supportRoute: 'https://www.suse.com/support/',
  docsRoute: 'https://documentation.suse.com/suse-ai-factory/latest/'
};

// === Navigation Configuration ===
export interface NavItem {
  name: string;
  label: string;
  route: {
    name: string;
    params: Record<string, string>;
    meta: Record<string, string>;
  };
  exact?: boolean;
  icon?: string;
}

export const NAVIGATION_ITEMS: NavItem[] = [
  {
    name:  'overview',
    label: 'Overview',
    route: {
      name:   `c-cluster-${PRODUCT}-overview`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta:   { product: PRODUCT }
    },
    icon: 'home'
  },
  {
    name: 'apps',
    label: 'Apps',
    route: {
      name: `c-cluster-${PRODUCT}-apps`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta: { product: PRODUCT }
    },
    icon: 'apps'
  },
  {
    name: 'install',
    label: 'Install',
    route: {
      name: `c-cluster-${PRODUCT}-install`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta: { product: PRODUCT }
    },
    icon: 'plus'
  },
  {
    name: 'manage',
    label: 'Manage',
    route: {
      name: `c-cluster-${PRODUCT}-manage`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta: { product: PRODUCT }
    },
    icon: 'gear'
  },
  {
    name:  'blueprints',
    label: 'Blueprints',
    route: {
      name:   `c-cluster-${ PRODUCT }-blueprints`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta:   { product: PRODUCT }
    },
    icon: 'blueprint'
  },
  {
    name:  'workloads',
    label: 'Workloads',
    route: {
      name:   `c-cluster-${ PRODUCT }-workloads`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta:   { product: PRODUCT }
    },
    icon: 'list-flat'
  },
];

// === Page Definitions ===
export const PAGE_TYPES = {
  OVERVIEW:     'overview',
  APPS:         'apps',
  INSTALL:      'install',
  MANAGE:       'manage',
  REPOSITORIES: 'repositories',
  BLUEPRINTS:   'blueprints',
  WORKLOADS:    'workloads',
  SETTINGS:     'settings',
  ABOUT:        'about',
} as const;

export type PageType = typeof PAGE_TYPES[keyof typeof PAGE_TYPES];

// === Virtual Type Configuration ===
export interface VirtualTypeConfig {
  name: string;
  label: string;
  route: NavItem['route'];
}

export const VIRTUAL_TYPES: VirtualTypeConfig[] = [
  {
    name:  PAGE_TYPES.OVERVIEW,
    label: 'Overview',
    route: {
      name:   `c-cluster-${PRODUCT}-${PAGE_TYPES.OVERVIEW}`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta:   { product: PRODUCT }
    }
  },
  {
    name:  PAGE_TYPES.APPS,
    label: 'Apps',
    route: {
      name:   `c-cluster-${PRODUCT}-${PAGE_TYPES.APPS}`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta:   { product: PRODUCT }
    }
  },
  {
    name:  PAGE_TYPES.BLUEPRINTS,
    label: 'Blueprints',
    route: {
      name:   `c-cluster-${ PRODUCT }-${ PAGE_TYPES.BLUEPRINTS }`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta:   { product: PRODUCT }
    }
  },
  {
    name:  PAGE_TYPES.WORKLOADS,
    label: 'Workloads',
    route: {
      name:   `c-cluster-${ PRODUCT }-${ PAGE_TYPES.WORKLOADS }`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta:   { product: PRODUCT }
    }
  },
  {
    name:  PAGE_TYPES.SETTINGS,
    label: 'Settings',
    route: {
      name:   `c-cluster-${ PRODUCT }-${ PAGE_TYPES.SETTINGS }`,
      params: { product: PRODUCT, cluster: MANAGEMENT_CLUSTER },
      meta:   { product: PRODUCT, cluster: MANAGEMENT_CLUSTER }
    }
  },
  {
    name:  PAGE_TYPES.ABOUT,
    label: 'About',
    route: {
      name:   `c-cluster-${ PRODUCT }-${ PAGE_TYPES.ABOUT }`,
      params: { product: PRODUCT, cluster: BLANK_CLUSTER },
      meta:   { product: PRODUCT }
    }
  }
];

// Explicit sidebar ordering: higher weight = higher in the list.
export const NAV_WEIGHTS: Record<string, number> = {
  [PAGE_TYPES.OVERVIEW]:   50,
  [PAGE_TYPES.APPS]:       40,
  [PAGE_TYPES.BLUEPRINTS]: 30,
  [PAGE_TYPES.WORKLOADS]:  20,
  [PAGE_TYPES.SETTINGS]:   10,
  [PAGE_TYPES.ABOUT]:      5,
};

// === Basic Types Configuration ===
export const BASIC_TYPES = [PAGE_TYPES.OVERVIEW, PAGE_TYPES.APPS, PAGE_TYPES.BLUEPRINTS, PAGE_TYPES.WORKLOADS, PAGE_TYPES.SETTINGS, PAGE_TYPES.ABOUT];

// === Product Metadata ===
export const PRODUCT_METADATA = {
  displayName: PRODUCT_NAME,
  description: 'Enterprise AI/ML application management for Kubernetes',
  vendor: 'SUSE',
  homepage: 'https://www.suse.com/',
  repository: 'https://github.com/suse/suse-ai-lifecycle-manager',
  license: 'Apache-2.0',
  keywords: ['ai', 'ml', 'kubernetes', 'helm', 'applications'],
  categories: ['AI/ML', 'Applications', 'Management'],
  maturity: 'stable',
  support: {
    level: 'enterprise',
    contact: 'support@suse.com',
    documentation: 'https://documentation.suse.com/suse-ai-factory/latest/',
    community: 'https://community.suse.com/'
  }
};

// === Feature Categories ===
export const FEATURE_CATEGORIES = {
  CORE: 'core',
  ADVANCED: 'advanced',
  EXPERIMENTAL: 'experimental',
  ENTERPRISE: 'enterprise'
} as const;

export type FeatureCategory = typeof FEATURE_CATEGORIES[keyof typeof FEATURE_CATEGORIES];

// === Export defaults ===
export default SUSEAI_PRODUCT;