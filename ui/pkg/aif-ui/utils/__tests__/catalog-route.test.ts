import { describe, it, expect } from 'vitest';
import { LIBRARY_QUERY, readLibraryFilter, withLibraryFilter } from '../catalog-route';

describe('readLibraryFilter', () => {
  it('reads the selected library from a route query', () => {
    expect(readLibraryFilter({ [LIBRARY_QUERY]: 'nvidia' })).toBe('nvidia');
  });

  it('returns "" ("All libraries") when the query carries no library', () => {
    expect(readLibraryFilter({ n: 'dps' })).toBe('');
  });

  it('returns "" for a missing or malformed query', () => {
    expect(readLibraryFilter(undefined)).toBe('');
    expect(readLibraryFilter(null)).toBe('');
    expect(readLibraryFilter({ [LIBRARY_QUERY]: null })).toBe('');
  });

  it('takes the first value when the router repeats the param', () => {
    expect(readLibraryFilter({ [LIBRARY_QUERY]: ['nvidia', 'suse-ai'] })).toBe('nvidia');
  });
});

describe('withLibraryFilter', () => {
  it('adds the library to a query, preserving the other params', () => {
    expect(withLibraryFilter({ n: 'dps', repo: 'nvidia-charts' }, 'nvidia'))
      .toEqual({ n: 'dps', repo: 'nvidia-charts', [LIBRARY_QUERY]: 'nvidia' });
  });

  it('drops the param for "All libraries" so the URL stays clean', () => {
    expect(withLibraryFilter({ n: 'dps', [LIBRARY_QUERY]: 'nvidia' }, ''))
      .toEqual({ n: 'dps' });
  });

  it('does not mutate the query it was given', () => {
    const query = { n: 'dps' };
    withLibraryFilter(query, 'nvidia');
    expect(query).toEqual({ n: 'dps' });
  });

  it('tolerates a missing query', () => {
    expect(withLibraryFilter(undefined, 'nvidia')).toEqual({ [LIBRARY_QUERY]: 'nvidia' });
  });
});

describe('library filter round-trip (SUSEAI-855)', () => {
  // The regression: picking NVIDIA on the Apps page, opening an app, then
  // cancelling must land back on the NVIDIA library — not the default one.
  it('survives Apps page -> install wizard -> cancel', () => {
    const selectedOnAppsPage = 'nvidia';

    // Apps.vue hands the filter to the install route alongside the app name.
    const installQuery = withLibraryFilter({ n: 'dps' }, selectedOnAppsPage);

    // AppWizard's Cancel echoes it back onto the apps route.
    const appsQuery = withLibraryFilter({}, readLibraryFilter(installQuery));

    // Apps.vue seeds its filter from the query it is mounted with.
    expect(readLibraryFilter(appsQuery)).toBe('nvidia');
  });

  it('round-trips "All libraries" as no param at all', () => {
    const installQuery = withLibraryFilter({ n: 'dps' }, '');
    expect(installQuery).not.toHaveProperty(LIBRARY_QUERY);
    expect(readLibraryFilter(installQuery)).toBe('');
  });
});
