import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('manage.vue: exports name ManagePage', () => {
  const src = read('pages/manage.vue');

  assert.match(src, /name:\s*'ManagePage'/);
});

test('manage.vue: calls getWorkload to pre-populate form', () => {
  const src = read('pages/manage.vue');

  assert.match(src, /getWorkload/);
});

test('manage.vue: calls putWorkload on apply (PUT full-replace semantics)', () => {
  const src = read('pages/manage.vue');

  assert.match(src, /putWorkload/);
  // Guard against the stale plan name — we use PUT, not PATCH.
  assert.doesNotMatch(src, /patchWorkload/);
});

test('manage.vue: reads ns and name from route params', () => {
  const src = read('pages/manage.vue');

  assert.match(src, /params\.ns|route\.params\.ns/);
  assert.match(src, /params\.name|route\.params\.name/);
});

test('manage.vue: has Apply button with manage l10n key', () => {
  const src = read('pages/manage.vue');

  assert.match(src, /aif\.pages\.wizards\.manage\.apply/);
});

test('manage.vue: keys valueOverrides by the workload name (not empty string)', () => {
  const src = read('pages/manage.vue');

  assert.match(src, /\[\s*this\.\$route\.params\.name\s*\]/);
  assert.doesNotMatch(src, /valueOverrides\s*=\s*\{\s*''\s*:/);
});

test('manage.vue: deep-clones the spec before mutating (avoids Vue-reactive aliasing)', () => {
  const src = read('pages/manage.vue');

  assert.match(src, /JSON\.parse\(JSON\.stringify\([^)]*spec/);
});

test('manage.vue: imports putWorkload from the operator-api helper', () => {
  const src = read('pages/manage.vue');

  assert.match(src, /import\s*\{[^}]*putWorkload[^}]*\}\s*from\s*['"][^'"]*operator-api['"]/);
});

test('manage.vue: shows Loading component while fetching', () => {
  const src = read('pages/manage.vue');

  assert.match(src, /import\s+Loading\s+from\s+['"]@shell\/components\/Loading['"]/);
  assert.match(src, /<Loading\b/);
});

test('manage.vue: refuses non-App workloads with appOnly l10n key', () => {
  const src = read('pages/manage.vue');

  // Guard: source.kind must equal 'App' before the rest of fetch() proceeds.
  assert.match(src, /source[?.\s]*kind\s*!==\s*['"]App['"]/);
  // The friendly error must reference the new l10n key, not a hard-coded string.
  assert.match(src, /aif\.pages\.wizards\.manage\.appOnly/);
});
