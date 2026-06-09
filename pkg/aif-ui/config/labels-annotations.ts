/**
 * Kubernetes Labels and Annotations Configuration
 * Following domain-driven design for K8s metadata management
 * Defines standard labels, annotations, and metadata helpers
 */

import { HELM_CONSTANTS, PRODUCT_SLUG } from '../utils/constants';

// === Standard Kubernetes Labels ===
export const STANDARD_LABELS = {
  // Standard Kubernetes labels (kubernetes.io)
  APP_NAME: 'app.kubernetes.io/name',
  APP_INSTANCE: 'app.kubernetes.io/instance',
  APP_VERSION: 'app.kubernetes.io/version',
  APP_COMPONENT: 'app.kubernetes.io/component',
  APP_PART_OF: 'app.kubernetes.io/part-of',
  APP_MANAGED_BY: 'app.kubernetes.io/managed-by',
  APP_CREATED_BY: 'app.kubernetes.io/created-by',

  // Helm labels
  HELM_CHART: 'helm.sh/chart',
  HELM_RELEASE: 'helm.sh/release',
  HELM_HERITAGE: 'helm.sh/heritage',

  // Common Rancher labels
  RANCHER_PROJECT_ID: 'field.cattle.io/projectId',
  RANCHER_CREATOR_ID: 'field.cattle.io/creatorId',
  
  // SUSE AI Extension labels
  SUSEAI_MANAGED: `${PRODUCT_SLUG}/managed`,
  SUSEAI_APP_ID: `${PRODUCT_SLUG}/app-id`,
  SUSEAI_REPOSITORY: `${PRODUCT_SLUG}/repository`,
  SUSEAI_CATEGORY: `${PRODUCT_SLUG}/category`,
  SUSEAI_INSTALLED_BY: `${PRODUCT_SLUG}/installed-by`
} as const;

// === Standard Kubernetes Annotations ===
export const STANDARD_ANNOTATIONS = {
  // Standard Kubernetes annotations
  DESCRIPTION: 'kubernetes.io/description',
  DOCUMENTATION: 'kubernetes.io/documentation',
  
  // Deployment annotations
  DEPLOYMENT_REVISION: 'deployment.kubernetes.io/revision',
  
  // Helm annotations
  HELM_CHART: 'helm.sh/chart',
  HELM_RELEASE: 'helm.sh/release',
  HELM_REVISION: 'helm.sh/revision',
  HELM_STATUS: 'helm.sh/status',
  HELM_VALUES: 'helm.sh/values',
  
  // Common Rancher annotations
  RANCHER_PROJECT_NAME: 'field.cattle.io/projectName',
  RANCHER_DESCRIPTION: 'field.cattle.io/description',
  
  // SUSE AI Extension annotations
  SUSEAI_CHART_VERSION: `${PRODUCT_SLUG}/chart-version`,
  SUSEAI_INSTALLATION_TIME: `${PRODUCT_SLUG}/installation-time`,
  SUSEAI_INSTALLATION_CONFIG: `${PRODUCT_SLUG}/installation-config`,
  SUSEAI_LAST_UPDATED: `${PRODUCT_SLUG}/last-updated`,
  SUSEAI_UPDATE_STRATEGY: `${PRODUCT_SLUG}/update-strategy`,
  SUSEAI_BACKUP_CONFIG: `${PRODUCT_SLUG}/backup-config`,
  SUSEAI_MONITORING_CONFIG: `${PRODUCT_SLUG}/monitoring-config`,
  SUSEAI_DEPENDENCIES: `${PRODUCT_SLUG}/dependencies`,
  SUSEAI_CUSTOM_VALUES: `${PRODUCT_SLUG}/custom-values`
} as const;

// === Label Value Helpers ===
export const LABEL_VALUES = {
  MANAGED_BY_SUSEAI: PRODUCT_SLUG,
  MANAGED_BY_HELM: 'Helm',
  COMPONENT_APPLICATION: 'application',
  COMPONENT_DATABASE: 'database',
  COMPONENT_WEB: 'web',
  COMPONENT_API: 'api',
  COMPONENT_WORKER: 'worker'
} as const;

// === Annotation Value Helpers ===
export const ANNOTATION_VALUES = {
  UPDATE_STRATEGY_AUTO: 'auto',
  UPDATE_STRATEGY_MANUAL: 'manual',
  UPDATE_STRATEGY_NOTIFY: 'notify-only',
  BACKUP_ENABLED: 'enabled',
  BACKUP_DISABLED: 'disabled',
  MONITORING_ENABLED: 'enabled',
  MONITORING_DISABLED: 'disabled'
} as const;

