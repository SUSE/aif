export interface BlueprintCatalogMember {
  name:      string;   // matches a Blueprint's ai-factory.suse.com/blueprint-name (family) label
  featured?: boolean;
  category?: string;
}

export interface BlueprintCatalogSpec {
  displayName?: string;
  description?: string;
  icon?:        string;
  maintainer?:  { name?: string; url?: string };
  categories?:  string[];
  blueprints?:  BlueprintCatalogMember[];
}

export interface BlueprintCatalog {
  apiVersion: string;
  kind:       string;
  metadata:   { name: string; labels?: Record<string, string> };
  spec:       BlueprintCatalogSpec;
}

export interface BlueprintCatalogList {
  items: BlueprintCatalog[];
}

// The built-in default catalog's name (matches operator BlueprintCatalogDefault).
export const CATALOG_DEFAULT_NAME = 'suse-default';
