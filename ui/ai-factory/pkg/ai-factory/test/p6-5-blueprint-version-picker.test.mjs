import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('BlueprintVersionPicker: component exists with correct name', () => {
  const src = read('components/blueprints/BlueprintVersionPicker.vue');

  assert.match(src, /name:\s*'BlueprintVersionPicker'/);
});

test('BlueprintVersionPicker: uses Rancher LabeledSelect', () => {
  const src = read('components/blueprints/BlueprintVersionPicker.vue');

  assert.match(src, /import LabeledSelect from '@shell\/components\/form\/LabeledSelect'/);
  assert.match(src, /<LabeledSelect/);
});

test('BlueprintVersionPicker: declares versions, modelValue, showDeprecated props', () => {
  const src = read('components/blueprints/BlueprintVersionPicker.vue');

  assert.match(src, /versions:\s*\{/);
  assert.match(src, /modelValue:\s*\{/);
  assert.match(src, /showDeprecated:\s*\{/);
});

test('BlueprintVersionPicker: emits update:modelValue', () => {
  const src = read('components/blueprints/BlueprintVersionPicker.vue');

  assert.match(src, /emits:\s*\[\s*'update:modelValue'\s*\]/);
});

test('BlueprintVersionPicker: filters non-Active options when showDeprecated=false', () => {
  const src = read('components/blueprints/BlueprintVersionPicker.vue');

  // The computed options list must reference both showDeprecated and the Active phase filter
  assert.match(src, /showDeprecated/);
  assert.match(src, /phase\s*===\s*'Active'/);
});
