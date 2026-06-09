import {
  MANAGEMENT_CLUSTER,
  OPERATOR_NAMESPACE,
  OPERATOR_SERVICE,
  OPERATOR_PORT,
} from './constants';
import type { AIWorkload, AIWorkloadSpec, AIWorkloadStatus, RegistryCredentials } from '../types/aiworkload-types';

// Rancher proxies service traffic:
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

export function getSettings(): Promise<any> {
  return operatorFetch('/api/v1/settings');
}

export function putSettings(spec: any): Promise<any> {
  return operatorFetch('/api/v1/settings', {
    method: 'PUT',
    body:   JSON.stringify({ spec }),
  });
}

export function getRegistryCredentials(timeoutMs = 30000): Promise<RegistryCredentials> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  return operatorFetch('/api/v1/settings/registry-credentials', { signal: controller.signal })
    .finally(() => clearTimeout(timer));
}

export function createAIWorkload(
  namespace: string,
  name:      string,
  spec:      AIWorkloadSpec,
  status?:   AIWorkloadStatus,
): Promise<AIWorkload> {
  return operatorFetch(`/api/v1/namespaces/${ encodeURIComponent(namespace) }/aiworkloads`, {
    method: 'POST',
    body:   JSON.stringify({ metadata: { name }, spec, status }),
  });
}

export function updateAIWorkload(
  namespace: string,
  name:      string,
  spec:      AIWorkloadSpec,
  status?:   AIWorkloadStatus,
): Promise<AIWorkload> {
  return operatorFetch(
    `/api/v1/namespaces/${ encodeURIComponent(namespace) }/aiworkloads/${ encodeURIComponent(name) }`,
    {
      method: 'PATCH',
      body:   JSON.stringify({ metadata: { name }, spec, status }),
    },
  );
}

export function listAIWorkloads(): Promise<{ items: AIWorkload[] }> {
  return operatorFetch('/api/v1/aiworkloads');
}

export function deleteAIWorkload(namespace: string, name: string): Promise<void> {
  return operatorFetch(
    `/api/v1/namespaces/${ encodeURIComponent(namespace) }/aiworkloads/${ encodeURIComponent(name) }`,
    { method: 'DELETE' },
  );
}

export function publishToFleetGit(bundleName: string, bundleYAML: string): Promise<{ commit: string }> {
  return operatorFetch('/api/v1/git/publish', {
    method: 'POST',
    body:   JSON.stringify({ bundleName, bundleYAML }),
  });
}
