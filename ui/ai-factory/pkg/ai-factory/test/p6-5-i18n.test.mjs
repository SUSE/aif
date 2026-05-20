import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('P6-5 en-us.yaml exposes all aif.pages.blueprints keys', () => {
  const l10n = read('l10n/en-us.yaml');

  const requiredLeaves = [
    // header
    'lineages:', 'versions:',
    // toolbar
    'search:', 'useCaseAll:', 'showWithdrawn:',
    // card
    'useCase:', 'origin:', 'published:', 'wrapsVendorChart:',
    'components:', 'publishedBy:', 'viewVersions:',
    // actions
    'deploy:', 'deployComingSoon:', 'startBundle:', 'startBundleComingSoon:',
    'deprecate:', 'withdraw:', 'reactivate:',
    'publisherEndpointComingSoon:', 'publisherRoleRequired:',
    // panel
    'changeDescription:',
    // empty states
    'none:', 'noResults:', 'unreachable:', 'loadError:'
  ];

  for (const leaf of requiredLeaves) {
    assert.match(l10n, new RegExp(leaf), `Missing i18n leaf: ${ leaf }`);
  }
});

test('P6-5 en-us.yaml keeps the blueprints.title key', () => {
  const l10n = read('l10n/en-us.yaml');

  // Existing key from the placeholder page — must survive the rewrite
  assert.match(l10n, /blueprints:\s*\n\s+title:/);
});
