import type { IPlugin } from '@shell/core/types';
import { PRODUCT_NAME, MANAGEMENT_CLUSTER, PAGE_IDS, CRD_TYPES } from './types';

const routeFor = (pageId: string) => ({
  name:   `${ PRODUCT_NAME }-c-cluster-${ pageId }`,
  params: {
    product: PRODUCT_NAME,
    cluster: MANAGEMENT_CLUSTER
  },
  meta: {
    product: PRODUCT_NAME,
    cluster: MANAGEMENT_CLUSTER
  }
});

const pageNav = [
  { id: PAGE_IDS.OVERVIEW,        labelKey: 'aif.nav.overview',        weight: 600 },
  { id: PAGE_IDS.APPS,            labelKey: 'aif.nav.apps',            weight: 500 },
  { id: PAGE_IDS.BLUEPRINTS,      labelKey: 'aif.nav.blueprints',      weight: 400 },
  { id: PAGE_IDS.BUNDLES,         labelKey: 'aif.nav.bundles',         weight: 300 },
  { id: PAGE_IDS.PENDING_REVIEWS, labelKey: 'aif.nav.pendingReviews',  weight: 200 },
  { id: PAGE_IDS.WORKLOADS,       labelKey: 'aif.nav.workloads',       weight: 150 },
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
    configureType,
    ignoreType
  } = $plugin.DSL(store, PRODUCT_NAME) as any;

  product({
    icon:                'ai-factory',
    inStore:             'management',
    isMultiClusterApp:   true,
    showClusterSwitcher: false,
    weight:              100,
    to:                  routeFor(PAGE_IDS.OVERVIEW)
  } as any);

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

  basicType([
    PAGE_IDS.OVERVIEW,
    PAGE_IDS.APPS,
    PAGE_IDS.BLUEPRINTS,
    PAGE_IDS.BUNDLES,
    PAGE_IDS.PENDING_REVIEWS,
    PAGE_IDS.WORKLOADS,
    PAGE_IDS.SETTINGS
  ]);

  basicType([CRD_TYPES.BUNDLE, CRD_TYPES.BLUEPRINT, CRD_TYPES.WORKLOAD, CRD_TYPES.SETTINGS]);

  // Suppress auto-generated nav items for raw CRD types — navigation is handled
  // by the virtualType entries above. ignoreType hides from the sidebar tree only;
  // it does not prevent direct store dispatches used by the custom pages.
  ignoreType(CRD_TYPES.BUNDLE);
  ignoreType(CRD_TYPES.BLUEPRINT);
  ignoreType(CRD_TYPES.WORKLOAD);
  ignoreType(CRD_TYPES.SETTINGS);

  // Bundles: author-created, directly deletable (spec §8.2 — "Delete: available in any state").
  // Blueprints: minted by approval workflow; only Deprecate/Withdraw/Reactivate are valid lifecycle actions.
  // Workloads: removed via a custom Uninstall action (P6-6) that cleans up K8s resources, not raw delete.
  // Settings: singleton CR managed by the operator; no delete action in spec.
  configureType(CRD_TYPES.BUNDLE,     { isCreatable: true,  isEditable: true,  isRemovable: true,  canYaml: true  });
  configureType(CRD_TYPES.BLUEPRINT,  { isCreatable: false, isEditable: false, isRemovable: false, canYaml: true  });
  configureType(CRD_TYPES.WORKLOAD,   { isCreatable: false, isEditable: false, isRemovable: false               });
  configureType(CRD_TYPES.SETTINGS,   { isCreatable: false, isEditable: true,  isRemovable: false               });
}
