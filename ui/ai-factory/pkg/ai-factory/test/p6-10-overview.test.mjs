import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('overview.vue: exports name OverviewPage', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /name:\s*'OverviewPage'/);
});

test('overview.vue: uses defineComponent and async fetch', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /defineComponent/);
  assert.match(src, /async fetch\s*\(/);
});

test('overview.vue: calls listWorkloads and uses Steve for blueprints', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /listWorkloads/);
  assert.match(src, /CRD_TYPES\.BLUEPRINT/);
  assert.match(src, /management\/findAll/);
});

test('overview.vue: renders 4 summary card keys', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /aif\.pages\.overview\.cards\.totalWorkloads/);
  assert.match(src, /aif\.pages\.overview\.cards\.running/);
  assert.match(src, /aif\.pages\.overview\.cards\.withIssues/);
  assert.match(src, /aif\.pages\.overview\.cards\.activeBlueprints/);
});

test('overview.vue: renders Recent Workloads panel', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /aif\.pages\.overview\.recentWorkloads\.title/);
});

test('overview.vue: renders Active Blueprints panel', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /aif\.pages\.overview\.activeBlueprints\.title/);
});

test('overview.vue: has 10-second auto-refresh', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /10[_\s]*[*]\s*1000|10000/);
  assert.match(src, /setInterval/);
});

test('overview.vue: has Quick Actions section', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /aif\.pages\.overview\.quickActions/);
});

test('overview.vue: Active Blueprints grouped by lineage (latest per lineage)', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /groupByLineage/);
  assert.match(src, /latestActive/);
});

test('overview.vue: Recent Workloads shows a source label, not just kind', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /sourceLabel/);
});

test('overview.vue: background poll is silent (does not reuse loadData error path)', () => {
  const src = read('pages/overview.vue');
  assert.match(src, /silentRefresh/);
  assert.match(src, /setInterval\([^)]*silentRefresh/);
});
