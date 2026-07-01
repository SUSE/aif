import type { Blueprint, BlueprintList, BlueprintOrigin, BlueprintSpec } from '../types/blueprint-types';
import { BLUEPRINT_NAME_LABEL } from '../types/blueprint-types';
import { operatorFetch } from './operator-config';

export function listBlueprints(): Promise<BlueprintList> {
  return operatorFetch('/api/v1/blueprints');
}

export function createBlueprint(spec: BlueprintSpec): Promise<Blueprint> {
  return operatorFetch('/api/v1/blueprints', {
    method: 'POST',
    body:   JSON.stringify({ spec }),
  });
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

export function slugifyBlueprintName(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

// groupBlueprintsByFamily groups Blueprint CRs by BLUEPRINT_NAME_LABEL, each group semver-sorted descending.
export function groupBlueprintsByFamily(items: Blueprint[]): Map<string, Blueprint[]> {
  const map = new Map<string, Blueprint[]>();
  for (const bp of items) {
    const family = bp.metadata.labels?.[BLUEPRINT_NAME_LABEL] || slugifyBlueprintName(bp.spec.displayName);
    const group  = map.get(family) || [];
    group.push(bp);
    map.set(family, group);
  }
  for (const [key, group] of map.entries()) {
    map.set(key, group.slice().sort((a, b) => semverCompare(b.spec.version, a.spec.version)));
  }
  return map;
}

// latestVersion returns the semver-greatest CR from a family group (assumes group is sorted descending).
export function latestVersion(versions: Blueprint[]): Blueprint {
  return versions[0];
}

// semverCompare returns negative if a < b, 0 if equal, positive if a > b.
// Per semver §11.3, a pre-release version has lower precedence than the release it precedes.
function semverCompare(a: string, b: string): number {
  const dashA = a.indexOf('-');
  const dashB = b.indexOf('-');
  const aCoreStr = dashA === -1 ? a : a.slice(0, dashA);
  const bCoreStr = dashB === -1 ? b : b.slice(0, dashB);
  const aPreStr  = dashA === -1 ? '' : a.slice(dashA + 1);
  const bPreStr  = dashB === -1 ? '' : b.slice(dashB + 1);
  const pa = aCoreStr.split('.').map(Number);
  const pb = bCoreStr.split('.').map(Number);
  for (let i = 0; i < 3; i++) {
    const diff = (pa[i] || 0) - (pb[i] || 0);
    if (diff !== 0) return diff;
  }
  if (aPreStr && !bPreStr) return -1;
  if (!aPreStr && bPreStr) return 1;
  return 0;
}
