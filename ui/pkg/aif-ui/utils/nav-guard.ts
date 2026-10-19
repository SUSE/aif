import type { NavigationGuard } from 'vue-router';
import type { RancherStore } from '../types/rancher-types';
import { canAccessExtension, ManagementNotReadyError } from './access';
import { logger } from './logger';

/**
 * Router beforeEach guard for the extension's routes (names starting with
 * `c-cluster-<product>-`). Sends the user home when access is denied, and also
 * shows a growl when access could not be checked.
 */
export function createAccessNavGuard(store: RancherStore, product: string): NavigationGuard {
  return async(to, _from, next) => {
    if (!to.name?.toString().startsWith(`c-cluster-${ product }-`)) return next();

    try {
      if (await canAccessExtension(store)) {
        next();
      } else {
        next({ name: 'home' });
      }
    } catch (err) {
      if (err instanceof ManagementNotReadyError) {
        logger.warn('management store did not load in time; failing closed', { action: 'nav-guard', data: err });
      } else {
        // canAccessExtension can throw if the management store is reset at
        // runtime (logout, session expiry, network failure). Fail closed to
        // avoid leaving the router in a hung state with next() never called.
        logger.warn('canAccessExtension threw unexpectedly; failing closed', { action: 'nav-guard', data: err });
      }
      store.dispatch('growl/error', {
        title:   'Access check failed',
        message: 'Unable to verify extension access. Please reload the page.',
        timeout: 8000,
      });
      next({ name: 'home' });
    }
  };
}
