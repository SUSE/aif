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
import { canAccessExtension, invalidateAccessCache, CRTB_TYPE, LOCAL_CLUSTER } from './utils/access';
import { logger } from './utils/logger';

export { PRODUCT } from './config/suseai';

const AIFACTORY_API_GROUP = 'ai-platform.suse.com';

let removeNavGuard:   (() => void) | null = null;
let removeCrtbWatch:  (() => void) | null = null;

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
    // ifHaveType and ifHaveGroup are valid at runtime but absent from the
    // published @rancher/shell DSL TypeScript types.
  } as Record<string, unknown>);

  const router = store.state.$router;

  if (router && typeof router.beforeEach === 'function') {
    removeNavGuard?.();
    removeNavGuard = router.beforeEach(async (to, _from, next) => {
      if (!to.name?.toString().startsWith(`c-cluster-${PRODUCT}-`)) return next();

      try {
        const canAccess = await canAccessExtension(store);

        canAccess ? next() : next({ name: 'home' });
      } catch (err) {
        // canAccessExtension can throw if the management store is reset at
        // runtime (logout, session expiry, network failure). Fail closed to
        // avoid leaving the router in a hung state with next() never called.
        logger.warn('canAccessExtension threw unexpectedly; failing closed', { action: 'nav-guard', data: err });
        store.dispatch('growl/error', {
          title:   'Access check failed',
          message: 'Unable to verify extension access. Please reload the page.',
          timeout: 8000,
        });
        next({ name: 'home' });
      }
    });

    // Invalidate the cached access decision when CRTBs change so a mid-session
    // role revocation is caught on the next navigation into the extension.
    removeCrtbWatch?.();
    removeCrtbWatch = store.watch(
      (_, getters) => getters['management/all'](CRTB_TYPE),
      () => invalidateAccessCache()
    );
  }

  VIRTUAL_TYPES.forEach(vType => {
    virtualType({ name: vType.name, label: vType.label, route: vType.route });
  });

  Object.entries(NAV_WEIGHTS).forEach(([type, weight]) => {
    weightType(type, weight, true);
  });

  basicType(BASIC_TYPES);

  // Prefetch local-namespace CRTBs for users who have CRTB schema access, so
  // the first navigation hits the Vuex cache rather than blocking on a network
  // request inside the nav guard. Skipped for users without schema access to
  // avoid a guaranteed 403 on every login.
  if (store.getters?.['management/schemaFor']?.(CRTB_TYPE)) {
    void store.dispatch('management/findAll', { type: CRTB_TYPE, opt: { namespaced: LOCAL_CLUSTER } });
  }

  void checkOperatorConnection();
}
