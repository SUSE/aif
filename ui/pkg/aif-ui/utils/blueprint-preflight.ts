import type { BlueprintComponent } from '../types/blueprint-types';
import type { RegistryStatus } from '../types/aiworkload-types';
import type { AppCRD } from '../types/rancher-types';

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
 * internal/credentials/credentials.go, and the NVIDIA repos (including the
 * gated nvidia-runai team repo) gate on the single
 * NVIDIA credential.
 */
export const REPO_CREDENTIAL: Record<string, RequiredCredential> = {
  'application-collection': 'applicationCollection',
  'suse-ai-registry':       'suseRegistry',
  nvidia:                   'nvidia',
  'nvidia-blueprints':      'nvidia',
  'nvidia-runai':           'nvidia',
};

export function requiredCredentialForRepo(chartRepo: string): RequiredCredential | null {
  return REPO_CREDENTIAL[chartRepo] ?? null;
}

/**
 * A registry is satisfied when the operator can create its ClusterRepos, per
 * getRegistryStatus(): `usable` covers resolved credentials and an anonymous
 * NVIDIA mirror. An older operator omits `usable`; its top-level fields (both
 * user and token resolve) are used instead.
 */
function isCredentialConfigured(cred: RequiredCredential, status: RegistryStatus | null): boolean {
  return (status?.usable ?? status)?.[cred] === true;
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

export interface ExistingRelease {
  clusterId: string;
  release:   string;
  chart:     string;
  version:   string;
  namespace: string;
}

/**
 * Returns, per component chartName, the Helm releases (catalog.cattle.io apps)
 * on the target clusters that already run the same chart. Matching is by chart
 * name only; components without a match are omitted.
 */
export function existingReleasesByComponent(
  components: BlueprintComponent[],
  appsByCluster: Record<string, AppCRD[]>,
): Record<string, ExistingRelease[]> {
  const wanted = new Set((components || []).map((c) => c.chartName));
  const result: Record<string, ExistingRelease[]> = {};
  for (const [clusterId, apps] of Object.entries(appsByCluster || {})) {
    for (const app of apps || []) {
      const chart = app?.spec?.chart?.metadata?.name;
      if (!chart || !wanted.has(chart)) continue;
      if (!result[chart]) result[chart] = [];
      result[chart].push({
        clusterId,
        release:   app.metadata?.name || '',
        chart,
        version:   app.spec.chart?.metadata?.version || '',
        namespace: app.metadata?.namespace || '',
      });
    }
  }
  return result;
}
