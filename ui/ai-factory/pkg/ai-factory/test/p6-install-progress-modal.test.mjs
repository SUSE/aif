import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(`../${ p }`, import.meta.url), 'utf8');

test('InstallProgressModal.vue: exists and exports name', () => {
  const src = read('components/wizards/InstallProgressModal.vue');
  assert.match(src, /name:\s*'InstallProgressModal'/);
});

test('InstallProgressModal.vue: accepts show, title, and progress props', () => {
  const src = read('components/wizards/InstallProgressModal.vue');
  assert.match(src, /props:\s*\{[\s\S]*?\bshow:\s*\{[\s\S]*?type:\s*Boolean/);
  assert.match(src, /props:\s*\{[\s\S]*?\btitle:\s*\{[\s\S]*?type:\s*String/);
  assert.match(src, /props:\s*\{[\s\S]*?\bprogress:\s*\{[\s\S]*?type:\s*Array/);
});

test('InstallProgressModal.vue: emits done and cancel', () => {
  const src = read('components/wizards/InstallProgressModal.vue');
  assert.match(src, /emits:\s*\[[^\]]*'done'/);
  assert.match(src, /emits:\s*\[[^\]]*'cancel'/);
});

test('InstallProgressModal.vue: exports PROGRESS_STATUS constant', () => {
  const src = read('components/wizards/InstallProgressModal.vue');
  assert.match(src, /export\s+const\s+PROGRESS_STATUS/);
});

test('InstallProgressModal.vue: uses ModalWithCard', () => {
  const src = read('components/wizards/InstallProgressModal.vue');
  assert.match(src, /import\s+ModalWithCard\s+from\s+'@shell\/components\/ModalWithCard'/);
});

test('InstallProgressModal.vue: defines hasFailures computed', () => {
  const src = read('components/wizards/InstallProgressModal.vue');
  assert.match(src, /hasFailures\s*\(\)\s*\{[\s\S]*?PROGRESS_STATUS\.FAILED/);
});
