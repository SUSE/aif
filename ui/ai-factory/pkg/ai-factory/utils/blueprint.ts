// Pure-domain helpers for the Blueprints page. No Vue / Steve / DOM imports.

export const BLUEPRINT_PHASES = ['Active', 'Deprecated', 'Withdrawn'] as const;
export type BlueprintPhase = (typeof BLUEPRINT_PHASES)[number];
export type BlueprintOrigin = 'Published' | 'WrapsVendorChart';

export interface ComponentRef {
  name:    string;
  kind:    string;
  app?:    { repo: string; chart: string; version: string };
}

export interface VendorChartRef {
  provider: string;
  repo:     string;
  chart:    string;
  version:  string;
}

export interface DeprecationStatus {
  reason?:    string;
  actionedBy: string;
  actionedAt: string;
}

export interface BlueprintVersion {
  id:                string;
  lineage:           string;
  version:           string;
  phase:             BlueprintPhase;
  useCase:           string;
  description:       string;
  changeDescription: string;
  components:        ComponentRef[];
  origin:            BlueprintOrigin;
  vendorChart?:      VendorChartRef;
  publishedBy:       string;
  publishedAt:       string;
  deprecation?:      DeprecationStatus;
  raw:               any;
}

export interface BlueprintLineage {
  lineage:        string;
  versions:       BlueprintVersion[]; // sorted desc, ALL phases
  latestActive?:  BlueprintVersion;
}

const LINEAGE_LABEL = 'ai.suse.com/blueprint-name';

export function toBlueprintVersion(cr: any): BlueprintVersion {
  const spec   = cr?.spec   ?? {};
  const status = cr?.status ?? {};
  const source = spec.source ?? {};
  const origin: BlueprintOrigin = source.type === 'WrapsVendorChart' ? 'WrapsVendorChart' : 'Published';

  return {
    id:                cr?.metadata?.name ?? '',
    lineage:           spec.blueprintName ?? cr?.metadata?.labels?.[LINEAGE_LABEL] ?? '',
    version:           spec.version ?? '',
    // Defensive default: the Blueprint reconciler (P1-2) stamps phase=Active
    // on the first reconcile pass, but in the brief window between CR create
    // and that pass status.phase may be unset. Treat it as Active for UI purposes.
    phase:             (status.phase as BlueprintPhase) || 'Active',
    useCase:           spec.useCase ?? '',
    description:       spec.description ?? '',
    changeDescription: spec.changeDescription ?? '',
    components:        spec.components ?? [],
    origin,
    vendorChart:       origin === 'WrapsVendorChart' ? source.vendorChartRef : undefined,
    publishedBy:       spec.publishedBy ?? '',
    publishedAt:       spec.publishedAt ?? '',
    deprecation:       status.deprecation,
    raw:               cr
  };
}

export function parseVersion(v: string): [number, number, number] {
  const parts = (v ?? '').split('.').map((n) => Number(n) || 0);

  return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0];
}

// Returns negative when a < b, zero when equal, positive when a > b.
// Compares major.minor.patch numerically; useful for upgrade-target
// filtering ("strictly greater than current") in workloads.vue.
export function compareVersions(a: string, b: string): number {
  const [aa, ab, ac] = parseVersion(a);
  const [ba, bb, bc] = parseVersion(b);

  if (aa !== ba) return aa - ba;
  if (ab !== bb) return ab - bb;

  return ac - bc;
}

export function sortVersionsDesc(vs: BlueprintVersion[]): BlueprintVersion[] {
  return [...vs].sort((x, y) => compareVersions(y.version, x.version));
}

export function groupByLineage(crs: any[]): BlueprintLineage[] {
  const byLineage = new Map<string, BlueprintVersion[]>();

  for (const cr of crs ?? []) {
    const v = toBlueprintVersion(cr);

    if (!v.lineage) continue;
    if (!byLineage.has(v.lineage)) byLineage.set(v.lineage, []);
    byLineage.get(v.lineage)!.push(v);
  }

  const out: BlueprintLineage[] = [];

  for (const [lineage, versions] of byLineage) {
    const sorted = sortVersionsDesc(versions);

    out.push({
      lineage,
      versions:      sorted,
      latestActive:  sorted.find((x) => x.phase === 'Active')
    });
  }

  return out;
}

export function selectDefaultVersion(l: BlueprintLineage): BlueprintVersion {
  return (
    l.versions.find((x) => x.phase === 'Active') ??
    l.versions.find((x) => x.phase === 'Deprecated') ??
    l.versions[0]
  );
}

// Contract: backend Settings controller is expected to emit a condition with
// type `CatalogRefreshReady`. When status === 'False' we surface the
// registry-unreachable banner per SOFTWARE_SPEC §6.
export function readUnreachable(settingsCR: any): boolean {
  const conditions = settingsCR?.status?.conditions ?? [];

  return conditions.some(
    (c: any) => c?.type === 'CatalogRefreshReady' && c?.status === 'False'
  );
}

// TODO P5-6: remove this localStorage hatch once /api/v1/auth/publishers is wired.
// Returns the synchronous override flag (no reactivity; intentionally not named
// `useIsPublisher` to avoid implying a Vue ref / composable contract).
export function readPublisherOverride(): { value: boolean } {
  let override = false;

  try {
    override = globalThis.localStorage?.getItem('aifPublisherOverride') === '1';
  } catch { /* SSR / sandbox without localStorage */ }

  return { value: override };
}
