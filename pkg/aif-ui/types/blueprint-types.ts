export interface BlueprintComponent {
  chartRepo:    string;
  chartName:    string;
  chartVersion: string;
  values?:      Record<string, any>;
}

export interface BlueprintSpec {
  displayName:  string;
  version:      string;
  description?: string;
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

export const BLUEPRINT_NAME_LABEL    = 'ai-platform.suse.com/blueprint-name';
export const BLUEPRINT_VERSION_LABEL = 'ai-platform.suse.com/blueprint-version';

export const SEMVER_PATTERN = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[a-zA-Z0-9.-]+)?(\+[a-zA-Z0-9.-]+)?$/;
