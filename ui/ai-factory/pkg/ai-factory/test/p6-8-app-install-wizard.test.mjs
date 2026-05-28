import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('app-install.vue: exports name AppInstallWizard', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /name:\s*'AppInstallWizard'/);
});

test('app-install.vue: uses WizardStepIndicator', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /WizardStepIndicator/);
});

test('app-install.vue: has 4 steps (basicInfo, target, configuration, review)', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /aif\.pages\.wizards\.steps\.basicInfo/);
  assert.match(src, /aif\.pages\.wizards\.steps\.target/);
  assert.match(src, /aif\.pages\.wizards\.steps\.configuration/);
  assert.match(src, /aif\.pages\.wizards\.steps\.review/);
});

test('app-install.vue: calls createWorkload from operator-api', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /createWorkload/);
  assert.match(src, /operator-api/);
});

test('app-install.vue: persists state to localStorage on step change', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /localStorage/);
});

test('app-install.vue: has instance name and namespace fields in step 1', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /aif\.pages\.wizards\.install\.instanceName/);
  assert.match(src, /aif\.pages\.wizards\.install\.namespace/);
});

test('app-install.vue: has target clusters and delivery strategy in step 2', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /aif\.pages\.wizards\.install\.targetClusters/);
  assert.match(src, /aif\.pages\.wizards\.install\.deliveryStrategy/);
});

test('app-install.vue: validates instance name as DNS label', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /DNS_LABEL/);
});

test('app-install.vue: Configuration step loads chart defaults via getAppValues', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /getAppValues/);
});

test('app-install.vue: shows InstallProgressModal after submit', () => {
  const src = read('pages/wizards/app-install.vue');
  assert.match(src, /InstallProgressModal/);
  assert.match(src, /showProgressModal/);
});

test('app-install.vue: keys valueOverrides by the instance name (not empty string)', () => {
  const src = read('pages/wizards/app-install.vue');
  // valueOverrides must be keyed by the workload name, e.g. { [this.form.name]: ... }
  assert.match(src, /\[\s*this\.form\.name\s*\]|\[\s*form\.name\s*\]/);
  assert.doesNotMatch(src, /valueOverrides:\s*\{\s*''\s*:/);
});

test('app-install.vue: sends explicit spec.name (not just metadata.name)', () => {
  const src = read('pages/wizards/app-install.vue');
  // `name: this.form.name` must appear at least twice: once in metadata, once in
  // spec. The spec-level copy removes the wizard's implicit dependency on the
  // handler defaulting spec.name from metadata.name.
  const matches = src.match(/name:\s*this\.form\.name/g) || [];

  assert.ok(matches.length >= 2, `expected ≥2 occurrences of "name: this.form.name", got ${ matches.length }`);
});
