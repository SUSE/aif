// Helm release-name helpers shared by the UI — the single source of the FNV-1a /
// capReleaseName logic. services/fleet-bundle.ts re-exports capReleaseName from here,
// and utils/workload-name.ts imports fnv1a32 from here; no other copies remain.

// 53 = 63 (K8s DNS-1123 label max) − 10 bytes Helm reserves for generated
// suffixes. Fleet validates spec.helm.releaseName against this.
const HELM_RELEASE_NAME_MAX = 53;
const HELM_HASH_LEN         = 6; // base36 suffix; 36^6 ≈ 2.2e9 distinct values.

// capReleaseName caps a name to Helm's 53-byte release-name limit, appending a
// short deterministic FNV-1a/base36 hash when truncating so distinct names that
// share a prefix don't collide. Always returns a valid DNS-1123 label.
//
// PARITY IS LOAD-BEARING: this MUST match the operator's Go capReleaseName
// (aif-operator .../blueprint.go) byte-for-byte. The UI derives expected release
// names with this function and matches them against the app.kubernetes.io/instance
// label the OPERATOR produced; any divergence for names > 53 chars silently
// misses those releases.
//
// Callers pass ASCII names (DNS-1123 labels), so .length (UTF-16 units) equals
// the byte count Go measures; not safe for arbitrary multibyte input.
export function capReleaseName(name: string): string {
  if (name.length <= HELM_RELEASE_NAME_MAX) return name;
  const hash = fnv1a32(name).toString(36).slice(0, HELM_HASH_LEN);
  const head = name.slice(0, HELM_RELEASE_NAME_MAX - hash.length - 1).replace(/^-+|-+$/g, '');
  return head ? `${ head }-${ hash }` : hash;
}

// fnv1a32 is the 32-bit FNV-1a hash, matching Go's hash/fnv New32a() byte-for-byte
// for ASCII input. Math.imul does the 32-bit multiply without precision loss.
export function fnv1a32(s: string): number {
  let h = 0x811c9dc5; // offset basis (2166136261)
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193); // FNV prime (16777619)
  }
  return h >>> 0;
}
