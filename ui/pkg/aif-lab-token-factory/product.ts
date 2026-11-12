import type { IPlugin } from '@shell/core/types';
import { PRODUCT, LAB_TYPE, LAB_ROUTE } from './config';

/**
 * PoC RISK: load-order dependency on aif-ui.
 * The 'suseai' product is registered by the aif-ui extension. Rancher runs each
 * plugin's init in load order; if this lab loads before aif-ui, the virtualType
 * and basicType below attach to a product that does not exist yet and the nav
 * entry may be missing or the group may be dropped. A real implementation needs
 * either an explicit ordering guarantee from aif-ui or a registration hook
 * exposed by aif-ui for labs. We deliberately do NOT call product() here.
 */
export function init($plugin: IPlugin, store: any) {
  const { virtualType, basicType } = $plugin.DSL(store, PRODUCT);

  virtualType({
    name:  LAB_TYPE,
    label: 'Token Factory',
    icon:  'icon-key',
    route: {
      name:   LAB_ROUTE,
      params: { product: PRODUCT, cluster: '_' },
      meta:   { product: PRODUCT }
    }
  } as any);

  // Same call shape as aif-ui, plus a group name so the type lands under "Labs".
  basicType([LAB_TYPE], 'Labs');
}
