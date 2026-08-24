import type {
  Application,
  Blueprint,
  BlueprintComponent,
  BlueprintList,
  BlueprintOrigin,
  BlueprintSpec,
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
  const persistedSpec = prepareBlueprintSpecForWrite(spec);
  return operatorFetch('/api/v1/blueprints', {
    method: 'POST',
    body:   JSON.stringify(blueprintName ? { spec: persistedSpec, blueprintName } : { spec: persistedSpec }),
  });
}

export function getApplication(name: string): Promise<Application> {
  return operatorFetch(`/api/v1/applications/${ encodeURIComponent(name) }`);
}

function getStoredBlueprint(name: string): Promise<Blueprint> {
  return operatorFetch(`/api/v1/blueprints/${ encodeURIComponent(name) }`);
}

export async function getBlueprint(name: string): Promise<Blueprint> {
  const blueprint = await getStoredBlueprint(name);
  return resolveApplicationComponents(blueprint);
}

export function deleteBlueprint(name: string): Promise<void> {
  return operatorFetch(`/api/v1/blueprints/${ encodeURIComponent(name) }`, {
    method: 'DELETE',
  });
}

export async function updateBlueprintDeprecated(name: string, deprecated: boolean): Promise<Blueprint> {
  // Deprecation is metadata on the Blueprint spec and must remain possible even
  // when a referenced Application is temporarily missing. Read the stored form
  // directly instead of resolving deployment coordinates for this write.
  const bp = await getStoredBlueprint(name);
  return operatorFetch(`/api/v1/blueprints/${ encodeURIComponent(name) }`, {
    method: 'PUT',
    body:   JSON.stringify({ spec: prepareBlueprintSpecForWrite({ ...bp.spec, deprecated }) }),
  });
}

// resolveApplicationComponents creates the UI view expected by the existing
// Blueprint wizards without mutating the API object or persisting derived
// coordinates. One request is issued per distinct logical Application.
export async function resolveApplicationComponents(blueprint: Blueprint): Promise<Blueprint> {
  const cache = new Map<string, Promise<Application>>();
  const resolveApplication = (name: string): Promise<Application> => {
    let pending = cache.get(name);
    if (!pending) {
      pending = getApplication(name);
      cache.set(name, pending);
    }
    return pending;
  };

  const components = await Promise.all(blueprint.spec.components.map(async(component): Promise<BlueprintComponent> => {
    if (!component.applicationRef) return component;

    const application = await resolveApplication(component.applicationRef.name);
    const chart = application?.spec?.chart;
    if (!chart?.name || !chart?.sourceRef) {
      throw new Error(`Application "${ component.applicationRef.name }" has no usable chart mapping.`);
    }

    return {
      ...component,
      chartRepo:    chart.sourceRef,
      chartName:    chart.name,
      chartVersion: component.applicationRef.version,
      vendor:       application.spec.credentialProfile ?? 'suse',
    };
  }));

  return {
    ...blueprint,
    spec: { ...blueprint.spec, components },
  };
}

// prepareBlueprintSpecForWrite removes the coordinates used only by the UI's
// resolved view. If a user selects another version while editing, that version
// is copied back into the logical requirement before the direct fields are
// removed. Legacy direct-chart components pass through unchanged.
export function prepareBlueprintSpecForWrite(spec: BlueprintSpec): Record<string, unknown> {
  return {
    ...spec,
    components: spec.components.map((component) => {
      if (!component.applicationRef) return component;

      const chartVersion = component.chartVersion;
      const logicalComponent: Record<string, unknown> = { ...component };
      delete logicalComponent.chartRepo;
      delete logicalComponent.chartName;
      delete logicalComponent.chartVersion;
      delete logicalComponent.vendor;
      return {
        ...logicalComponent,
        applicationRef: {
          ...component.applicationRef,
          version: chartVersion || component.applicationRef.version,
        },
      };
    }),
  };
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
