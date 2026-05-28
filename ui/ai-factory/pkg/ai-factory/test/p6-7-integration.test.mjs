import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');

test('apps.vue imports match existing files', () => {
  const page = read('pages/apps.vue');

  assert.match(page, /from '\.\.\/components\/apps\/AppCard\.vue'/);
  assert.match(page, /from '\.\.\/utils\/operator-api'/);

  // Verify the imported files exist by reading them (throws if missing)
  read('components/apps/AppCard.vue');
  read('utils/operator-api.ts');
});

test('all i18n keys used in templates exist in en-us.yaml', () => {
  const l10n = read('l10n/en-us.yaml');
  const files = [
    read('pages/apps.vue'),
    read('components/apps/AppCard.vue')
  ];

  const usedKeys = [];

  for (const source of files) {
    const matches = source.matchAll(/t\(['"]([^'"]+)['"]\)/g);

    for (const m of matches) {
      usedKeys.push(m[1]);
    }
  }

  // Every used key should have a corresponding leaf in the YAML
  // We check that the deepest segment appears in the file (lightweight check)
  for (const key of usedKeys) {
    if (!key.startsWith('aif.pages.apps.')) {
      continue;
    }
    const segments = key.split('.');
    const leaf = segments[segments.length - 1];

    assert.match(l10n, new RegExp(`${leaf}:`), `Missing i18n key: ${key}`);
  }
});

test('AppCard emits match what apps.vue listens for', () => {
  const card = read('components/apps/AppCard.vue');
  const page = read('pages/apps.vue');

  assert.match(card, /emits:\s*\[\s*'install'\s*\]/);

  assert.match(page, /@install/);
});

test('utils/operator-api.ts App interface fields align with Go types', () => {
  const api = read('utils/operator-api.ts');
  const goTypes = readFileSync(new URL('../../../../../pkg/apps/types.go', import.meta.url), 'utf8');

  // Check that every JSON tag in Go has a matching TS field
  const jsonTags = [...goTypes.matchAll(/json:"(\w+)(?:,omitempty)?"/g)].map((m) => m[1]);

  // Skip Go-only types from non-App structs (ListOpts, EngineSettings, RegistrySettings, AppCollectionSettings, SourceStatus)
  const skipList = [
    // RegistrySettings
    'endpoint',
    // AppCollectionSettings
    'apiURL', 'ociHost',
    // SourceStatus
    'lastSuccessAt', 'lastError', 'entryCount',
    // EngineSettings uses struct fields without json tags, so no need to skip here
    // These are the fields we actually want to validate from App and ChartRef:
    // App: id, name, displayName, description, publisher, version, logoURL, source, assetType, categories, tags, chartRef, projectURL, referenceBlueprint, useCase
    // ChartRef: repo, chart, version
  ];

  for (const tag of jsonTags) {
    if (skipList.includes(tag)) {
      continue;
    }
    assert.match(api, new RegExp(`${tag}[?]?:\\s`), `Missing TS field for Go JSON tag: ${tag}`);
  }
});
