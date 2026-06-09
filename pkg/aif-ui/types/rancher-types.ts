/**
 * Rancher-specific type definitions for SUSE AI Extension
 * Replaces `any` types with proper interfaces
 */

// === Store Types ===
export interface RancherStore {
  dispatch: (action: string, payload?: any) => Promise<any>;
  getters?: Record<string, any>;
  registerModule?: (name: string, module: any) => void;
}

// === Cluster Types ===
export interface ClusterInfo {
  id: string;
  name: string;
}

export interface ClusterResource {
  id: string;
  type: string;
  metadata: {
    name: string;
    namespace?: string;
    resourceVersion?: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
  };
  spec?: Record<string, any>;
  status?: Record<string, any>;
}

// === Namespace Types ===
export interface NamespaceResource {
  metadata: {
    name: string;
  };
  spec?: Record<string, any>;
  status?: Record<string, any>;
}

// === Node Types ===
export interface NodeResource {
  metadata: {
    name: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
  };
  spec?: {
    taints?: Array<{ key: string; value?: string; effect: string }>;
  };
  status?: {
    nodeInfo?: {
      osImage?: string;
      kernelVersion?: string;
      containerRuntimeVersion?: string;
    };
    capacity?: Record<string, string>;
    allocatable?: Record<string, string>;
    conditions?: Array<{ type: string; status: string; reason?: string; message?: string }>;
  };
}

export interface NodeMetric {
  metadata: {
    name: string;
  };
  usage?: {
    cpu?: string;
    memory?: string;
  };
  metrics?: {
    cpu?: string;
    memory?: string;
  };
}

// === Helm Release Types ===
export interface HelmSecret {
  metadata: {
    name: string;
    namespace: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
  };
  type: string;
  data: {
    release?: string;
    [key: string]: string | undefined;
  };
}

export interface HelmConfigMap {
  metadata: {
    name: string;
    namespace: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
  };
  data: Record<string, string>;
}

export interface HelmReleaseInfo {
  release?: string;
  chartBase?: string;
  version?: string;
}

export interface HelmInstallationDetails {
  chartName: string;
  chartVersion: string;
  values: Record<string, any>;
  releaseName: string;
  namespace: string;
  clusterId: string;
}

// === App Types ===
export interface AppCRD {
  metadata: {
    name: string;
    namespace: string;
    generation?: number;
    resourceVersion?: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
  };
  spec: {
    targetNamespace?: string;
    chart?: {
      metadata?: {
        name?: string;
        version?: string;
      };
      values?: Record<string, unknown>;
    };
    chartName?: string;
    version?: string;
    values?: Record<string, unknown>;
    valuesYaml?: string;
  };
  status?: {
    observedGeneration?: number;
    conditions?: Array<{
      type: string;
      status: string;
      message?: string;
    }>;
    summary?: {
      state?: string;
    };
  };
}

// === Registry Secret Types ===
export interface RegistrySecret {
  metadata: {
    name: string;
    namespace: string;
    resourceVersion?: string;
  };
  type: string;
  data: {
    '.dockerconfigjson': string;
  };
}

export interface DockerConfig {
  auths: Record<string, {
    auth: string;
    username?: string;
    password?: string;
  }>;
}

// === Repository Types ===
export interface RepositoryIndex {
  entries: Record<string, Array<{
    name: string;
    version: string;
    description?: string;
    appVersion?: string;
  }>>;
}

export interface FileEntry {
  content?: string;
  contents?: string;
  data?: string;
  base64?: string;
  value?: string;
  Value?: string;
  text?: string;
  encoding?: string;
  name?: string;
}

// === Chart Types ===
export interface ChartVersion {
  name: string;
  version: string;
  description?: string;
  appVersion?: string;
}

// === Service Account Types ===
export interface ServiceAccount {
  metadata: {
    name: string;
    namespace: string;
    resourceVersion?: string;
  };
  imagePullSecrets?: Array<{
    name: string;
  }>;
}

