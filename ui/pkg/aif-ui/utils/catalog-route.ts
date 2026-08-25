// The Apps page library filter ("SUSE AI Library" / "NVIDIA AI Library" / ...)
// travels in the URL rather than in component state.
//
// Apps.vue is a route component: it is torn down when the user opens an app and
// mounted fresh on every return, so a ref alone always comes back at its
// default. services/ui-persist is deliberately a no-op, so localStorage is not
// an option either. The query string is the one carrier that survives the
// wizard round-trip, a reload, and browser back/forward — and it makes a
// filtered catalog linkable as a bonus.
//
// An absent param means "All libraries"; we never write an empty value.

export const LIBRARY_QUERY = 'library';

// Mirrors vue-router's LocationQueryRaw, so the result drops straight into a
// RouteLocationRaw without a cast.
type QueryValue = string | number | null | undefined | (string | number | null | undefined)[];
type RouteQuery = Record<string, QueryValue> | null | undefined;

/** The selected library in `query`, or '' for "All libraries". */
export function readLibraryFilter(query: RouteQuery): string {
  const raw = query?.[LIBRARY_QUERY];
  // vue-router yields an array when a param is repeated (?library=a&library=b).
  const value = Array.isArray(raw) ? raw[0] : raw;
  return typeof value === 'string' ? value : '';
}

/** A copy of `query` carrying `library` — or without the param when it is ''. */
export function withLibraryFilter(query: RouteQuery, library: string): Record<string, QueryValue> {
  const next: Record<string, QueryValue> = { ...(query || {}) };
  if (library) {
    next[LIBRARY_QUERY] = library;
  } else {
    delete next[LIBRARY_QUERY];
  }
  return next;
}
