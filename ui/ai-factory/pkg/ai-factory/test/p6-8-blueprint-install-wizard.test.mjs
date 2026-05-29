import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('blueprint-install.vue: exports name BlueprintInstallWizard', () => {
  const src = read('pages/wizards/blueprint-install.vue');
  assert.match(src, /name:\s*'BlueprintInstallWizard'/);
});

test('blueprint-install.vue: uses WizardStepIndicator', () => {
  const src = read('pages/wizards/blueprint-install.vue');
  assert.match(src, /WizardStepIndicator/);
});

test('blueprint-install.vue: has 3 steps (basicInfo, target, review)', () => {
  const src = read('pages/wizards/blueprint-install.vue');
  assert.match(src, /aif\.pages\.wizards\.steps\.basicInfo/);
  assert.match(src, /aif\.pages\.wizards\.steps\.target/);
  assert.match(src, /aif\.pages\.wizards\.steps\.review/);
});

test('blueprint-install.vue: calls createWorkload with Blueprint source kind', () => {
  const src = read('pages/wizards/blueprint-install.vue');
  assert.match(src, /createWorkload/);
  assert.match(src, /Blueprint/);
});

test('blueprint-install.vue: reads bpName and bpVersion from route params', () => {
  const src = read('pages/wizards/blueprint-install.vue');
  assert.match(src, /bpName|params\.bpName/);
  assert.match(src, /bpVersion|params\.bpVersion/);
});

test('blueprint-install.vue: shows InstallProgressModal after submit', () => {
  const src = read('pages/wizards/blueprint-install.vue');
  assert.match(src, /InstallProgressModal/);
  assert.match(src, /showProgressModal/);
});

test('blueprint-install.vue: validates workload name with DNS_LABEL', () => {
  const src = read('pages/wizards/blueprint-install.vue');
  assert.match(src, /DNS_LABEL/);
});
