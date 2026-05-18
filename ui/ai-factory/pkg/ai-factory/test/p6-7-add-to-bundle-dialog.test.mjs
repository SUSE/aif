import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');

test('AddToBundleDialog.vue exists and has correct component name', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /name:\s*'AddToBundleDialog'/);
});

test('AddToBundleDialog.vue accepts app prop', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /props.*app/s);
});

test('AddToBundleDialog.vue has two modes: existing and new', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /existing/);
  assert.match(source, /new/);
  assert.match(source, /mode/);
});

test('AddToBundleDialog.vue creates bundles via management store', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /dispatch.*management/);
  assert.match(source, /BUNDLE|bundle/i);
});

test('AddToBundleDialog.vue builds ComponentRef with kind App', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /kind.*App|ComponentKindApp|kind:\s*'App'/s);
  assert.match(source, /chartRef|app.*repo/s);
});

test('AddToBundleDialog.vue emits added and cancel events', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /emit.*added|'added'/s);
  assert.match(source, /emit.*cancel|'cancel'/s);
});

test('AddToBundleDialog.vue uses ModalWithCard from shell', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /ModalWithCard/);
  assert.match(source, /@shell\/components\/ModalWithCard/);
});

test('AddToBundleDialog.vue uses i18n keys for all text', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /aif\.pages\.apps\.dialog\.title/);
  assert.match(source, /aif\.pages\.apps\.dialog\.modeExisting/);
  assert.match(source, /aif\.pages\.apps\.dialog\.modeNew/);
  assert.match(source, /aif\.pages\.apps\.dialog\.cancel/);
  assert.match(source, /aif\.pages\.apps\.dialog\.confirm/);
});

test('AddToBundleDialog.vue has bundle name input for new mode', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /LabeledInput|labeled-input/);
  assert.match(source, /aif\.pages\.apps\.dialog\.newBundleName/);
});

test('AddToBundleDialog.vue uses the picker value as bundle namespace (not a hardcoded literal)', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  // Picker exists and is wired to the namespace ref.
  assert.match(source, /LabeledSelect/);
  assert.match(source, /aif\.pages\.apps\.dialog\.newBundleNamespace/);
  assert.match(source, /newBundleNamespace/);

  // Bundle metadata uses a `namespace` variable (shorthand or reference),
  // not a string literal. This catches regressions like `namespace: 'default'`
  // or any other hardcoded value inside the metadata object.
  assert.match(source, /metadata:\s*\{[^}]*\bnamespace\b[^}]*\}/);
  assert.doesNotMatch(source, /metadata:\s*\{[^}]*namespace:\s*['"]/);
});

test('AddToBundleDialog.vue fetches namespaces via management store', () => {
  const source = read('components/apps/AddToBundleDialog.vue');

  assert.match(source, /import.*NAMESPACE.*from.*@shell\/config\/types/);
  assert.match(source, /findAll.*NAMESPACE|type:\s*NAMESPACE/s);
});
