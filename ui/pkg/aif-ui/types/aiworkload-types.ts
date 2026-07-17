// pkg/aif-ui/types/aiworkload-types.ts
export type AIWorkloadSourceType = 'App' | 'Blueprint';
export type AIWorkloadDeployStrategy = 'Helm' | 'FleetBundle' | 'GitOps';
export type AIWorkloadPhase = 'Pending' | 'Running' | 'Degraded' | 'Failed';
export type AIWorkloadClusterPhase = 'Running' | 'Failed' | 'Pending';

export interface AppSource {
  chartRepo:    string;
  chartName:    string;
  chartVersion: string;
  release:      string;
}

export interface BlueprintSource {
  name:    string;
  version: string;
}

export interface AIWorkloadSource {
  sourceType:  AIWorkloadSourceType;
  app?:        AppSource;
  blueprint?:  BlueprintSource;
}

export interface ComponentValueOverride {
  componentName: string;
  values?:       Record<string, any>;
}

export interface AIWorkloadSpec {
  displayName:      string;
  source:           AIWorkloadSource;
  targetNamespace:  string;
  targetClusters?:  string[];
  deployStrategy?:  AIWorkloadDeployStrategy;
  componentValues?: ComponentValueOverride[];
  fleetBundleNames?: string[];
}

export interface AIWorkloadClusterStatus {
  clusterId: string;
  phase:     AIWorkloadClusterPhase;
  message?:  string;
}

export interface DeployedSourceSnapshot {
  version:      string;
  renderDigest: string;
  certifiedAt:  string;
}

export interface AIWorkloadComponentStatus {
  componentName:     string;
  clusterId:         string;
  phase:             AIWorkloadClusterPhase;
  revision?:         string;
  installedVersion?: string;
  message?:          string;
}

export type AIWorkloadOperationState = 'InProgress' | 'Succeeded' | 'Failed' | 'Superseded';

export interface AIWorkloadOperation {
  type:            'Upgrade' | 'Rollback' | 'Retry';
  nonce:           string;
  targetVersion?:  string;
  expectedDigest?: string;
  retryEpoch?:     number;
  requestedAt:     string;
  intentDigest?:   string;
  state:           AIWorkloadOperationState;
  reason?:         string;
}

export interface AIWorkloadStatus {
  phase?:              AIWorkloadPhase;
  clusterStatuses?:    AIWorkloadClusterStatus[];
  conditions?:         any[];
  observedGeneration?: number;
  deployedSource?:     DeployedSourceSnapshot;
  componentStatuses?:  AIWorkloadComponentStatus[];
  activeOperation?:    AIWorkloadOperation;
}

export interface AIWorkload {
  apiVersion: string;
  kind:       string;
  metadata:   { name: string; namespace: string };
  spec:       AIWorkloadSpec;
  status?:    AIWorkloadStatus;
}

export interface RegistryCred {
  username:     string;
  password:     string;
  registryHost: string;
}

export interface RegistryCredentials {
  applicationCollection?: RegistryCred;
  suseRegistry?:          RegistryCred;
  nvidia?:                RegistryCred;
}
