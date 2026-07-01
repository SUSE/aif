// Helpers for deriving per-cluster AIWorkload CR names.
//
// Background: an AIWorkload CR's identity in the management cluster is
// (namespace, name). Two installs of the same chart targeting different
// downstream clusters would otherwise collide on (namespace, release) — even
// though they're logically independent deployments on independent clusters.
// We sidestep that by suffixing the cluster ID onto the CR name; the user
// continues to see the unprefixed release name in the UI.

// FNV-1a 32-bit; matches the operator's Go capReleaseName so client+server
// produce identical truncated names for the same input.
function fnv32a(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h >>> 0;
}

function toBase36(n: number): string {
  return n.toString(36);
}

// DNS-1123 label rules — lower-case alphanumerics + hyphens, can't start or
// end with hyphen. Cluster IDs from Rancher already satisfy this; sanitize
// defensively for arbitrary release names so we never emit invalid metadata.
function sanitizeLabel(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '');
}

// K8s/Helm DNS-1123 label cap.
const MAX_LABEL_LEN = 63;
// Suffix length when truncation forces a deterministic FNV hash.
const HASH_SUFFIX_LEN = 6;

// crNameForCluster builds `<release>-<clusterId>` as a valid DNS-1123 label.
// Truncates with a stable base-36 hash when the joined name exceeds 63 chars,
// so different (release, cluster) pairs that share a long common prefix
// don't end up with identical truncated names.
//
// The hash is computed from the full pre-truncation name, NOT the cluster ID
// alone — this guarantees uniqueness across overlapping prefix cases.
export function crNameForCluster(release: string, clusterId: string): string {
  const safe = sanitizeLabel(`${release}-${clusterId}`);
  if (safe.length <= MAX_LABEL_LEN) {
    return safe;
  }
  const suffix = toBase36(fnv32a(safe)).slice(0, HASH_SUFFIX_LEN);
  const head = safe.slice(0, MAX_LABEL_LEN - suffix.length - 1).replace(/-+$/, '');
  return head ? `${head}-${suffix}` : suffix;
}

// crNameForClusters returns one CR name per requested cluster, preserving
// input order. Convenience wrapper so wizards don't reach for crNameForCluster
// in a loop.
export function crNameForClusters(release: string, clusterIds: string[]): string[] {
  return clusterIds.map((c) => crNameForCluster(release, c));
}
