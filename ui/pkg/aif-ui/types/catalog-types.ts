export interface CatalogMember {
  name:      string;   // matches a Blueprint's ai-factory.suse.com/blueprint-name (family) label
  featured?: boolean;
  category?: string;
}

export interface CatalogSpec {
  displayName?: string;
  description?: string;
  icon?:        string;
  maintainer?:  { name?: string; url?: string };
  categories?:  string[];
  blueprints?:  CatalogMember[];
}

export interface Catalog {
  apiVersion: string;
  kind:       string;
  metadata:   { name: string; labels?: Record<string, string> };
  spec:       CatalogSpec;
}

export interface CatalogList {
  items: Catalog[];
}

// The built-in default catalog's name (matches operator BlueprintCatalogDefault).
export const CATALOG_DEFAULT_NAME = 'suse-default';
