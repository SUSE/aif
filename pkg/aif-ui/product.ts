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

export function init($plugin: IPlugin, store: RancherStore) {
  store.registerModule?.(PRODUCT, suseaiStore);

  let registered = false;

  function doRegister() {
    if (registered) return;
    registered = true;

    const { product, virtualType, basicType, weightType } = $plugin.DSL(store, PRODUCT);

    product({
      icon:        'suseai',
      iconHeader:  require('./assets/SUSE-AI-Factory-Logo_pos-green-horizontal.svg'),
      inStore:     SUSEAI_PRODUCT.inStore,
      isMultiClusterApp: true,
      showClusterSwitcher: false,
      weight: SUSEAI_PRODUCT.weight,
      to: {
        name: `c-cluster-${PRODUCT}-${PAGE_TYPES.OVERVIEW}`,
        params: { product: PRODUCT, cluster: MANAGEMENT_CLUSTER },
        meta: { product: PRODUCT, cluster: MANAGEMENT_CLUSTER }
      }
    } as any);

    const router = (store as any).state.$router;

    router.beforeEach(async (to: any, _from: any, next: any) => {
      if (!to.name?.toString().startsWith(`c-cluster-${PRODUCT}-`)) return next();

      const canAccess = await canAccessExtension(store);

      canAccess ? next() : next({ name: 'home' });
    });

    VIRTUAL_TYPES.forEach(vType => {
      virtualType({
        name:  vType.name,
        label: vType.label,
        route: vType.route
      });
    });

    Object.entries(NAV_WEIGHTS).forEach(([type, weight]) => {
      weightType(type, weight, true);
    });

    basicType(BASIC_TYPES);

    void checkOperatorConnection();
  }

  // Full 3-layer async check runs once schemas are ready. This correctly
  // excludes downstream cluster owners (pass layer 2 but fail layer 3).
  function onSchemasReady() {
    void canAccessExtension(store).then(allowed => {
      if (allowed) doRegister();
    });
  }

  // Check synchronously whether schemas are already loaded (warm/cached session).
  let schemasReady = false;

  try {
    (store as any).getters['management/schemaFor']('management.cattle.io.cluster');
    schemasReady = true;
  } catch { /* not ready yet */ }

  if (schemasReady) {
    onSchemasReady();
  } else {
    // Watch for the cluster schema to appear — it's present for every
    // authenticated user once the management store initialises.
    const unwatch = (store as any).watch(
      () => {
        try {
          return !!(store as any).getters['management/schemaFor'](
            'management.cattle.io.cluster'
          );
        } catch {
          return false;
        }
      },
      (ready: boolean) => {
        if (!ready) return;
        unwatch();
        onSchemasReady();
      }
    );
  }
}
