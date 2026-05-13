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

test('apps.vue imports and uses AddToBundleDialog component', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /import AddToBundleDialog from/);
  assert.match(source, /AddToBundleDialog/);
});

test('apps.vue imports fetchApps and fetchCategories from services', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /import.*fetchApps.*from.*services\/apps-api/s);
  assert.match(source, /import.*fetchCategories.*from.*services\/apps-api/s);
});

test('apps.vue has search input with correct i18n placeholder', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /aif\.pages\.apps\.toolbar\.search/);
  assert.match(source, /type="search"|type='search'/);
});

test('apps.vue has source filter select', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /aif\.pages\.apps\.toolbar\.sourceAll/);
  assert.match(source, /aif\.pages\.apps\.toolbar\.sourceNvidia/);
  assert.match(source, /aif\.pages\.apps\.toolbar\.sourceSuse/);
});

test('apps.vue has category filter select', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /aif\.pages\.apps\.toolbar\.categoryAll/);
  assert.match(source, /categories/);
});

test('apps.vue has tile/list view toggle', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /viewMode/);
  assert.match(source, /tiles|tile/);
  assert.match(source, /list/);
});

test('apps.vue has Include Reference Blueprints toggle', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /includeReferenceBlueprints|includeRefBlueprints/);
  assert.match(source, /aif\.pages\.apps\.toolbar\.includeRefBlueprints/);
  assert.match(source, /localStorage/);
});

test('apps.vue has refresh button', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /aif\.pages\.apps\.toolbar\.refresh/);
  assert.match(source, /refresh/);
});

test('apps.vue has header with per-source pill counts', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /nvidiaCount|nvidia-count/);
  assert.match(source, /suseCount|suse-count/);
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
  assert.match(source, /aif\.pages\.apps\.list\.publisher/);
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

test('apps.vue handles add-to-bundle dialog show/hide', () => {
  const source = read('pages/apps.vue');

  assert.match(source, /showAddToBundleDialog|showDialog|dialogApp/);
});
