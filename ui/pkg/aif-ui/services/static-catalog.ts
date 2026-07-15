import type { AppCollectionItem } from './app-collection';
import { getCatalog } from '../utils/operator-api';

/**
 * Fetch the static application catalog from the operator (GET /api/v1/catalog).
 *
 * The operator owns the static catalog end to end: it returns the admin-configured
 * remote catalog when set, otherwise its bundled default — already normalized,
 * validated, and library-stamped. This is used only in static mode; dynamic mode
 * discovers apps from chart repositories in the UI and does not call this.
 *
 * Errors propagate so the Apps page can show an error state — there is no UI-side
 * fallback, because the bundled catalog now lives in the operator.
 */
export async function fetchStaticCatalog(): Promise<AppCollectionItem[]> {
  const data = await getCatalog();
  return Array.isArray(data) ? (data as AppCollectionItem[]) : [];
}
