import type { AppCollectionItem } from './app-collection';
import { getCatalog } from '../utils/operator-api';

/**
 * Fetch the static application catalog from the operator (GET /api/v1/catalog).
 *
 * The operator owns the static catalog end to end: it returns the admin-configured
 * remote catalog when set, otherwise its bundled default — already normalized,
 * validated, and library-stamped. Static mode uses this as the whole app list;
 * dynamic mode still calls it (via fetchCuratedOverlayOrEmpty) to overlay curated
 * metadata onto the apps it discovers from chart repositories.
 *
 * Errors propagate so the Apps page can show an error state — there is no UI-side
 * fallback, because the bundled catalog now lives in the operator.
 */
export async function fetchStaticCatalog(): Promise<AppCollectionItem[]> {
  const data = await getCatalog();
  return Array.isArray(data) ? (data as AppCollectionItem[]) : [];
}
