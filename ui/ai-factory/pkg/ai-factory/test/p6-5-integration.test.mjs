import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync, existsSync } from 'node:fs';

const here = (p) => new URL(`../${ p }`, import.meta.url);
const read = (p) => readFileSync(here(p), 'utf8');

test('blueprints.vue imports resolve to files that exist', () => {
  read('pages/blueprints.vue');                               // throws if missing
  for (const p of [
    'components/blueprints/BlueprintCard.vue',
    'components/blueprints/BlueprintVersionPicker.vue',
    'components/blueprints/BlueprintPhasePill.vue',
    'utils/blueprint.ts',
    'utils/operator-api.ts'
  ]) {
    assert.ok(existsSync(here(p)), `Missing ${ p }`);
  }
});

test('BlueprintVersionsPanel.vue is removed (reference-parity rework)', () => {
  assert.ok(!existsSync(here('components/blueprints/BlueprintVersionsPanel.vue')),
    'BlueprintVersionsPanel.vue should have been deleted');
});

test('all aif.pages.blueprints.* keys used in templates exist in en-us.yaml', () => {
  const l10n = read('l10n/en-us.yaml');
  const sources = [
    read('pages/blueprints.vue'),
    read('components/blueprints/BlueprintCard.vue'),
    read('components/blueprints/BlueprintVersionPicker.vue'),
    read('components/blueprints/BlueprintPhasePill.vue')
  ];
  const usedKeys = new Set();

  for (const src of sources) {
    for (const m of src.matchAll(/t\(['"]([^'"]+)['"]/g)) {
      usedKeys.add(m[1]);
    }
  }

  // Lightweight smoke test: matches the leaf of each used key in the YAML
  // text. Accepts false positives (e.g. a YAML comment containing the leaf
  // word would satisfy the regex) in exchange for not pulling in js-yaml as
  // a test dep. Matches the pattern used in p6-7-integration.test.mjs.
  for (const key of usedKeys) {
    if (!key.startsWith('aif.pages.blueprints.')) continue;
    const leaf = key.split('.').pop();

    assert.match(l10n, new RegExp(`${ leaf }:`), `Missing i18n key: ${ key }`);
  }
});

test('BlueprintCard emits the reference-parity action set; page binds them', () => {
  const card = read('components/blueprints/BlueprintCard.vue');
  const page = read('pages/blueprints.vue');

  for (const ev of ['deploy', 'copy', 'edit', 'deprecate', 'delete']) {
    assert.match(card, new RegExp(`['"]${ ev }['"]`));
    assert.match(page, new RegExp(`@${ ev }=`));
  }
});

test('BlueprintVersionPicker emits update:modelValue; card binds it', () => {
  const picker = read('components/blueprints/BlueprintVersionPicker.vue');
  const card   = read('components/blueprints/BlueprintCard.vue');

  assert.match(picker, /emits:\s*\[\s*'update:modelValue'\s*\]/);
  assert.match(card, /@update:model-value=/);
});

test('config/types and routing did NOT drift in P6-5', () => {
  // Blueprint must still be registered with the management store
  const product = read('config/aif-product.ts');
  const routing = read('routing/index.ts');

  assert.match(product, /CRD_TYPES\.BLUEPRINT/);
  assert.match(routing, /PAGE_IDS\.BLUEPRINTS/);
  assert.match(routing, /pages\/blueprints\.vue/);
});
