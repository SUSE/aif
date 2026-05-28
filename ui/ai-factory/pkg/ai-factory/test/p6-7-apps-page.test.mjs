import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');

test('apps.vue uses defineComponent with Composition API setup', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /defineComponent/);
  assert.match(source, /setup\s*\(/);
});

test('apps.vue keeps component name AppsPage', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /name:\s*'AppsPage'/);
});

test('apps.vue imports and uses AppCard component', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /import AppCard from/);
  assert.match(source, /AppCard/);
});

test('apps.vue imports listApps from operator-api', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /import.*listApps.*from.*utils\/operator-api/s);
});

test('apps.vue has search input with correct i18n placeholder', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /aif\.pages\.apps\.toolbar\.search/);
  assert.match(source, /type="search"|type='search'/);
});

test('apps.vue: registry selector has SUSE AI Library and Nvidia NGC only', () => {
  const src = read('pages/apps.vue');

  assert.match(src, /aif\.pages\.apps\.toolbar\.registrySuseLibrary/);
  assert.match(src, /aif\.pages\.apps\.toolbar\.registryNvidia/);
  // no "All" option
  assert.doesNotMatch(src, /toolbar\.sourceAll/);
});

test('apps.vue: category filter and reference-blueprints toggle removed', () => {
  const src = read('pages/apps.vue');

  assert.doesNotMatch(src, /categoryFilter/);
  assert.doesNotMatch(src, /includeRefBlueprints|includeReferenceBlueprints/);
});

test('apps.vue: Add to Bundle removed', () => {
  const src = read('pages/apps.vue');

  assert.doesNotMatch(src, /AddToBundle|add-to-bundle/);
});

test('apps.vue: results summary replaces count pills', () => {
  const src = read('pages/apps.vue');

  assert.match(src, /aif\.pages\.apps\.resultsSummary/);
  assert.doesNotMatch(src, /apps-page__pill/);
});

test('apps.vue: card selection navigates to app-install route', () => {
  const src = read('pages/apps.vue');

  assert.match(src, /app-install/);
  assert.match(src, /\.id/);
});

test('apps.vue has tile/list view toggle', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /viewMode/);
  assert.match(source, /tiles|tile/);
  assert.match(source, /list/);
});

test('apps.vue has refresh button', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /aif\.pages\.apps\.toolbar\.refresh/);
  assert.match(source, /refresh/);
});

test('apps.vue renders tile grid when viewMode is tiles', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /tiles-grid|app-tiles/);
  assert.match(source, /AppCard/);
});

test('apps.vue renders table when viewMode is list', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /<table|list-view/);
  assert.match(source, /aif\.pages\.apps\.list\.name/);
});

test('apps.vue has client-side search filter on name and description', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /search/);
  assert.match(source, /name.*toLowerCase|toLowerCase.*name/s);
  assert.match(source, /description.*toLowerCase|toLowerCase.*description/s);
});

test('apps.vue has empty state messages', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /aif\.pages\.apps\.empty\.noResults/);
  assert.match(source, /aif\.pages\.apps\.empty\.noCatalog/);
});

test('apps.vue has error banner', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /error/);
  assert.match(source, /Banner|banner/);
});

test('apps.vue has loading state', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /loading/);
});

test('apps.vue injects t() into setup with proxy binding so runtime calls do not lose this', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /const t = instance\?\.proxy\?\.t\?\.bind\(instance\.proxy\)/);
});