// === Metadata Interfaces ===
export interface StandardLabels {
  [STANDARD_LABELS.APP_NAME]?: string;
  [STANDARD_LABELS.APP_INSTANCE]?: string;
  [STANDARD_LABELS.APP_VERSION]?: string;
  [STANDARD_LABELS.APP_COMPONENT]?: string;
  [STANDARD_LABELS.APP_PART_OF]?: string;
  [STANDARD_LABELS.APP_MANAGED_BY]?: string;
  [STANDARD_LABELS.SUSEAI_MANAGED]?: string;
  [STANDARD_LABELS.SUSEAI_APP_ID]?: string;
  [STANDARD_LABELS.SUSEAI_REPOSITORY]?: string;
  [STANDARD_LABELS.SUSEAI_CATEGORY]?: string;
  [key: string]: string | undefined;
}

export interface StandardAnnotations {
  [STANDARD_ANNOTATIONS.DESCRIPTION]?: string;
  [STANDARD_ANNOTATIONS.SUSEAI_CHART_VERSION]?: string;
  [STANDARD_ANNOTATIONS.SUSEAI_INSTALLATION_TIME]?: string;
  [STANDARD_ANNOTATIONS.SUSEAI_INSTALLATION_CONFIG]?: string;
  [STANDARD_ANNOTATIONS.SUSEAI_LAST_UPDATED]?: string;
  [STANDARD_ANNOTATIONS.SUSEAI_UPDATE_STRATEGY]?: string;
  [key: string]: string | undefined;
}

export interface ResourceMetadata {
  name: string;
  namespace?: string;
  labels?: StandardLabels;
  annotations?: StandardAnnotations;
}

// === Metadata Helper Functions ===

/**
 * Create standard labels for SUSE AI managed resources
 */
export function createStandardLabels(options: {
  appId: string;
  appName?: string;
  instance?: string;
  version?: string;
  component?: string;
  repository?: string;
  category?: string;
  partOf?: string;
}): StandardLabels {
  return {
    [STANDARD_LABELS.APP_NAME]: options.appName || options.appId,
    [STANDARD_LABELS.APP_INSTANCE]: options.instance || options.appId,
    [STANDARD_LABELS.APP_VERSION]: options.version,
    [STANDARD_LABELS.APP_COMPONENT]: options.component,
    [STANDARD_LABELS.APP_PART_OF]: options.partOf,
    [STANDARD_LABELS.APP_MANAGED_BY]: LABEL_VALUES.MANAGED_BY_SUSEAI,
    [STANDARD_LABELS.SUSEAI_MANAGED]: 'true',
    [STANDARD_LABELS.SUSEAI_APP_ID]: options.appId,
    [STANDARD_LABELS.SUSEAI_REPOSITORY]: options.repository,
    [STANDARD_LABELS.SUSEAI_CATEGORY]: options.category
  };
}

/**
 * Create standard annotations for SUSE AI managed resources
 */
export function createStandardAnnotations(options: {
  description?: string;
  chartVersion?: string;
  installationConfig?: Record<string, any>;
  updateStrategy?: string;
  dependencies?: string[];
  customValues?: Record<string, any>;
}): StandardAnnotations {
  const now = new Date().toISOString();
  
  return {
    [STANDARD_ANNOTATIONS.DESCRIPTION]: options.description,
    [STANDARD_ANNOTATIONS.SUSEAI_CHART_VERSION]: options.chartVersion,
    [STANDARD_ANNOTATIONS.SUSEAI_INSTALLATION_TIME]: now,
    [STANDARD_ANNOTATIONS.SUSEAI_INSTALLATION_CONFIG]: options.installationConfig 
      ? JSON.stringify(options.installationConfig) 
      : undefined,
    [STANDARD_ANNOTATIONS.SUSEAI_LAST_UPDATED]: now,
    [STANDARD_ANNOTATIONS.SUSEAI_UPDATE_STRATEGY]: options.updateStrategy || ANNOTATION_VALUES.UPDATE_STRATEGY_MANUAL,
    [STANDARD_ANNOTATIONS.SUSEAI_DEPENDENCIES]: options.dependencies 
      ? JSON.stringify(options.dependencies)
      : undefined,
    [STANDARD_ANNOTATIONS.SUSEAI_CUSTOM_VALUES]: options.customValues 
      ? JSON.stringify(options.customValues)
      : undefined
  };
}

