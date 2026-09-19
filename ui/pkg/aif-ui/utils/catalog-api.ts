import type { Catalog, CatalogList } from '../types/catalog-types';
import { operatorFetch } from './operator-config';

// listCatalogs returns the Catalog CRs (blueprint catalogs) from the operator.
export function listCatalogs(): Promise<CatalogList> {
  return operatorFetch('/api/v1/catalogs');
}

// catalogDisplayName is the human label for a catalog (falls back to its name).
export function catalogDisplayName(cat: Catalog): string {
  return cat.spec.displayName?.trim() || cat.metadata.name;
}

// familiesInCatalog is the set of blueprint family names a catalog offers
// (keyed by the same family key groupBlueprintsByFamily produces).
export function familiesInCatalog(cat: Catalog): Set<string> {
  return new Set((cat.spec.blueprints ?? []).map(b => b.name));
}
