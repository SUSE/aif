import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');

test('apps-api.ts exports fetchApps, fetchApp, fetchCategories', () => {
  const source = read('services/apps-api.ts');

  assert.match(source, /export async function fetchApps/);
  assert.match(source, /export async function fetchApp/);
  assert.match(source, /export async function fetchCategories/);
});

test('apps-api.ts defines App and ChartRef interfaces matching Go types', () => {
  const source = read('services/apps-api.ts');

  // App fields must match pkg/apps/types.go JSON tags
  for (const field of [
    'id: string',
    'name: string',
    'displayName: string',
    'description: string',
    'publisher: string',
    'version: string',
    'logoURL: string',
    'source: string',
    'assetType: string',
    'categories: string[]',
    'tags: string[]',
    'chartRef: ChartRef',
    'projectURL: string',
    'referenceBlueprint: boolean',
  ]) {
    assert.match(source, new RegExp(field.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  }

  // ChartRef fields
  for (const field of ['repo: string', 'chart: string', 'version: string']) {
    assert.match(source, new RegExp(field.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  }
});

test('apps-api.ts defines FetchAppsParams with correct filter fields', () => {
  const source = read('services/apps-api.ts');

  assert.match(source, /export interface FetchAppsParams/);
  assert.match(source, /source\?:/);
  assert.match(source, /category\?:/);
  assert.match(source, /includeReferenceBlueprints\?:/);
});

test('fetchApps builds correct query string from params', () => {
  const source = read('services/apps-api.ts');

  // Must use /api/v1/apps endpoint
  assert.match(source, /\/api\/v1\/apps/);
  // Must append query params
  assert.match(source, /includeReferenceBlueprints/);
  assert.match(source, /URLSearchParams|searchParams|query/);
});

test('fetchCategories calls /api/v1/apps/categories', () => {
  const source = read('services/apps-api.ts');

  assert.match(source, /\/api\/v1\/apps\/categories/);
});

test('fetchApp calls /api/v1/apps/ with id', () => {
  const source = read('services/apps-api.ts');

  assert.match(source, /\/api\/v1\/apps\/.*\$\{.*id/s);
});
