import { operatorFetch } from './operator-config';
import type { AIWorkload, AIWorkloadSpec, AIWorkloadStatus, RegistryCredentials } from '../types/aiworkload-types';

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

export function getVersion(timeoutMs = 5000): Promise<{ version: string; commit: string; chartVersion: string }> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  return operatorFetch('/api/v1/version', { signal: controller.signal })
    .finally(() => clearTimeout(timer));
}

export interface ValidateOverride {
  userSecretRef?:  { name: string; key: string } | null;
  tokenSecretRef?: { name: string; key: string } | null;
  credSecretRef?:  { name: string; key: string } | null;
  repoURL?:        string;
  branch?:         string;
}

export interface ValidateRequest {
  targets?:   string[];
  overrides?: Record<string, ValidateOverride>;
}

export interface ValidateResult {
  target:     string;
  status:     'ok' | 'failed' | 'error' | 'skipped';
  host?:      string;
  message:    string;
  latencyMs?: number;
}

export interface ValidateResponse {
  results: ValidateResult[];
}

export function validateCredentials(body: ValidateRequest, timeoutMs = 20000): Promise<ValidateResponse> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  return operatorFetch('/api/v1/settings/validate-credentials', {
    method: 'POST',
    body:   JSON.stringify(body),
    signal: controller.signal,
  }).finally(() => clearTimeout(timer));
}
