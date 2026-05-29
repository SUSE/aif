import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('blueprints.vue: still exports name BlueprintsPage (P6-1 scaffold contract)', () => {
  const src = read('pages/blueprints.vue');

  assert.match(src, /name:\s*'BlueprintsPage'/);
});

test('blueprints.vue: uses defineComponent (Options-API style, matches settings.vue)', () => {
  const src = read('pages/blueprints.vue');

  assert.match(src, /defineComponent/);
  assert.match(src, /async fetch\s*\(/);
});

test('blueprints.vue: dispatches Steve findAll for blueprint AND settings', () => {
  const src = read('pages/blueprints.vue');

  assert.match(src, /CRD_TYPES\.BLUEPRINT/);
  assert.match(src, /CRD_TYPES\.SETTINGS/);
  assert.match(src, /management\/findAll/);
});

test('blueprints.vue: renders Loading + Banner Rancher primitives', () => {
  const src = read('pages/blueprints.vue');

  assert.match(src, /import Loading from '@shell\/components\/Loading'/);
  assert.match(src, /import \{ Banner \} from '@components\/Banner'/);
});

test('blueprints.vue: surfaces the registry-unreachable banner copy', () => {
  const src = read('pages/blueprints.vue');

  assert.match(src, /aif\.pages\.blueprints\.empty\.unreachable/);
});

test('blueprints.vue: surfaces the no-blueprints empty copy', () => {
  const src = read('pages/blueprints.vue');

  assert.match(src, /aif\.pages\.blueprints\.empty\.none/);
});

// ── BlueprintCard: three-dot menu + Install, no legacy chrome ────────────────
test('BlueprintCard.vue: uses ActionMenuShell three-dot menu', () => {
  const src = read('components/blueprints/BlueprintCard.vue');
  assert.match(src, /ActionMenuShell/);
});

test('BlueprintCard.vue: emits copy, edit, deprecate, delete, deploy', () => {
  const src = read('components/blueprints/BlueprintCard.vue');
  for (const ev of ['copy', 'edit', 'deprecate', 'delete', 'deploy']) {
    assert.match(src, new RegExp(`['"]${ ev }['"]`));
  }
});

test('BlueprintCard.vue: admin-only actions gated on isAdmin', () => {
  const src = read('components/blueprints/BlueprintCard.vue');
  assert.match(src, /isAdmin/);
});

test('BlueprintCard.vue: legacy chrome removed (publisher, Start Bundle, view-versions, withdraw/reactivate)', () => {
  const src = read('components/blueprints/BlueprintCard.vue');
  assert.doesNotMatch(src, /isPublisher|publisher-actions/);
  assert.doesNotMatch(src, /startBundle/i);
  assert.doesNotMatch(src, /view-versions/);
  assert.doesNotMatch(src, /withdraw|reactivate/i);
});

// ── blueprints.vue: admin role, toolbar, toggle, modals ──────────────────────
test('blueprints.vue: checks admin role via globalrolebinding', () => {
  const src = read('pages/blueprints.vue');
  assert.match(src, /globalrolebinding/);
  assert.match(src, /globalRoleName/);
  assert.match(src, /isAdmin/);
});

test('blueprints.vue: toolbar has Create and Refresh', () => {
  const src = read('pages/blueprints.vue');
  assert.match(src, /aif\.pages\.blueprints\.toolbar\.create/);
  assert.match(src, /aif\.pages\.blueprints\.toolbar\.refresh/);
});

test('blueprints.vue: Show deprecated toggle replaces Show withdrawn', () => {
  const src = read('pages/blueprints.vue');
  assert.match(src, /showDeprecated/);
  assert.match(src, /aif\.pages\.blueprints\.toolbar\.showDeprecated/);
  assert.doesNotMatch(src, /showWithdrawn/);
});

test('blueprints.vue: legacy chrome removed (use-case filter, versions panel)', () => {
  const src = read('pages/blueprints.vue');
  assert.doesNotMatch(src, /useCaseFilter/);
  assert.doesNotMatch(src, /BlueprintVersionsPanel/);
});

test('blueprints.vue: imports blueprint write functions', () => {
  const src = read('pages/blueprints.vue');
  assert.match(src, /deprecateBlueprint/);
  assert.match(src, /deleteBlueprint/);
});

test('blueprints.vue: deprecate is a toggle (deprecate/undeprecate)', () => {
  const src = read('pages/blueprints.vue');
  assert.match(src, /currentlyDeprecated/);
  assert.match(src, /aif\.pages\.blueprints\.undeprecateModal\.title/);
});

test('blueprints.vue: delete & deprecate modals warn about active workloads', () => {
  const src = read('pages/blueprints.vue');
  assert.match(src, /listWorkloads/);
  assert.match(src, /activeWorkloads/);
});
