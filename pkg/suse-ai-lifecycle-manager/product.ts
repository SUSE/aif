import type { IPlugin } from '@shell/core/types';
import suseaiStore from './store/suseai-common';
import {
  PRODUCT,
  MANAGEMENT_CLUSTER,
  SUSEAI_PRODUCT,
  VIRTUAL_TYPES,
  BASIC_TYPES,
  NAV_WEIGHTS,
  PAGE_TYPES
} from './config/suseai';
import type { RancherStore } from './types/rancher-types';

export { PRODUCT } from './config/suseai';

export function init($plugin: IPlugin, store: RancherStore) {
  const { product, virtualType, basicType, weightType } = $plugin.DSL(store, PRODUCT);

  // Register store modules following standard patterns
  store.registerModule?.(PRODUCT, suseaiStore);

  // Configure product following standard patterns
  product({
    icon: 'suseai',
    inStore: SUSEAI_PRODUCT.inStore,
    isMultiClusterApp: true,
    showClusterSwitcher: false,
    weight: SUSEAI_PRODUCT.weight,
    to: {
      name: `c-cluster-${PRODUCT}-${PAGE_TYPES.OVERVIEW}`,
      params: { product: PRODUCT, cluster: MANAGEMENT_CLUSTER },
      meta: { product: PRODUCT, cluster: MANAGEMENT_CLUSTER }
    }
  } as any);

  // Register virtual types following standard patterns
  VIRTUAL_TYPES.forEach(vType => {
    virtualType({
      name:  vType.name,
      label: vType.label,
      route: vType.route
    });
  });

  // Apply explicit sidebar ordering (higher weight = higher in list).
  Object.entries(NAV_WEIGHTS).forEach(([type, weight]) => {
    weightType(type, weight, true);
  });

  // Register basic types
  basicType(BASIC_TYPES);
}
