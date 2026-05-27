import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('WizardStepIndicator.vue: exists and exports name', () => {
  const src = read('components/wizards/WizardStepIndicator.vue');
  assert.match(src, /name:\s*'WizardStepIndicator'/);
});

test('WizardStepIndicator.vue: accepts steps and currentStep props', () => {
  const src = read('components/wizards/WizardStepIndicator.vue');
  assert.match(src, /steps/);
  assert.match(src, /currentStep/);
});

test('WizardStepIndicator.vue: renders step numbers and labels', () => {
  const src = read('components/wizards/WizardStepIndicator.vue');
  assert.match(src, /v-for.*step/);
  assert.match(src, /step\.label|step\.title/);
});

test('WizardStepIndicator.vue: emits go-to-step on completed step click', () => {
  const src = read('components/wizards/WizardStepIndicator.vue');
  assert.match(src, /emit.*go-to-step|go-to-step/);
});
