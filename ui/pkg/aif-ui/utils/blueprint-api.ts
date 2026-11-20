import type {
  Blueprint, BlueprintBasicInfo, BlueprintComponent, BlueprintList, BlueprintOrigin, BlueprintSpec,
} from '../types/blueprint-types';
import { BLUEPRINT_NAME_LABEL } from '../types/blueprint-types';
import { operatorFetch } from './operator-config';

export function listBlueprints(): Promise<BlueprintList> {
  return operatorFetch('/api/v1/blueprints');
}

// createBlueprint POSTs a new blueprint. Pass blueprintName when saving a new
// version of an existing family (its blueprint-name label) so the backend keeps
// all versions grouped under one tile instead of re-deriving the family from the
// display name — which would split bundled blueprints into duplicate tiles.
export function createBlueprint(spec: BlueprintSpec, blueprintName?: string): Promise<Blueprint> {
  return operatorFetch('/api/v1/blueprints', {
    method: 'POST',
    body:   JSON.stringify(blueprintName ? { spec, blueprintName } : { spec }),
  });
}

// buildBlueprintSpec assembles the spec the wizard submits and reviews. Fields
// the wizard has no step for (source, icon) come from the prefill so an Edit
// keeps them; a new blueprint or a Copy (which prefills neither) gets Custom
// with no icon. rollback is written only as 'Disabled' so allowed specs stay
// identical to those of older operators/CRDs.
export function buildBlueprintSpec(
  basicInfo: BlueprintBasicInfo,
  components: BlueprintComponent[],
  prefill?: BlueprintSpec,
): BlueprintSpec {
  return {
    displayName: basicInfo.displayName,
    version:     basicInfo.version,
    description: basicInfo.description || undefined,
    icon:        prefill?.icon,
    source:      prefill?.source ?? 'Custom',
    rollback:    basicInfo.allowRollback ? undefined : 'Disabled',
    components,
  };
}

export function getBlueprint(name: string): Promise<Blueprint> {
  return operatorFetch(`/api/v1/blueprints/${ encodeURIComponent(name) }`);
}

export function deleteBlueprint(name: string): Promise<void> {
  return operatorFetch(`/api/v1/blueprints/${ encodeURIComponent(name) }`, {
    method: 'DELETE',
  });
}

export async function updateBlueprintDeprecated(name: string, deprecated: boolean): Promise<Blueprint> {
  const bp = await getBlueprint(name);
  return operatorFetch(`/api/v1/blueprints/${ encodeURIComponent(name) }`, {
    method: 'PUT',
    body:   JSON.stringify({ spec: { ...bp.spec, deprecated } }),
  });
}

// sourceFor returns the blueprint's source for display purposes.
// Blueprints created before this field existed have spec.source === undefined
// and are treated as 'Custom'.
export function sourceFor(bp: Blueprint): BlueprintOrigin {
  return bp.spec.source ?? 'Custom';
}

// blueprintCRName derives the CR name matching the backend logic.
// Build-metadata suffix (+...) is stripped since '+' is illegal in Kubernetes names.
// "My AI Stack", "1.0.0" → "my-ai-stack-1-0-0"
export function blueprintCRName(displayName: string, version: string): string {
  const slug = slugifyBlueprintName(displayName);
  // Strip build metadata before hyphenating — matches Go backend bpCRName behavior.
  const ver  = version.replace(/\+.*$/, '').replace(/\./g, '-');
  return `${ slug }-${ ver }`;
}

// Must stay in sync with the backend slugifyBlueprintName in
// operator/internal/api/blueprint.go — blueprint tile grouping and CR-name
// derivation depend on both producing identical output.
export function slugifyBlueprintName(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

// blueprintFamily is the family key of a Blueprint CR: its BLUEPRINT_NAME_LABEL,
// falling back to the slugified display name for unlabelled CRs.
export function blueprintFamily(bp: Blueprint): string {
  return bp.metadata.labels?.[BLUEPRINT_NAME_LABEL] || slugifyBlueprintName(bp.spec.displayName);
}

// groupBlueprintsByFamily groups Blueprint CRs by BLUEPRINT_NAME_LABEL, each group semver-sorted descending.
export function groupBlueprintsByFamily(items: Blueprint[]): Map<string, Blueprint[]> {
  const map = new Map<string, Blueprint[]>();
  for (const bp of items) {
    const family = blueprintFamily(bp);
    const group  = map.get(family) || [];
    group.push(bp);
    map.set(family, group);
  }
  for (const [key, group] of map.entries()) {
    map.set(key, group.slice().sort((a, b) => semverCompare(b.spec.version, a.spec.version)));
  }
  return map;
}

// findBlueprint resolves the exact Blueprint CR backing an AIWorkload's blueprint
// source: the CR whose family (see blueprintFamily) is `name` and whose
// spec.version equals `version`. Returns null when there is no such CR — e.g. a
// custom blueprint or one not present in the loaded list.
export function findBlueprint(items: Blueprint[], name: string, version: string): Blueprint | null {
  if (!name || !version) return null;
  return items.find((bp) => bp.spec.version === version && blueprintFamily(bp) === name) || null;
}

// latestVersion returns the semver-greatest CR from a family group (assumes group is sorted descending).
export function latestVersion(versions: Blueprint[]): Blueprint {
  return versions[0];
}

// semverCompare returns negative if a < b, 0 if equal, positive if a > b.
// Build metadata is ignored and pre-releases order per semver §11.3-11.4.
export function semverCompare(a: string, b: string): number {
  const parse = (v: string) => {
    const main = v.split('+')[0];
    const dash = main.indexOf('-');

    return {
      core: (dash === -1 ? main : main.slice(0, dash)).split('.').map(Number),
      pre:  dash === -1 ? [] : main.slice(dash + 1).split('.'),
    };
  };
  const pa = parse(a);
  const pb = parse(b);
  for (let i = 0; i < 3; i++) {
    const diff = (pa.core[i] || 0) - (pb.core[i] || 0);
    if (diff !== 0) return diff;
  }
  if (pa.pre.length && !pb.pre.length) return -1;
  if (!pa.pre.length && pb.pre.length) return 1;
  for (let i = 0; i < Math.max(pa.pre.length, pb.pre.length); i++) {
    if (i >= pa.pre.length) return -1;
    if (i >= pb.pre.length) return 1;
    const x = pa.pre[i];
    const y = pb.pre[i];
    const xNum = /^\d+$/.test(x);
    const yNum = /^\d+$/.test(y);
    if (xNum && yNum && Number(x) !== Number(y)) return Number(x) - Number(y);
    if (xNum !== yNum) return xNum ? -1 : 1;
    if (!xNum && x !== y) return x < y ? -1 : 1;
  }
  return 0;
}
