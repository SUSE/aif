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

test('blueprints.vue: imports the gallery components and helpers', () => {
  const src = read('pages/blueprints.vue');

  assert.match(src, /import BlueprintCard from '\.\.\/components\/blueprints\/BlueprintCard\.vue'/);
  assert.match(src, /import BlueprintVersionsPanel from '\.\.\/components\/blueprints\/BlueprintVersionsPanel\.vue'/);
  assert.match(src, /import.*groupByLineage.*readUnreachable.*useIsPublisher[\s\S]*from '\.\.\/utils\/blueprint'/);
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

test('blueprints.vue: has a search input and show-withdrawn toggle', () => {
  const src = read('pages/blueprints.vue');

  assert.match(src, /aif\.pages\.blueprints\.toolbar\.search/);
  assert.match(src, /aif\.pages\.blueprints\.toolbar\.showWithdrawn/);
});

test('blueprints.vue: listens for the card view-versions event', () => {
  const src = read('pages/blueprints.vue');

  assert.match(src, /@view-versions/);
});

test('blueprints.vue: hides all-Withdrawn lineages when showWithdrawn=false', () => {
  const src = read('pages/blueprints.vue');

  // The filter must consider the showWithdrawn flag AND every-version-Withdrawn case
  assert.match(src, /showWithdrawn/);
  assert.match(src, /every[\s\S]*Withdrawn|Withdrawn[\s\S]*every/);
});
