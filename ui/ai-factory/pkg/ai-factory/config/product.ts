import type { IPlugin } from '@shell/core/types';
import { PRODUCT_NAME, BLANK_CLUSTER, PAGE_IDS } from './types';

const routeFor = (pageId: string) => ({
  name:   `${ PRODUCT_NAME }-c-cluster-${ pageId }`,
  params: {
    product: PRODUCT_NAME,
    cluster: BLANK_CLUSTER
  },
  meta: {
    product: PRODUCT_NAME,
    cluster: BLANK_CLUSTER
  }
});

const pageNav = [
  { id: PAGE_IDS.OVERVIEW,        labelKey: 'aif.nav.overview',        weight: 600 },
  { id: PAGE_IDS.APPS,            labelKey: 'aif.nav.apps',            weight: 500 },
  { id: PAGE_IDS.BLUEPRINTS,      labelKey: 'aif.nav.blueprints',      weight: 400 },
  { id: PAGE_IDS.BUNDLES,         labelKey: 'aif.nav.bundles',         weight: 300 },
  { id: PAGE_IDS.WORKLOADS,       labelKey: 'aif.nav.workloads',       weight: 200 },
  { id: PAGE_IDS.PENDING_REVIEWS, labelKey: 'aif.nav.pendingReviews',  weight: 150 },
  { id: PAGE_IDS.SETTINGS,        labelKey: 'aif.nav.settings',        weight: 100 }
];

/**
 * Product registration following Rancher Dashboard extension DSL patterns.
 */
export function init($plugin: IPlugin, store: any): void {
  const {
    product,
    virtualType,
    basicType,
    weightGroup
  } = $plugin.DSL(store, PRODUCT_NAME);

  product({
    icon:                'ai-factory',
    inStore:             'management',
    showClusterSwitcher: false,
    weight:              100,
    to:                  routeFor(PAGE_IDS.OVERVIEW)
  });

  pageNav.forEach((page) => {
    // ConfigureVirtualTypeOptions is missing `weight`, but type-map.js reads type.weight
    // directly (before the label-keyed map lookup that breaks for hyphenated IDs).
    virtualType({
      name:     page.id,
      labelKey: page.labelKey,
      route:    routeFor(page.id),
      weight:   page.weight
    } as any);
  });

  const globalPages = [
    PAGE_IDS.OVERVIEW,
    PAGE_IDS.APPS,
    PAGE_IDS.BLUEPRINTS,
    PAGE_IDS.BUNDLES,
    PAGE_IDS.PENDING_REVIEWS,
    PAGE_IDS.SETTINGS
  ];

  const clusterPages = [
    PAGE_IDS.WORKLOADS
  ];

  basicType(globalPages, 'Global');
  basicType(clusterPages, 'Clusters');

  weightGroup('Global', 1100, true);
  weightGroup('Clusters', 1000, true);
}
