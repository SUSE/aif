import { isAdminUser } from '@shell/store/type-map';
import type { RancherStore } from '../types/rancher-types';

export const LOCAL_CLUSTER = 'local';
export const CRTB_TYPE    = 'management.cattle.io.clusterroletemplatebinding';

const CLUSTER_OWNER_ROLE = 'cluster-owner';

interface CrtbBinding {
  metadata?:          { namespace?: string };
  roleTemplateName?:  string;
  userPrincipalName?: string;
}

// Cached so the CRTB scan runs at most once per session. Invalidated on CRTB
// store changes so mid-session role revocations are caught on next navigation.
let cachedDecision: boolean | null = null;

export function invalidateAccessCache(): void {
  cachedDecision = null;
}

async function resolveAccess(store: RancherStore): Promise<boolean> {
  const getters = store.getters;
  if (!getters) return false;

  if (isAdminUser(getters)) return true;

  const crtbMethods: string[] = (
    getters['management/schemaFor'](CRTB_TYPE)?.collectionMethods || []
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
 *
 *  1. isAdminUser  — schema-based fast path for global admins.
 *  2. CRTB POST    — eliminates cluster members and standard users.
 *  3. Local binding — confirms ownership of the management cluster,
 *                    filtering out users who own only downstream clusters.
 */
export async function canAccessExtension(store: RancherStore): Promise<boolean> {
  if (cachedDecision !== null) return cachedDecision;
  cachedDecision = await resolveAccess(store);
  return cachedDecision;
}
