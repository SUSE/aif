export type BlueprintComponentVendor = 'suse' | 'nvidia';

export interface BlueprintComponent {
  chartRepo:        string;
  chartName:        string;
  chartVersion:     string;
  vendor?:          BlueprintComponentVendor;
  values?:          Record<string, any>;
  targetNamespace?: string;
  releaseName?:     string;
  // chartName of other components in the same blueprint that must be Ready
  // before this one deploys. Mirrors BlueprintComponent.dependsOn (Go).
  dependsOn?:       string[];
}

// BlueprintOrigin (not BlueprintSource) to avoid collision with the existing
// reference interface BlueprintSource in aiworkload-types.ts. JSON field
// stays `source`; only the type identifier differs. Mirrors the Go-side
// rename in aif-operator/api/v1alpha1/blueprint_types.go. Must match the CRD
// enum (pinned by blueprint-crd-parity.test.ts).
export const BLUEPRINT_ORIGINS = ['SUSE', 'Nvidia', 'Partner', 'Custom'] as const;
export type BlueprintOrigin = typeof BLUEPRINT_ORIGINS[number];

export interface BlueprintSpec {
  displayName:  string;
  version:      string;
  description?: string;
  icon?:        string;
  source?:      BlueprintOrigin;
  deprecated?:  boolean;
  components:   BlueprintComponent[];
}

export interface Blueprint {
  apiVersion: string;
  kind:       string;
  metadata:   {
    name:               string;
    labels?:            Record<string, string>;
    creationTimestamp?:  string;
  };
  spec: BlueprintSpec;
}

export interface BlueprintList {
  items: Blueprint[];
}

export const BLUEPRINT_NAME_LABEL    = 'ai-factory.suse.com/blueprint-name';
// Provenance label the Helm chart stamps on product-shipped ("bundled") blueprints.
// Distinct from spec.source (the BlueprintOrigin enum SUSE/Nvidia/Partner/Custom).
export const BLUEPRINT_SOURCE_LABEL   = 'ai-factory.suse.com/source';
export const BLUEPRINT_SOURCE_BUNDLED = 'bundled';
// Label Fleet stamps on Blueprints it syncs from a catalog GitRepo. Mirrors
// the operator's isManagedBlueprint check (blueprint.go).
export const FLEET_BUNDLE_NAME_LABEL = 'fleet.cattle.io/bundle-name';

export const SEMVER_PATTERN = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[a-zA-Z0-9.-]+)?(\+[a-zA-Z0-9.-]+)?$/;

// Kubernetes DNS-1123 label: lowercase alphanumeric and '-', must start and end
// with an alphanumeric, max 63 chars. Used to validate an optional component namespace.
export const DNS_LABEL_PATTERN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

// Helm/Fleet reject release names longer than 53 chars (63 DNS-label max minus
// the 10 bytes Helm reserves for generated suffixes). Mirrors the operator's
// helmReleaseNameMax. Used to validate an optional per-component release name.
export const HELM_RELEASE_NAME_MAX = 53;