// === Error Types ===
export interface RancherError {
  status?: number;
  code?: number;
  message?: string;
  response?: {
    status?: number;
    data?: any;
  };
  stack?: string;
  data?: any;
}

// === Request Types ===
export interface RancherRequestConfig {
  url: string;
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';
  data?: any;
  headers?: Record<string, string>;
  responseType?: 'json' | 'text' | 'arraybuffer';
}

// === API Response Types ===
export interface ListResponse<T extends { id?: string } | { metadata?: { name?: string } }> {
  items?: T[];
  data?: T[] | T;
}

export interface ResourceResponse<T extends { id?: string } | { metadata?: { name?: string } }> {
  data?: T;
  metadata?: {
    resourceVersion?: string;
  };
}

// === Installation Types ===
export interface InstallationPayload {
  metadata: {
    name: string;
    namespace: string;
    resourceVersion?: string;
  };
  spec: {
    chart: {
      metadata: {
        name: string;
        version: string;
      };
    };
    values?: Record<string, any>;
    targetNamespace?: string;
  };
}

// === Project Types ===
export interface ProjectResource {
  id: string;
  metadata: {
    name: string;
  };
  spec?: {
    clusterName?: string;
  };
}

// === Type Guards ===
export function isRancherError(error: any): error is RancherError {
  return error && (typeof error.status === 'number' || typeof error.code === 'number');
}

export function isClusterResource(obj: any): obj is ClusterResource {
  return obj && obj.metadata && typeof obj.metadata.name === 'string';
}

export function isHelmSecret(obj: any): obj is HelmSecret {
  return obj &&
         obj.metadata &&
         typeof obj.metadata.name === 'string' &&
         obj.type === 'helm.sh/release.v1';
}

export function isAppCRD(obj: any): obj is AppCRD {
  return obj &&
         obj.metadata &&
         typeof obj.metadata.name === 'string' &&
         obj.spec;
}

// === Runtime Validation Functions ===
export function validateClusterInfo(obj: unknown): ClusterInfo | null {
  if (!obj || typeof obj !== 'object') return null;
  const cluster = obj as any;

  if (typeof cluster.id !== 'string' || typeof cluster.name !== 'string') {
    return null;
  }

  return {
    id: cluster.id,
    name: cluster.name
  };
}

export function validateAppCollectionItem(obj: unknown): boolean {
  if (!obj || typeof obj !== 'object') return false;
  const app = obj as any;

  return typeof app.name === 'string' &&
         typeof app.slug_name === 'string' &&
         (app.packaging_format === 'HELM_CHART' || app.packaging_format === 'CONTAINER' || !app.packaging_format);
}

export function validateListResponse<T extends { id?: string } | { metadata?: { name?: string } }>(obj: unknown, itemValidator: (item: unknown) => boolean): ListResponse<T> | null {
  if (!obj || typeof obj !== 'object') return null;
  const response = obj as any;

  // Check if it has items array
  if (response.items && Array.isArray(response.items)) {
    const validItems = response.items.filter(itemValidator);
    return { items: validItems as T[] };
  }

  // Check if it has data array
  if (response.data && Array.isArray(response.data)) {
    const validItems = response.data.filter(itemValidator);
    return { data: validItems as T[] };
  }

  // Check if data is a single item
  if (response.data && itemValidator(response.data)) {
    return { data: response.data as T };
  }

  return null;
}

export function validateHelmSecret(obj: unknown): HelmSecret | null {
  if (!isHelmSecret(obj)) return null;

  const secret = obj as any;
  if (!secret.metadata?.name || !secret.metadata?.namespace) return null;

  return {
    metadata: {
      name: secret.metadata.name,
      namespace: secret.metadata.namespace,
      labels: secret.metadata.labels || {},
      annotations: secret.metadata.annotations || {}
    },
    type: secret.type,
    data: secret.data || {}
  };
}