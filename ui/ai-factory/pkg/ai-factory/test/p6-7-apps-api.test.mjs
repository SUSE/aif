import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');

test('operator-api.ts exports listApps, getApp, listCategories', () => {
  const source = read('utils/operator-api.ts');

  assert.match(source, /export async function listApps/);
  assert.match(source, /export async function getApp/);
  assert.match(source, /export async function listCategories/);
});

test('operator-api.ts defines App and ChartRef interfaces matching Go types', () => {
  const source = read('utils/operator-api.ts');

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

test('operator-api.ts defines ListAppsParams with correct filter fields', () => {
  const source = read('utils/operator-api.ts');

  assert.match(source, /export interface ListAppsParams/);
  assert.match(source, /source\?:/);
  assert.match(source, /category\?:/);
  assert.match(source, /includeReferenceBlueprints\?:/);
});

test('listApps builds correct query string from params', () => {
  const source = read('utils/operator-api.ts');

  // Must use /api/v1/apps endpoint
  assert.match(source, /\/api\/v1\/apps/);
  // Must append query params
  assert.match(source, /includeReferenceBlueprints/);
  assert.match(source, /URLSearchParams|searchParams|query/);
});

test('listCategories calls /api/v1/apps/categories', () => {
  const source = read('utils/operator-api.ts');

  assert.match(source, /\/api\/v1\/apps\/categories/);
});

test('getApp calls /api/v1/apps/ with id', () => {
  const source = read('utils/operator-api.ts');

  assert.match(source, /\/api\/v1\/apps\/.*\$\{.*id/s);
});

test('operator-api.ts routes through Rancher K8s service proxy', () => {
  const source = read('utils/operator-api.ts');

  assert.match(source, /\/k8s\/clusters\//);
  assert.match(source, /OPERATOR_NAMESPACE/);
  assert.match(source, /OPERATOR_SERVICE/);
  assert.match(source, /OPERATOR_PORT/);
});

test('operator service constants are exported from config/types.ts', () => {
  const types = read('config/types.ts');

  assert.match(types, /export const OPERATOR_NAMESPACE/);
  assert.match(types, /export const OPERATOR_SERVICE/);
  assert.match(types, /export const OPERATOR_PORT/);
});
