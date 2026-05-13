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

test('AppCard.vue renders reference blueprint badge conditionally', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /referenceBlueprint/);
  assert.match(source, /aif\.pages\.apps\.badge\.referenceBlueprint/);
});

test('AppCard.vue has Install and Add to Bundle buttons', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /aif\.pages\.apps\.card\.install/);
  assert.match(source, /aif\.pages\.apps\.card\.addToBundle/);
});

test('AppCard.vue emits install and add-to-bundle events', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /emit.*install|'install'/s);
  assert.match(source, /emit.*add-to-bundle|'add-to-bundle'/s);
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

test('AppCard.vue renders tags from categories', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /categories|tags/);
});

test('AppCard.vue renders external link when projectURL exists', () => {
  const source = read('components/apps/AppCard.vue');

  assert.match(source, /projectURL/);
  assert.match(source, /target="_blank"/);
});
