import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');

test('AppCard.vue exists and has correct component name', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /name:\s*'AppCard'/);
});

test('AppCard.vue accepts app prop', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /props.*app/s);
});

test('AppCard.vue renders publisher badge with source-based styling', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /publisher-badge/);
  assert.match(source, /nvidia/i);
  assert.match(source, /suse/i);
});

test('AppCard.vue: whole card is clickable and emits install/select', () => {
  const src = read('components/apps/AppCard.vue');

  assert.match(src, /@click/);
  assert.match(src, /\$emit\(\s*['"](install|select)['"]/);
});

test('AppCard.vue: Add to Bundle button removed', () => {
  const src = read('components/apps/AppCard.vue');

  assert.doesNotMatch(src, /add-to-bundle/);
});

test('AppCard.vue: shows packaging badge (Helm/Container)', () => {
  const src = read('components/apps/AppCard.vue');

  assert.match(src, /aif\.pages\.apps\.packaging\.(helm|container)|assetType/);
});

test('AppCard.vue emits install event', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /emits:\s*\[\s*'install'\s*\]/);
});

test('AppCard.vue renders logo with fallback', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /logoURL|logo-url/i);
  assert.match(source, /@error|onerror/i);
});

test('AppCard.vue renders description with line clamp', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /line-clamp|webkit-line-clamp/);
});

test('AppCard.vue renders external link when projectURL exists', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /projectURL/);
  assert.match(source, /target="_blank"/);
});
