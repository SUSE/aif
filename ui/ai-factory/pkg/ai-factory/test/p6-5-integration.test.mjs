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
    'components/blueprints/BlueprintVersionsPanel.vue',
    'utils/blueprint.ts'
  ]) {
    assert.ok(existsSync(here(p)), `Missing ${ p }`);
  }
});

test('all aif.pages.blueprints.* keys used in templates exist in en-us.yaml', () => {
  const l10n = read('l10n/en-us.yaml');
  const sources = [
    read('pages/blueprints.vue'),
    read('components/blueprints/BlueprintCard.vue'),
    read('components/blueprints/BlueprintVersionPicker.vue'),
    read('components/blueprints/BlueprintPhasePill.vue'),
    read('components/blueprints/BlueprintVersionsPanel.vue')
  ];
  const usedKeys = new Set();

  for (const src of sources) {
    for (const m of src.matchAll(/t\(['"]([^'"]+)['"]/g)) {
      usedKeys.add(m[1]);
    }
  }

  for (const key of usedKeys) {
    if (!key.startsWith('aif.pages.blueprints.')) continue;
    const leaf = key.split('.').pop();

    assert.match(l10n, new RegExp(`${ leaf }:`), `Missing i18n key: ${ key }`);
  }
});

test('BlueprintCard emits view-versions; page listens for it', () => {
  const card = read('components/blueprints/BlueprintCard.vue');
  const page = read('pages/blueprints.vue');

  assert.match(card, /emits:\s*\[[^\]]*'view-versions'/);
  assert.match(page, /@view-versions=/);
});

test('BlueprintVersionPicker emits update:modelValue; card binds it', () => {
  const picker = read('components/blueprints/BlueprintVersionPicker.vue');
  const card   = read('components/blueprints/BlueprintCard.vue');

  assert.match(picker, /emits:\s*\[\s*'update:modelValue'\s*\]/);
  assert.match(card, /@update:model-value=/);
});

test('BlueprintVersionsPanel emits close; page binds it', () => {
  const panel = read('components/blueprints/BlueprintVersionsPanel.vue');
  const page  = read('pages/blueprints.vue');

  assert.match(panel, /emits:\s*\[\s*'close'\s*\]/);
  assert.match(page, /@close=/);
});

test('config/types and routing did NOT drift in P6-5', () => {
  // Blueprint must still be registered with the management store
  const product = read('config/aif-product.ts');
  const routing = read('routing/index.ts');

  assert.match(product, /CRD_TYPES\.BLUEPRINT/);
  assert.match(routing, /PAGE_IDS\.BLUEPRINTS/);
  assert.match(routing, /pages\/blueprints\.vue/);
});
