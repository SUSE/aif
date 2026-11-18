import type { BlueprintComponent } from '../types/blueprint-types';
import type { RegistryStatus } from '../types/aiworkload-types';

/**
 * Registry credentials the Settings page manages. These keys match both the
 * Settings spec blocks and the RegistryStatus response from
 * getRegistryStatus().
 */
export type RequiredCredential = 'applicationCollection' | 'suseRegistry' | 'nvidia';

/**
 * Maps a blueprint component's `chartRepo` (a ClusterRepo name) to the
 * credential that must be configured for the operator to create that repo.
 * Mirrors the operator's Settings controller: the ClusterRepo names come from
 * internal/credentials/credentials.go, and both NVIDIA repos gate on the single
 * NVIDIA credential.
 */
export const REPO_CREDENTIAL: Record<string, RequiredCredential> = {
  'application-collection': 'applicationCollection',
  'suse-ai-registry':       'suseRegistry',
  nvidia:                   'nvidia',
  'nvidia-blueprints':      'nvidia',
};

export function requiredCredentialForRepo(chartRepo: string): RequiredCredential | null {
  return REPO_CREDENTIAL[chartRepo] ?? null;
}

/**
 * A credential counts as configured when the operator can resolve it. We read
 * this from getRegistryStatus() (the /registry-status endpoint), which resolves
 * credentials the way the operator does (EffectiveRefs: Settings spec refs and
 * well-known secret names) and reports true only when both user and token
 * resolve, the operator's requirement before it creates the ClusterRepo.
 */
function isCredentialConfigured(cred: RequiredCredential, status: RegistryStatus | null): boolean {
  return status?.[cred] === true;
}

/**
 * Returns the distinct required credentials that the blueprint's components use
 * (by chartRepo) but that the operator cannot resolve, per getRegistryStatus().
 * A null status means unknown (operator unreachable) and returns []. Components
 * whose chartRepo is not in REPO_CREDENTIAL are skipped — the operator status is
 * the backstop.
 */
export function missingCredentialsForBlueprint(
  components: BlueprintComponent[],
  registryStatus: RegistryStatus | null,
): RequiredCredential[] {
  if (!registryStatus) return [];
  const required = new Set<RequiredCredential>();
  for (const c of components || []) {
    const cred = requiredCredentialForRepo(c.chartRepo);
    if (cred) required.add(cred);
  }
  return Array.from(required).filter((cred) => !isCredentialConfigured(cred, registryStatus));
}
