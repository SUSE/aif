/**
 * Rancher-specific type definitions for SUSE AI Extension
 * Replaces `any` types with proper interfaces
 */

import type { Router } from 'vue-router';

// === Store Types ===

// Minimal store interface required by service-layer functions that only call dispatch.
// RancherStore satisfies this type, as does any { dispatch } adapter used in Vuex actions.
export interface Dispatchable {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  dispatch: (action: string, payload?: any) => Promise<any>;
}

export interface RancherStore extends Dispatchable {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  getters?: Record<string, any>;
  registerModule?: (name: string, module: any) => void;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  watch:           (getter: (state: any, getters: any) => any, cb: (value: any, oldValue: any) => void) => () => void;
  state: {
    $router?: Router;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    [key: string]: any;
  };
}

// === Cluster Types ===
export interface ClusterInfo {
  id: string;
  name: string;
  ready: boolean;
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

// === API Response Types ===
export interface ListResponse<T extends { id?: string } | { metadata?: { name?: string } }> {
  items?: T[];
  data?: T[] | T;
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


export function isHelmSecret(obj: any): obj is HelmSecret {
  return obj &&
         obj.metadata &&
         typeof obj.metadata.name === 'string' &&
         obj.type === 'helm.sh/release.v1';
}
