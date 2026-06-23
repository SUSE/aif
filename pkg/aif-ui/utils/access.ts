import { isAdminUser } from '@shell/store/type-map';

const LOCAL_CLUSTER      = 'local';
const CLUSTER_OWNER_ROLE = 'cluster-owner';
const CRTB_TYPE          = 'management.cattle.io.clusterroletemplatebinding';

/**
 * Full async access check (all three layers). Used both at product registration
 * (sidebar visibility) and in the navigation guard.
 *
 *  1. isAdminUser  — schema-based fast path for global admins.
 *  2. CRTB POST    — eliminates cluster members and standard users.
 *  3. Local binding — confirms ownership of the management cluster,
 *                    filtering out users who own only downstream clusters.
 */
export async function canAccessExtension(store: any): Promise<boolean> {
  const getters = store.getters;

  if (isAdminUser(getters)) return true;

  const crtbMethods: string[] = (
    getters['management/schemaFor'](CRTB_TYPE)?.collectionMethods || []
  ).map((m: string) => m.toUpperCase());

  if (!crtbMethods.includes('POST')) return false;

  await store.dispatch('management/findAll', { type: CRTB_TYPE });

  const principalId: string = getters['auth/principalId'] || '';
  const allCrtbs: any[]     = getters['management/all'](CRTB_TYPE) || [];

  return allCrtbs.some(
    (b: any) =>
      b.metadata?.namespace === LOCAL_CLUSTER &&
      b.roleTemplateName    === CLUSTER_OWNER_ROLE &&
      b.userPrincipalName   === principalId
  );
}
