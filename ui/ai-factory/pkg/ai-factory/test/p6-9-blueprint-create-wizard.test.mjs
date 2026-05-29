import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('blueprint-create.vue: exports name BlueprintCreateWizard', () => {
  const src = read('pages/wizards/blueprint-create.vue');
  assert.match(src, /name:\s*'BlueprintCreateWizard'/);
});

test('blueprint-create.vue: uses WizardStepIndicator', () => {
  const src = read('pages/wizards/blueprint-create.vue');
  assert.match(src, /WizardStepIndicator/);
});

test('blueprint-create.vue: has 4 steps (basicInfo, selectApps, configuration, review)', () => {
  const src = read('pages/wizards/blueprint-create.vue');
  assert.match(src, /aif\.pages\.wizards\.create\.steps\.basicInfo/);
  assert.match(src, /aif\.pages\.wizards\.create\.steps\.selectApps/);
  assert.match(src, /aif\.pages\.wizards\.create\.steps\.configuration/);
  assert.match(src, /aif\.pages\.wizards\.create\.steps\.review/);
});

test('blueprint-create.vue: calls createBlueprint with valueOverrides', () => {
  const src = read('pages/wizards/blueprint-create.vue');
  assert.match(src, /createBlueprint/);
  assert.match(src, /operator-api/);
  assert.match(src, /valueOverrides/);
});

test('blueprint-create.vue: has blueprintName, version, useCase fields', () => {
  const src = read('pages/wizards/blueprint-create.vue');
  assert.match(src, /aif\.pages\.wizards\.create\.blueprintName/);
  assert.match(src, /aif\.pages\.wizards\.create\.version/);
  assert.match(src, /aif\.pages\.wizards\.create\.useCase/);
});

test('blueprint-create.vue: validates version as semver', () => {
  const src = read('pages/wizards/blueprint-create.vue');
  assert.match(src, /SEMVER|semver/);
});

test('blueprint-create.vue: Select Apps step is catalog-driven (listApps)', () => {
  const src = read('pages/wizards/blueprint-create.vue');
  assert.match(src, /listApps/);
  assert.match(src, /aif\.pages\.wizards\.create\.selectApps\.search/);
});

test('blueprint-create.vue: Configuration step loads per-component defaults via getAppValues', () => {
  const src = read('pages/wizards/blueprint-create.vue');
  assert.match(src, /getAppValues/);
  assert.match(src, /aif\.pages\.wizards\.create\.config\.loadDefaults/);
});

test('blueprint-create.vue: gates Next on step readiness', () => {
  const src = read('pages/wizards/blueprint-create.vue');
  assert.match(src, /stepReady|:disabled/);
});

test('blueprints.vue: has New Blueprint navigation button', () => {
  const src = read('pages/blueprints.vue');
  assert.match(src, /blueprint-create/);
});
