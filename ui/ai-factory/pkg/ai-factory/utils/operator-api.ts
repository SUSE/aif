import {
  MANAGEMENT_CLUSTER,
  OPERATOR_NAMESPACE,
  OPERATOR_SERVICE,
  OPERATOR_PORT,
} from '../config/types';

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

export function listApps(params: Record<string, string> = {}): Promise<any> {
  const qs = new URLSearchParams(params).toString();

  return operatorFetch(`/api/v1/apps${ qs ? `?${ qs }` : '' }`);
}

export function getApp(id: string): Promise<any> {
  return operatorFetch(`/api/v1/apps/${ encodeURIComponent(id) }`);
}

export function listAppCategories(): Promise<string[]> {
  return operatorFetch('/api/v1/apps/categories');
}

// ── Bundles ───────────────────────────────────────────────────────────────────

export function listBundles(): Promise<any> {
  return operatorFetch('/api/v1/bundles');
}

export function getBundle(namespace: string, name: string): Promise<any> {
  return operatorFetch(`/api/v1/bundles/${ namespace }/${ name }`);
}

export function createBundle(spec: any): Promise<any> {
  return operatorFetch('/api/v1/bundles', { method: 'POST', body: JSON.stringify(spec) });
}

export function patchBundle(namespace: string, name: string, spec: any): Promise<any> {
  return operatorFetch(`/api/v1/bundles/${ namespace }/${ name }`, { method: 'PATCH', body: JSON.stringify(spec) });
}

export function deleteBundle(namespace: string, name: string): Promise<void> {
  return operatorFetch(`/api/v1/bundles/${ namespace }/${ name }`, { method: 'DELETE' });
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

// ── Workloads ─────────────────────────────────────────────────────────────────

export function listWorkloads(): Promise<any> {
  return operatorFetch('/api/v1/workloads');
}

export function getWorkload(namespace: string, name: string): Promise<any> {
  return operatorFetch(`/api/v1/workloads/${ namespace }/${ name }`);
}

export function deleteWorkload(namespace: string, name: string): Promise<void> {
  return operatorFetch(`/api/v1/workloads/${ namespace }/${ name }`, { method: 'DELETE' });
}
