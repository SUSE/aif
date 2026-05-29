import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('BlueprintCard: component exists with correct name', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.match(src, /name:\s*'BlueprintCard'/);
});

test('BlueprintCard: declares lineage, isAdmin, showDeprecated props', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.match(src, /lineage:\s*\{/);
  assert.match(src, /isAdmin:\s*\{/);
  assert.match(src, /showDeprecated:\s*\{/);
});

test('BlueprintCard: imports the picker and pill', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.match(src, /import BlueprintVersionPicker from '\.\/BlueprintVersionPicker\.vue'/);
  assert.match(src, /import BlueprintPhasePill from '\.\/BlueprintPhasePill\.vue'/);
});

test('BlueprintCard: imports selectDefaultVersion from utils', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.match(src, /import.*selectDefaultVersion.*from '\.\.\/\.\.\/utils\/blueprint'/);
});

test('BlueprintCard: emits deploy, copy, edit, deprecate, delete', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.match(src, /emits:\s*\[[^\]]*'deploy'[^\]]*\]/);
  for (const ev of ['copy', 'edit', 'deprecate', 'delete']) {
    assert.match(src, new RegExp(`['"]${ ev }['"]`));
  }
});

test('BlueprintCard: Install button is primary (always enabled)', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.match(src, /aif\.pages\.blueprints\.actions\.install/);
  assert.match(src, /\$emit\(['"]deploy['"]/);
});

test('BlueprintCard: admin-only actions gated on isAdmin via tileActions', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.match(src, /props\.isAdmin/);
  assert.match(src, /tileActions/);
});

test('BlueprintCard: deprecate label flips between Deprecate and Undeprecate', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.match(src, /aif\.pages\.blueprints\.actions\.deprecate/);
  assert.match(src, /aif\.pages\.blueprints\.actions\.undeprecate/);
  assert.match(src, /isDeprecated/);
});

test('BlueprintCard: vendor-chart origin shows chart name in tooltip', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.match(src, /vendorChart/);
  assert.match(src, /WrapsVendorChart/);
});

test('BlueprintCard: legacy chrome removed (publisher, Start Bundle, view-versions, withdraw/reactivate)', () => {
  const src = read('components/blueprints/BlueprintCard.vue');

  assert.doesNotMatch(src, /isPublisher|publisher-actions/);
  assert.doesNotMatch(src, /startBundle/i);
  assert.doesNotMatch(src, /view-versions/);
  assert.doesNotMatch(src, /withdraw|reactivate/i);
});
