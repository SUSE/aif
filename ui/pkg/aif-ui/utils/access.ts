import { isAdminUser } from '@shell/store/type-map';
import type { RancherStore } from '../types/rancher-types';

export const LOCAL_CLUSTER = 'local';
export const CRTB_TYPE    = 'management.cattle.io.clusterroletemplatebinding';

const CLUSTER_OWNER_ROLE = 'cluster-owner';

// Same 30s limit the Rancher shell uses when it waits for managementReady.
export const MANAGEMENT_READY_TIMEOUT_MS = 30000;

interface CrtbBinding {
  metadata?:          { namespace?: string };
  roleTemplateName?:  string;
  userPrincipalName?: string;
}

/**
 * Thrown by canAccessExtension when the management store does not load within
 * the timeout, so callers can tell "could not check" apart from "denied".
 */
export class ManagementNotReadyError extends Error {
  constructor() {
    super(`Management store not ready after ${ MANAGEMENT_READY_TIMEOUT_MS / 1000 }s`);
    this.name = 'ManagementNotReadyError';
  }
}

// Cached so the CRTB scan runs at most once per session. Invalidated on CRTB
// store changes so mid-session role revocations are caught on next navigation.
let cachedDecision: boolean | null = null;

// Shared by navigations that arrive while a check is running, so they reuse one
// managementReady watcher, one timer and one CRTB findAll.
let inFlight: Promise<boolean> | null = null;

export function invalidateAccessCache(): void {
  cachedDecision = null;
}

/**
 * Resolves true once Rancher has loaded the management store (root state.managementReady),
 * or false if it does not within the timeout. The access checks below read management
 * schemas, and isAdminUser throws "Schemas aren't loaded yet" when they are missing.
 * The nav guard can run before that, e.g. on the navigation right after creating a cluster.
 */
function waitForManagementReady(store: RancherStore, timeoutMs = MANAGEMENT_READY_TIMEOUT_MS): Promise<boolean> {
  if (store.state.managementReady) return Promise.resolve(true);

  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      unwatch();
      resolve(false);
    }, timeoutMs);
    const unwatch = store.watch(
      (state) => state.managementReady,
      (ready) => {
        if (!ready) return;
        clearTimeout(timer);
        unwatch();
        resolve(true);
      }
    );
  });
}

async function resolveAccess(store: RancherStore): Promise<boolean> {
  const getters = store.getters;
  if (!getters) return false;

  if (isAdminUser(getters)) return true;

  // allowThrow=false: with no schemas loaded the getter throws instead of
  // returning null, and `?.` does not guard against that.
  const crtbMethods: string[] = (
    getters['management/schemaFor'](CRTB_TYPE, false, false)?.collectionMethods || []
  ).map((m: string) => m.toUpperCase());

  if (!crtbMethods.includes('POST')) return false;

  // The binding we need is always in the 'local' namespace. A full findAll can
  // be thousands of records in large installations; scoping to local keeps it small.
  try {
    await store.dispatch('management/findAll', { type: CRTB_TYPE, opt: { namespaced: LOCAL_CLUSTER } });
  } catch {
    return false;
  }

  const principalId: string = getters['auth/principalId'];
  if (!principalId) return false;

  // management/all returns the full Vuex store contents, not just what the
  // namespace-scoped findAll fetched. Pre-filter to local so the scan below
  // cannot be confused by bindings from downstream namespaces.
  const localCrtbs: CrtbBinding[] = (getters['management/all'](CRTB_TYPE) || [])
    .filter((b: CrtbBinding) => b.metadata?.namespace === LOCAL_CLUSTER);

  // Known limitation: only direct user bindings are checked. If cluster-owner
  // access was granted through a group principal (LDAP group, GitHub org, etc.),
  // the CRTB will have groupPrincipalName set and userPrincipalName empty, so
  // this check produces a false negative for that user.
  // A proper fix requires verifying which principals /v3/principals returns for
  // non-admin users and whether group memberships are included — needs testing
  // against a real Rancher instance with external auth configured.
  //
  // Known limitation: custom cluster role templates that inherit from
  // 'cluster-owner' are not recognised. Only the built-in role is checked.
  // This is intentional — inherited roles may grant equivalent permissions
  // but expanding the check requires enumerating the role hierarchy, which
  // is not supported by the current access model.
  return localCrtbs.some(
    (b) =>
      b.roleTemplateName  === CLUSTER_OWNER_ROLE &&
      b.userPrincipalName === principalId
  );
}

/**
 * Returns true if the current user is allowed to access the extension.
 * Result is cached for the session; invalidated when CRTBs change in the store.
 * Waits for the management store first; if it never loads, rejects with
 * ManagementNotReadyError and caches nothing, so the next navigation checks again.
 * Concurrent calls share one in-flight check.
 *
 *  1. isAdminUser  — schema-based fast path for global admins.
 *  2. CRTB POST    — eliminates cluster members and standard users.
 *  3. Local binding — confirms ownership of the management cluster,
 *                    filtering out users who own only downstream clusters.
 */
export async function canAccessExtension(store: RancherStore): Promise<boolean> {
  if (cachedDecision !== null) return cachedDecision;
  if (inFlight) return inFlight;

  inFlight = (async () => {
    if (!(await waitForManagementReady(store))) throw new ManagementNotReadyError();
    cachedDecision = await resolveAccess(store);

    return cachedDecision;
  })().finally(() => {
    inFlight = null;
  });

  return inFlight;
}
