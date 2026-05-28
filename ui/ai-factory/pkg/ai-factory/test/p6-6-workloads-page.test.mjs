import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('workloads.vue: exports name WorkloadsPage', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /name:\s*'WorkloadsPage'/);
});

test('workloads.vue: uses defineComponent and async fetch', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /defineComponent/);
  assert.match(src, /async fetch\s*\(/);
});

test('workloads.vue: calls listWorkloads from operator-api', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /listWorkloads/);
  assert.match(src, /operator-api/);
});

test('workloads.vue: renders State, Name, Namespace, Source columns', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /aif\.pages\.workloads\.columns\.state/);
  assert.match(src, /aif\.pages\.workloads\.columns\.name/);
  assert.match(src, /aif\.pages\.workloads\.columns\.namespace/);
  assert.match(src, /aif\.pages\.workloads\.columns\.source/);
});

test('workloads.vue: has delete action with confirmation modal', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /deleteWorkload/);
  assert.match(src, /confirmDelete|deleteConfirm/);
});

test('workloads.vue: has 10-second silent auto-refresh', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /10[_\s]*[*]\s*1000|10000/);
  // The background poll must be silent (does not flash the error banner).
  assert.match(src, /silentRefresh/);
  assert.match(src, /setInterval\([^)]*silentRefresh/);
});

test('workloads.vue: has manual Refresh button', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /aif\.pages\.workloads\.toolbar\.refresh/);
});

test('workloads.vue: empty state key present', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /aif\.pages\.workloads\.empty/);
});

test('workloads.vue: renders Deploy and Version columns', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /aif\.pages\.workloads\.columns\.deploy/);
  assert.match(src, /aif\.pages\.workloads\.columns\.version/);
});

test('workloads.vue: Manage button shown only for App source workloads', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /aif\.pages\.workloads\.actions\.manage/);
  // Accept either `source.kind` or `source?.kind` — the optional-chain form
  // is preferred but the gate logic is what we are pinning here.
  assert.match(src, /source\??\.kind.*['"']App['"]|['"']App['"].*source\??\.kind/);
});

test('workloads.vue: Manage button disabled when phase is not Running', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /phase.*Running|Running.*phase/);
  assert.match(src, /:disabled/);
});

test('workloads.vue: Manage navigates to the workload-manage route with ns + name params', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /workload-manage/);
  assert.match(src, /metadata\.namespace/);
  assert.match(src, /metadata\.name/);
});

test('workloads.vue: has search input', () => {
  const src = read('pages/workloads.vue');
  assert.match(src, /aif\.pages\.workloads\.toolbar\.search/);
});
