// pkg/aif-ui/types/aiworkload-types.ts
import type { ComponentKustomize } from './blueprint-types';

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
  // Enabled controls whether this Blueprint component is deployed at all.
  // Defaults to true (omitted/undefined == enabled).
  enabled?:      boolean;
  // Install-time Kustomize overlay, applied after the Blueprint component's own overlay.
  kustomize?:    ComponentKustomize;
  // Replaces the Blueprint component's dependsOn when set; [] means no dependencies.
  dependsOn?:    string[];
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
  // The capped Helm release name the operator installed (== the pods'
  // app.kubernetes.io/instance label). Absent on statuses from older operators.
  releaseName?:      string;
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
  customized?:         boolean;
  resolvedValues?:     ResolvedValue[];
  /** "<blueprint name>@<version>": the highest rollback-disabled version the operator started rendering; moves below it are refused. */
  minimumSafeVersion?: string;
}

export interface ResolvedValue {
  componentName: string;
  path:          string;
  value:         string;
  source:        'auto' | 'user';
}

export interface AIWorkload {
  apiVersion: string;
  kind:       string;
  metadata:   { name: string; namespace: string; uid?: string };
  spec:       AIWorkloadSpec;
  status?:    AIWorkloadStatus;
}

export interface RegistryFlags {
  applicationCollection: boolean;
  suseRegistry:          boolean;
  nvidia:                boolean;
}

/**
 * Top-level fields: whether the operator can resolve each registry's
 * credentials. `usable`: whether the operator can serve the registry's charts
 * (credentials resolve or an anonymous mirror is set); absent from older
 * operators.
 */
export interface RegistryStatus extends RegistryFlags {
  usable?: RegistryFlags;
}

/** What the calling user may do, as evaluated by the operator. */
export interface Capabilities {
  advancedCustomization: boolean;
}
