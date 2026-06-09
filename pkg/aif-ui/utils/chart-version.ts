/**
 * Pure helpers for filtering and ordering Helm chart versions shown in the
 * install/upgrade version selectors. Shared by `services/rancher-apps.ts` and
 * `services/chart-service.ts` so the accepted-version rules live in one place.
 */

// Versions we surface in the UI: x.y.z, optionally with an upstream `v` prefix
// (NVIDIA NGC charts, e.g. `v1.2.1`) or a Rancher `+upA.B.C` build suffix
// (SUSE charts, e.g. `1.0.0+up2.3.4`). Rancher/Helm semver treats the leading
// `v` as valid, so we accept it too.
const CATALOG_VERSION = /^v?\d+\.\d+\.\d+(\+up\d+\.\d+\.\d+)?$/;

/** True when a version string is in a format we display in selectors. */
export function isCatalogVersion(version: string): boolean {
  return CATALOG_VERSION.test(version);
}

/**
 * Compare two version strings for descending (newest-first) order.
 * A leading `v` is stripped before numeric comparison so `v1.2.1` and `1.2.1`
 * order correctly (otherwise `parseInt('v1')` is NaN and the major component
 * collapses to 0, corrupting cross-major ordering).
 */
export function compareVersionsDesc(a: string, b: string): number {
  const parts = (v: string): number[] =>
    v.replace(/^v/, '').split('.').map((n) => parseInt(n, 10) || 0);
  const pa = parts(a);
  const pb = parts(b);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const da = pa[i] || 0;
    const db = pb[i] || 0;
    if (da !== db) return db - da;
  }
  return b.localeCompare(a);
}

/** Dedupe, drop non-displayable versions, and sort newest-first. */
export function filterAndSortVersions(versions: string[]): string[] {
  return Array.from(new Set(versions)).filter(isCatalogVersion).sort(compareVersionsDesc);
}