/**
 * Update last updated annotation
 */
export function updateLastUpdatedAnnotation(annotations: StandardAnnotations): StandardAnnotations {
  return {
    ...annotations,
    [STANDARD_ANNOTATIONS.SUSEAI_LAST_UPDATED]: new Date().toISOString()
  };
}

/**
 * Check if resource is managed by SUSE AI
 */
export function isManagedBySuseAI(labels?: Record<string, string>): boolean {
  return labels?.[STANDARD_LABELS.SUSEAI_MANAGED] === 'true' ||
         labels?.[STANDARD_LABELS.APP_MANAGED_BY] === LABEL_VALUES.MANAGED_BY_SUSEAI;
}

/**
 * Check if resource is managed by Helm
 */
export function isManagedByHelm(labels?: Record<string, string>): boolean {
  return labels?.[STANDARD_LABELS.APP_MANAGED_BY] === LABEL_VALUES.MANAGED_BY_HELM ||
         labels?.[STANDARD_LABELS.HELM_HERITAGE] === 'Helm';
}

/**
 * Get app ID from labels
 */
export function getAppIdFromLabels(labels?: Record<string, string>): string | null {
  return labels?.[STANDARD_LABELS.SUSEAI_APP_ID] || 
         labels?.[STANDARD_LABELS.APP_NAME] || 
         null;
}

/**
 * Get Helm release name from labels
 */
export function getHelmReleaseFromLabels(labels?: Record<string, string>): string | null {
  return labels?.[STANDARD_LABELS.APP_INSTANCE] ||
         labels?.[STANDARD_LABELS.HELM_RELEASE] ||
         null;
}

/**
 * Get installation config from annotations
 */
export function getInstallationConfigFromAnnotations(annotations?: Record<string, string>): Record<string, any> | null {
  const configStr = annotations?.[STANDARD_ANNOTATIONS.SUSEAI_INSTALLATION_CONFIG];
  if (!configStr) return null;
  
  try {
    return JSON.parse(configStr);
  } catch (error) {
    console.warn('Failed to parse installation config annotation:', error);
    return null;
  }
}

/**
 * Get custom values from annotations
 */
export function getCustomValuesFromAnnotations(annotations?: Record<string, string>): Record<string, any> | null {
  const valuesStr = annotations?.[STANDARD_ANNOTATIONS.SUSEAI_CUSTOM_VALUES];
  if (!valuesStr) return null;
  
  try {
    return JSON.parse(valuesStr);
  } catch (error) {
    console.warn('Failed to parse custom values annotation:', error);
    return null;
  }
}

/**
 * Get dependencies from annotations
 */
export function getDependenciesFromAnnotations(annotations?: Record<string, string>): string[] | null {
  const depsStr = annotations?.[STANDARD_ANNOTATIONS.SUSEAI_DEPENDENCIES];
  if (!depsStr) return null;
  
  try {
    return JSON.parse(depsStr);
  } catch (error) {
    console.warn('Failed to parse dependencies annotation:', error);
    return null;
  }
}

/**
 * Create selector for SUSE AI managed resources
 */
export function createSuseAISelector(): Record<string, string> {
  return {
    [STANDARD_LABELS.SUSEAI_MANAGED]: 'true'
  };
}

/**
 * Create selector for specific app
 */
export function createAppSelector(appId: string): Record<string, string> {
  return {
    [STANDARD_LABELS.SUSEAI_APP_ID]: appId,
    [STANDARD_LABELS.SUSEAI_MANAGED]: 'true'
  };
}

/**
 * Create selector for Helm releases
 */
export function createHelmReleaseSelector(releaseName: string): Record<string, string> {
  return {
    [STANDARD_LABELS.APP_INSTANCE]: releaseName,
    [STANDARD_LABELS.APP_MANAGED_BY]: LABEL_VALUES.MANAGED_BY_HELM
  };
}

export default {
  STANDARD_LABELS,
  STANDARD_ANNOTATIONS,
  LABEL_VALUES,
  ANNOTATION_VALUES,
  createStandardLabels,
  createStandardAnnotations,
  isManagedBySuseAI,
  isManagedByHelm,
  getAppIdFromLabels,
  getHelmReleaseFromLabels
};