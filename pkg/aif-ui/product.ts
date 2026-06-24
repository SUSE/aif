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
import { checkOperatorConnection } from './utils/operator-config';
import { canAccessExtension } from './utils/access';

export { PRODUCT } from './config/suseai';

const CRTB_TYPE         = 'management.cattle.io.clusterroletemplatebinding';
const AIFACTORY_API_GROUP = 'ai-platform.suse.com';

export function init($plugin: IPlugin, store: RancherStore) {
  store.registerModule?.(PRODUCT, suseaiStore);

  const { product, virtualType, basicType, weightType } = $plugin.DSL(store, PRODUCT);

  product({
    icon:              'suseai',
    iconHeader:        require('./assets/SUSE-AI-Factory-Logo_pos-green-horizontal.svg'),
    inStore:           SUSEAI_PRODUCT.inStore,
    isMultiClusterApp: true,
    showClusterSwitcher: false,
    weight:            SUSEAI_PRODUCT.weight,
    // Both conditions are AND-checked by Rancher's activeProducts getter.
    // ifHaveType: users without CRTB schema access (standard users, cluster members)
    // never see the icon.
    // ifHaveGroup: the ai-platform.suse.com CRDs are management-cluster-scoped;
    // downstream cluster owners do not have access to them in the management store,
    // so they are excluded despite having CRTB access.
    // Navigation is further restricted by the nav guard below.
    ifHaveType:  CRTB_TYPE,
    ifHaveGroup: AIFACTORY_API_GROUP,
    to: {
      name: `c-cluster-${PRODUCT}-${PAGE_TYPES.OVERVIEW}`,
      params: { product: PRODUCT, cluster: MANAGEMENT_CLUSTER },
      meta: { product: PRODUCT, cluster: MANAGEMENT_CLUSTER }
    }
  } as any);

  const router = store.state.$router;

  if (router && typeof router.beforeEach === 'function') {
    router.beforeEach(async (to: any, _from: any, next: any) => {
      if (!to.name?.toString().startsWith(`c-cluster-${PRODUCT}-`)) return next();

      try {
        const canAccess = await canAccessExtension(store);

        canAccess ? next() : next({ name: 'home' });
      } catch {
        // canAccessExtension can throw if the management store is reset at
        // runtime (logout, session expiry, network failure). Fail closed to
        // avoid leaving the router in a hung state with next() never called.
        next({ name: 'home' });
      }
    });
  }

  VIRTUAL_TYPES.forEach(vType => {
    virtualType({ name: vType.name, label: vType.label, route: vType.route });
  });

  Object.entries(NAV_WEIGHTS).forEach(([type, weight]) => {
    weightType(type, weight, true);
  });

  basicType(BASIC_TYPES);

  void checkOperatorConnection();
}
