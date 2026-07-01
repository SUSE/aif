export type BlueprintComponentVendor = 'suse' | 'nvidia';

export interface BlueprintComponent {
  chartRepo:        string;
  chartName:        string;
  chartVersion:     string;
  vendor?:          BlueprintComponentVendor;
  values?:          Record<string, any>;
  targetNamespace?: string;
}

// BlueprintOrigin (not BlueprintSource) to avoid collision with the existing
// reference interface BlueprintSource in aiworkload-types.ts. JSON field
// stays `source`; only the type identifier differs. Mirrors the Go-side
// rename in aif-operator/api/v1alpha1/blueprint_types.go.
export type BlueprintOrigin = 'SUSE' | 'Nvidia' | 'Custom';

export interface BlueprintSpec {
  displayName:  string;
  version:      string;
  description?: string;
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
export const BLUEPRINT_VERSION_LABEL = 'ai-factory.suse.com/blueprint-version';

export const SEMVER_PATTERN = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[a-zA-Z0-9.-]+)?(\+[a-zA-Z0-9.-]+)?$/;

// Kubernetes DNS-1123 label: lowercase alphanumeric and '-', must start and end
// with an alphanumeric, max 63 chars. Used to validate an optional component namespace.
export const DNS_LABEL_PATTERN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
