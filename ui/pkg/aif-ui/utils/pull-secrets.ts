import type { RegistryStatus } from '../types/aiworkload-types';

// Image pull secrets the operator delivers to every AIWorkload's target
// namespace. They must equal combinedPullSecretName and
// nvidiaImagePullSecretName in operator/internal/controller/aiworkload/blueprint.go.
export const OPERATOR_SUSE_PULL_SECRET = 'suse-ai-pull-combined';
export const OPERATOR_NVIDIA_PULL_SECRET = 'ngc-secret';

// Name prefix of the per-registry pull secrets the browser creates
// (ensureRegistrySecretSimple). Older releases referenced them from chart
// values; the operator-delivered combined secret replaces them.
export const LEGACY_SUSE_PULL_SECRET_PREFIX = 'suse-ai-pull-secret-';

// operatorPullSecretNames returns the operator-delivered pull secret names a
// chart from the given library should reference. SUSE charts use the combined
// secret, which carries the auths of every configured registry (NVIDIA
// included). NVIDIA charts use ngc-secret. Custom and unclassified repositories
// get none, and so does an unknown registry status (fail-soft).
export function operatorPullSecretNames(library: string | undefined, status: RegistryStatus | null): string[] {
  if (!status) return [];
  if (library === 'suse-ai') {
    return status.applicationCollection || status.suseRegistry || status.nvidia ? [OPERATOR_SUSE_PULL_SECRET] : [];
  }
  if (library === 'nvidia') {
    return status.nvidia ? [OPERATOR_NVIDIA_PULL_SECRET] : [];
  }
  return [];
}

// pullSecretEntryName returns the secret name of an imagePullSecrets entry,
// which charts accept as a {name} object or a plain string.
function pullSecretEntryName(entry: unknown): string | undefined {
  if (typeof entry === 'string') return entry;
  const name = entry && typeof entry === 'object' ? (entry as { name?: unknown }).name : undefined;
  return typeof name === 'string' ? name : undefined;
}

// mergePullSecretList keeps every existing entry in its own shape, except
// legacy browser-created names that are not requested, then appends each
// requested name that is missing as a {name} object. Deduplicated by name.
function mergePullSecretList(existing: unknown, names: string[]): unknown[] {
  const seen = new Set<string>();
  const merged: unknown[] = [];
  for (const entry of Array.isArray(existing) ? existing : []) {
    const name = pullSecretEntryName(entry);
    if (name !== undefined) {
      if (seen.has(name) || (name.startsWith(LEGACY_SUSE_PULL_SECRET_PREFIX) && !names.includes(name))) continue;
      seen.add(name);
    }
    merged.push(entry);
  }
  for (const name of names) {
    if (!seen.has(name)) {
      seen.add(name);
      merged.push({ name });
    }
  }
  return merged;
}

// mergeImagePullSecrets adds the given pull secret names to the chart's
// pod-spec imagePullSecrets and global.imagePullSecrets lists, in place. Entries
// the user added (e.g. for a private mirror) are kept; only legacy
// browser-created names that are not in `names` are dropped. No names leaves
// the values untouched, so a legacy secret is never dropped without a
// replacement. The other keys of global are untouched.
export function mergeImagePullSecrets(values: Record<string, any>, names: string[]): void {
  if (names.length === 0) return;
  values.imagePullSecrets = mergePullSecretList(values.imagePullSecrets, names);
  const global = values.global && typeof values.global === 'object' && !Array.isArray(values.global) ? values.global : {};
  values.global = { ...global, imagePullSecrets: mergePullSecretList(global.imagePullSecrets, names) };
}

// Keep in sync with the operator's chart_pullsecret_shape.go. Network Operator
// wraps each entry as {name: entry}, so its operator-level override needs names
// rather than objects. Leave root/global lists in the shape its subcharts use.
const FLAT_STRING_PULL_SECRET_PATHS = new Map<string, string[]>([
  ['network-operator', ['operator', 'imagePullSecrets']],
]);

export function adaptPullSecretShapeForChart(chartName: string, values: Record<string, any>): void {
  const keys = FLAT_STRING_PULL_SECRET_PATHS.get(chartName.split('/').pop() || '');
  if (!keys?.length || !Array.isArray(values.imagePullSecrets)) return;
  const names: string[] = values.imagePullSecrets
    .filter((entry: any) => entry && typeof entry.name === 'string' && entry.name !== '')
    .map((entry: { name: string }) => entry.name);
  if (names.length === 0) return;

  let parent = values;
  for (const key of keys.slice(0, -1)) {
    const child = parent[key];
    if (child === undefined || child === null) {
      parent[key] = {};
    } else if (typeof child !== 'object' || Array.isArray(child)) {
      return;
    }
    parent = parent[key];
  }
  const leaf = keys[keys.length - 1];
  const existing = parent[leaf];
  if (existing !== undefined && existing !== null && !Array.isArray(existing)) return;
  const list = existing || [];
  parent[leaf] = [...names.filter(name => !list.includes(name)), ...list];
}
