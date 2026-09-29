import type { BlueprintCatalog, BlueprintCatalogList } from '../types/catalog-types';
import { operatorFetch } from './operator-config';

// listCatalogs returns the BlueprintCatalog CRs (blueprint catalogs) from the operator.
export function listCatalogs(): Promise<BlueprintCatalogList> {
  return operatorFetch('/api/v1/blueprint-catalogs');
}

// catalogDisplayName is the human label for a catalog (falls back to its name).
export function catalogDisplayName(cat: BlueprintCatalog): string {
  return cat.spec.displayName?.trim() || cat.metadata.name;
}

// familiesInCatalog is the set of blueprint family names a catalog offers
// (keyed by the same family key groupBlueprintsByFamily produces).
export function familiesInCatalog(cat: BlueprintCatalog): Set<string> {
  return new Set((cat.spec.blueprints ?? []).map(b => b.name));
}
