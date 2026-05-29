import {
  MANAGEMENT_CLUSTER,
  OPERATOR_NAMESPACE,
  OPERATOR_SERVICE,
  OPERATOR_PORT,
} from '../config/types';
import { mockAPI, USE_MOCK_API } from './mock-api';

export interface ChartRef {
  repo: string;
  chart: string;
  version: string;
}

export interface App {
  id: string;
  name: string;
  displayName: string;
  description: string;
  publisher: string;
  version: string;
  logoURL: string;
  source: string;
  assetType: string;
  categories: string[];
  tags: string[];
  chartRef: ChartRef;
  projectURL: string;
  referenceBlueprint: boolean;
  useCase?: string;
  lastUpdatedAt?: string;
}

export interface ListAppsParams {
  source?: 'nvidia' | 'suse' | 'all';
  category?: string;
  includeReferenceBlueprints?: boolean;
}

// Rancher proxies service traffic through the Kubernetes API server:
// /k8s/clusters/<cluster>/api/v1/namespaces/<ns>/services/http:<svc>:<port>/proxy/
const BASE_URL = `/k8s/clusters/${ MANAGEMENT_CLUSTER }/api/v1/namespaces/${ OPERATOR_NAMESPACE }/services/http:${ OPERATOR_SERVICE }:${ OPERATOR_PORT }/proxy`;

interface OperatorError extends Error {
  status: number;
  code:   string;
}

async function operatorFetch(path: string, options: RequestInit = {}): Promise<any> {
  const res = await fetch(`${ BASE_URL }${ path }`, {
    ...options,
    headers: {
      'Accept': 'application/json',
      ...(options.body ? { 'Content-Type': 'application/json' } : {}),
      ...(options.headers || {}),
    },
  });

  const body = await res.json().catch(() => null);

  if (!res.ok) {
    const err = new Error(body?.message || res.statusText) as OperatorError;

    err.status = res.status;
    err.code   = body?.error || 'INTERNAL_ERROR';
    throw err;
  }

  return body;
}

// ── Settings ──────────────────────────────────────────────────────────────────

export function getSettings(): Promise<any> {
  return operatorFetch('/api/v1/settings');
}

export function putSettings(spec: any): Promise<any> {
  return operatorFetch('/api/v1/settings', {
    method: 'PUT',
    body:   JSON.stringify({ spec }),
  });
}

// ── Apps ──────────────────────────────────────────────────────────────────────

export async function listApps(params?: ListAppsParams): Promise<App[]> {
  if (USE_MOCK_API) {
    return mockAPI.apps.list(params);
  }

  const query: Record<string, string> = {};

  if (params?.source && params.source !== 'all') {
    query.source = params.source;
  }
  if (params?.category) {
    query.category = params.category;
  }
  if (params?.includeReferenceBlueprints !== undefined) {
    query.includeReferenceBlueprints = String(params.includeReferenceBlueprints);
  }

  const qs = new URLSearchParams(query).toString();

  return operatorFetch(`/api/v1/apps${ qs ? `?${ qs }` : '' }`);
}

export async function getApp(id: string): Promise<App> {
  if (USE_MOCK_API) {
    const app = mockAPI.apps.list({ includeReferenceBlueprints: true }).find((a) => a.id === id);

    if (!app) {
      throw new Error(`App not found: ${ id }`);
    }

    return app;
  }

  return operatorFetch(`/api/v1/apps/${ encodeURIComponent(id) }`);
}

export async function listCategories(): Promise<string[]> {
  if (USE_MOCK_API) {
    return mockAPI.apps.categories();
  }

  return operatorFetch('/api/v1/apps/categories');
}

export function getAppValues(id: string, version: string): Promise<{ values: Record<string, any>; questions: Record<string, any> | null }> {
  return operatorFetch(`/api/v1/apps/${ encodeURIComponent(id) }/values?version=${ encodeURIComponent(version) }`);
}

// ── Blueprints ────────────────────────────────────────────────────────────────

export function listBlueprints(params: Record<string, string> = {}): Promise<any> {
  const qs = new URLSearchParams(params).toString();

  return operatorFetch(`/api/v1/blueprints${ qs ? `?${ qs }` : '' }`);
}

export function getBlueprint(name: string): Promise<any> {
  return operatorFetch(`/api/v1/blueprints/${ name }`);
}

export function getBlueprintVersion(name: string, version: string): Promise<any> {
  return operatorFetch(`/api/v1/blueprints/${ name }/versions/${ version }`);
}

export function createBlueprint(spec: any): Promise<any> {
  return operatorFetch('/api/v1/blueprints', {
    method: 'POST',
    body:   JSON.stringify(spec),
  });
}

export function deprecateBlueprint(name: string, version: string, deprecated: boolean): Promise<any> {
  return operatorFetch(`/api/v1/blueprints/${ encodeURIComponent(name) }/${ encodeURIComponent(version) }`, {
    method: 'PATCH',
    body:   JSON.stringify({ deprecated }),
  });
}

export function deleteBlueprint(name: string, version: string): Promise<void> {
  return operatorFetch(`/api/v1/blueprints/${ encodeURIComponent(name) }/${ encodeURIComponent(version) }`, { method: 'DELETE' });
}

// ── Workloads ─────────────────────────────────────────────────────────────────

export function listWorkloads(): Promise<any> {
  return operatorFetch('/api/v1/workloads');
}

export function getWorkload(namespace: string, name: string): Promise<any> {
  return operatorFetch(`/api/v1/workloads/${ encodeURIComponent(namespace) }/${ encodeURIComponent(name) }`);
}

export function createWorkload(payload: { metadata: { name: string; namespace: string }; spec: any }): Promise<any> {
  return operatorFetch('/api/v1/workloads', {
    method: 'POST',
    body:   JSON.stringify(payload),
  });
}

export function putWorkload(namespace: string, name: string, spec: any): Promise<any> {
  return operatorFetch(`/api/v1/workloads/${ encodeURIComponent(namespace) }/${ encodeURIComponent(name) }`, {
    method: 'PUT',
    body:   JSON.stringify({ spec }),
  });
}

export function deleteWorkload(namespace: string, name: string): Promise<void> {
  return operatorFetch(`/api/v1/workloads/${ encodeURIComponent(namespace) }/${ encodeURIComponent(name) }`, { method: 'DELETE' });
}

export function upgradeWorkload(namespace: string, name: string, toBlueprintVersion: string): Promise<any> {
  return operatorFetch(`/api/v1/workloads/${ encodeURIComponent(namespace) }/${ encodeURIComponent(name) }/upgrade`, {
    method: 'POST',
    body:   JSON.stringify({ toBlueprintVersion }),
  });
}
