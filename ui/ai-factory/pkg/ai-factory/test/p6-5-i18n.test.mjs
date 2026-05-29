import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('en-us.yaml exposes all aif.pages.blueprints keys after reference-parity rework', () => {
  const l10n = read('l10n/en-us.yaml');

  const requiredLeaves = [
    // phase
    'active:', 'deprecated:', 'withdrawn:',
    // toolbar
    'search:', 'showDeprecated:', 'create:', 'refresh:',
    // card
    'useCase:', 'origin:', 'published:', 'wrapsVendorChart:',
    'components:', 'publishedBy:',
    // actions
    'install:', 'copy:', 'edit:', 'deprecate:', 'undeprecate:', 'delete:',
    // workload warning
    'activeWorkloadsWarning:',
    // empty states
    'none:', 'noResults:', 'unreachable:', 'loadError:'
  ];

  for (const leaf of requiredLeaves) {
    assert.match(l10n, new RegExp(leaf), `Missing i18n leaf: ${ leaf }`);
  }
});

test('en-us.yaml drops obsolete legacy keys (publisher, startBundle, deployComingSoon, etc.)', () => {
  const l10n = read('l10n/en-us.yaml');

  // The blueprints subtree must not include legacy keys that the rework removed.
  const blueprintsBlockMatch = l10n.match(/\n    blueprints:[\s\S]*?(?=\n    [a-zA-Z]|\Z)/);

  assert.ok(blueprintsBlockMatch, 'blueprints subtree not found in en-us.yaml');

  const block = blueprintsBlockMatch[0];

  assert.doesNotMatch(block, /startBundle/);
  assert.doesNotMatch(block, /deployComingSoon/);
  assert.doesNotMatch(block, /publisherLabel|publisherEndpointComingSoon|publisherRoleRequired/);
  assert.doesNotMatch(block, /showWithdrawn:/);
  assert.doesNotMatch(block, /useCaseAll:/);
  assert.doesNotMatch(block, /viewVersions:/);
  assert.doesNotMatch(block, /versionsPanel:/);
});

test('en-us.yaml keeps the blueprints.title key', () => {
  const l10n = read('l10n/en-us.yaml');

  // Existing key from the placeholder page — must survive the rewrite
  assert.match(l10n, /blueprints:\s*\n\s+title:/);
});
