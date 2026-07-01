/**
 * Feature Flag System for SUSE AI Extension
 * Version-aware feature management system
 * Enables/disables features based on Rancher version, Kubernetes version, and cluster capabilities
 * 
 * Usage:
 *   import { featureEnabled, RELEASE_FEATURES } from '@/utils/feature-flags';
 *   
 *   // Check if feature is enabled for current context
 *   if (featureEnabled('bulk-operations', clusterId, store)) {
 *     // Feature is enabled
 *   }
 *   
 *   // Get feature configuration
 *   const config = RELEASE_FEATURES['bulk-operations'];
 *   
 * Key Functions:
 * - featureEnabled(flag, clusterId?, store?) - Check if feature is enabled
 * - getClusterVersion(clusterId, store?) - Get cluster version info
 * - getLatestCompatibleVersion(versions, requirements) - Find compatible version
 * - checkVersionRequirements(clusterVersion, requirements) - Validate requirements
 */

import { FEATURE_FLAGS, VERSION_REQUIREMENTS } from './constants';
import type { FeatureFlag } from './constants';

type ClusterId = string;

// === Feature Configuration ===
export interface FeatureConfig {
  flag: FeatureFlag;
  displayName: string;
  description: string;
  requirements: FeatureRequirements;
  defaultEnabled: boolean;
  experimental?: boolean;
  deprecated?: boolean;
}

export interface FeatureRequirements {
  minRancherVersion?: string;
  minKubernetesVersion?: string;
  minHelmVersion?: string;
  requiredCapabilities?: string[];
  requiredFeatureGates?: string[];
  clusterRole?: 'any' | 'management' | 'downstream';
  supportedApiVersions?: string[];
  clusterCapabilities?: string[];
  installedOperators?: string[];
}

export interface ClusterVersion {
  rancher: string;
  kubernetes: string;
  helm?: string;
  distribution?: string;
}

export interface ClusterCapabilities {
  canInstallApps: boolean;
  canManageNamespaces: boolean;
  canAccessSecrets: boolean;
  canCreateServiceAccounts: boolean;
  hasHelmSupport: boolean;
  hasRancherAppsSupport: boolean;
  supportedApiVersions: string[];
  installedOperators: string[];
  featureGates: string[];
}

// === Release Feature Configuration ===
export const RELEASE_FEATURES: Record<FeatureFlag, FeatureConfig> = {
  [FEATURE_FLAGS.BULK_OPERATIONS]: {
    flag: FEATURE_FLAGS.BULK_OPERATIONS,
    displayName: 'Bulk Operations',
    description: 'Install, upgrade, or uninstall multiple applications at once',
    requirements: {
      minRancherVersion: '2.6.0',
      minKubernetesVersion: '1.20.0'
    },
    defaultEnabled: true
  },

  [FEATURE_FLAGS.ADVANCED_FILTERING]: {
    flag: FEATURE_FLAGS.ADVANCED_FILTERING,
    displayName: 'Advanced Filtering',
    description: 'Enhanced filtering options for applications and clusters',
    requirements: {},
    defaultEnabled: true
  },

  [FEATURE_FLAGS.CUSTOM_REPOSITORIES]: {
    flag: FEATURE_FLAGS.CUSTOM_REPOSITORIES,
    displayName: 'Custom Repositories',
    description: 'Add and manage custom Helm chart repositories',
    requirements: {
      minRancherVersion: '2.6.0',
      requiredCapabilities: ['canManageNamespaces'],
      clusterRole: 'management'
    },
    defaultEnabled: true
  },

  [FEATURE_FLAGS.HEALTH_MONITORING]: {
    flag: FEATURE_FLAGS.HEALTH_MONITORING,
    displayName: 'Health Monitoring',
    description: 'Real-time health monitoring and alerting for installed applications',
    requirements: {
      minRancherVersion: '2.6.0',
      minKubernetesVersion: '1.21.0',
      requiredCapabilities: ['canAccessSecrets'],
      supportedApiVersions: ['metrics.k8s.io/v1beta1']
    },
    defaultEnabled: false,
    experimental: true
  },

  [FEATURE_FLAGS.AUTO_UPDATES]: {
    flag: FEATURE_FLAGS.AUTO_UPDATES,
    displayName: 'Automatic Updates',
    description: 'Automatically update applications to latest compatible versions',
    requirements: {
      minRancherVersion: '2.7.0',
      minKubernetesVersion: '1.22.0',
      minHelmVersion: '3.8.0'
    },
    defaultEnabled: false,
    experimental: true
  },

  [FEATURE_FLAGS.ROLLBACK_SUPPORT]: {
    flag: FEATURE_FLAGS.ROLLBACK_SUPPORT,
    displayName: 'Rollback Support',
    description: 'Roll back applications to previous versions',
    requirements: {
      minRancherVersion: '2.6.0',
      minHelmVersion: '3.8.0',
      requiredCapabilities: ['hasHelmSupport']
    },
    defaultEnabled: true
  },

  [FEATURE_FLAGS.MULTI_CLUSTER]: {
    flag: FEATURE_FLAGS.MULTI_CLUSTER,
    displayName: 'Multi-Cluster Operations',
    description: 'Deploy and manage applications across multiple clusters',
    requirements: {
      minRancherVersion: '2.6.0',
      clusterRole: 'management'
    },
    defaultEnabled: true
  },

  [FEATURE_FLAGS.OFFLINE_MODE]: {
    flag: FEATURE_FLAGS.OFFLINE_MODE,
    displayName: 'Offline Mode',
    description: 'Work with cached application data in offline environments',
    requirements: {},
    defaultEnabled: false,
    experimental: true
  },

  [FEATURE_FLAGS.BACKUP_RESTORE]: {
    flag: FEATURE_FLAGS.BACKUP_RESTORE,
    displayName: 'Backup & Restore',
    description: 'Backup and restore application configurations and data',
    requirements: {
      minRancherVersion: '2.7.0',
      minKubernetesVersion: '1.22.0',
      requiredCapabilities: ['canAccessSecrets', 'canCreateServiceAccounts'],
      installedOperators: ['velero', 'backup-operator']
    },
    defaultEnabled: false,
    experimental: true
  },

  [FEATURE_FLAGS.SECURITY_SCANNING]: {
    flag: FEATURE_FLAGS.SECURITY_SCANNING,
    displayName: 'Security Scanning',
    description: 'Scan applications for security vulnerabilities',
    requirements: {
      minRancherVersion: '2.7.0',
      minKubernetesVersion: '1.21.0',
      installedOperators: ['neuvector', 'falco', 'trivy-operator']
    },
    defaultEnabled: false,
    experimental: true
  }
};

// === Version Comparison Utilities ===
export function compareVersions(version1: string, version2: string): number {
  const v1Parts = version1.split('.').map(Number);
  const v2Parts = version2.split('.').map(Number);
  
  const maxLength = Math.max(v1Parts.length, v2Parts.length);
  
  for (let i = 0; i < maxLength; i++) {
    const v1Part = v1Parts[i] || 0;
    const v2Part = v2Parts[i] || 0;
    
    if (v1Part > v2Part) return 1;
    if (v1Part < v2Part) return -1;
  }
  
  return 0;
}

export function isVersionGTE(currentVersion: string, requiredVersion: string): boolean {
  return compareVersions(currentVersion, requiredVersion) >= 0;
}

export function isVersionCompatible(version: string, minVersion?: string, maxVersion?: string): boolean {
  if (minVersion && compareVersions(version, minVersion) < 0) {
    return false;
  }
  
  if (maxVersion && compareVersions(version, maxVersion) > 0) {
    return false;
  }
  
  return true;
}

// === Cluster Version Detection ===
export function getClusterVersion(clusterId: string, store?: any): ClusterVersion {
  // This would get version info from the store
  // For now, return default values
  return {
    rancher: VERSION_REQUIREMENTS.RECOMMENDED_RANCHER_VERSION,
    kubernetes: VERSION_REQUIREMENTS.RECOMMENDED_KUBERNETES_VERSION,
    helm: VERSION_REQUIREMENTS.MIN_HELM_VERSION,
    distribution: 'K3s'
  };
}

export function getLatestCompatibleVersion(versions: string[], requirements: FeatureRequirements): string | null {
  if (!versions.length) return null;
  
  // Sort versions in descending order
  const sortedVersions = versions.sort((a, b) => compareVersions(b, a));
  
  // Find the latest version that meets all requirements
  for (const version of sortedVersions) {
    const clusterVersion = { rancher: version, kubernetes: version };
    if (checkVersionRequirements(clusterVersion, requirements)) {
      return version;
    }
  }
  
  return null;
}

// === Requirement Checking ===
export function checkVersionRequirements(
  clusterVersion: ClusterVersion, 
  requirements: FeatureRequirements
): boolean {
  // Check Rancher version
  if (requirements.minRancherVersion) {
    if (!isVersionGTE(clusterVersion.rancher, requirements.minRancherVersion)) {
      return false;
    }
  }
  
  // Check Kubernetes version
  if (requirements.minKubernetesVersion) {
    if (!isVersionGTE(clusterVersion.kubernetes, requirements.minKubernetesVersion)) {
      return false;
    }
  }
  
  // Check Helm version
  if (requirements.minHelmVersion && clusterVersion.helm) {
    if (!isVersionGTE(clusterVersion.helm, requirements.minHelmVersion)) {
      return false;
    }
  }
  
  return true;
}

export function checkCapabilityRequirements(
  clusterCapabilities: ClusterCapabilities,
  requirements: FeatureRequirements
): boolean {
  // Check required capabilities
  if (requirements.requiredCapabilities) {
    for (const capability of requirements.requiredCapabilities) {
      if (!(clusterCapabilities as any)[capability]) {
        return false;
      }
    }
  }
  
  // Check supported API versions
  if (requirements.supportedApiVersions) {
    for (const apiVersion of requirements.supportedApiVersions) {
      if (!clusterCapabilities.supportedApiVersions.includes(apiVersion)) {
        return false;
      }
    }
  }
  
  // Check installed operators
  if (requirements.installedOperators) {
    for (const operator of requirements.installedOperators) {
      if (!clusterCapabilities.installedOperators.includes(operator)) {
        return false;
      }
    }
  }
  
  // Check feature gates
  if (requirements.requiredFeatureGates) {
    for (const gate of requirements.requiredFeatureGates) {
      if (!clusterCapabilities.featureGates.includes(gate)) {
        return false;
      }
    }
  }
  
  return true;
}

// === Main Feature Flag Functions ===
export function featureEnabled(
  flag: FeatureFlag,
  clusterId?: ClusterId,
  store?: any
): boolean {
  const config = RELEASE_FEATURES[flag];
  if (!config) {
    console.warn(`Unknown feature flag: ${flag}`);
    return false;
  }
  
  // Check if feature is deprecated
  if (config.deprecated) {
    console.warn(`Feature ${flag} is deprecated and will be removed in a future version`);
    return false;
  }
  
  // Start with default enabled state
  let enabled = config.defaultEnabled;
  
  // Check version requirements
  if (clusterId && store) {
    const clusterVersion = getClusterVersion(clusterId, store);
    if (!checkVersionRequirements(clusterVersion, config.requirements)) {
      return false;
    }
    
    // Get cluster capabilities from store
    const clusterCapabilities = store.getters[`suseai/clusters/clusterCapabilities`](clusterId);
    if (clusterCapabilities && !checkCapabilityRequirements(clusterCapabilities, config.requirements)) {
      return false;
    }
  }
  
  // Check user preferences (stored in localStorage or user settings)
  try {
    const userPreferences = JSON.parse(localStorage.getItem('suseai-feature-flags') || '{}');
    if (flag in userPreferences) {
      enabled = userPreferences[flag];
    }
  } catch (error) {
    console.warn('Failed to load user feature preferences:', error);
  }
  
  return enabled;
}

export function getAllEnabledFeatures(clusterId?: ClusterId, store?: any): FeatureFlag[] {
  const enabledFeatures: FeatureFlag[] = [];
  
  for (const flag of Object.values(FEATURE_FLAGS)) {
    if (featureEnabled(flag, clusterId, store)) {
      enabledFeatures.push(flag);
    }
  }
  
  return enabledFeatures;
}

export function getFeatureConfig(flag: FeatureFlag): FeatureConfig | null {
  return RELEASE_FEATURES[flag] || null;
}

export function isExperimentalFeature(flag: FeatureFlag): boolean {
  const config = getFeatureConfig(flag);
  return config?.experimental === true;
}

export function setUserFeaturePreference(flag: FeatureFlag, enabled: boolean): void {
  try {
    const preferences = JSON.parse(localStorage.getItem('suseai-feature-flags') || '{}');
    preferences[flag] = enabled;
    localStorage.setItem('suseai-feature-flags', JSON.stringify(preferences));
  } catch (error) {
    console.error('Failed to save user feature preference:', error);
  }
}

export function resetUserFeaturePreferences(): void {
  try {
    localStorage.removeItem('suseai-feature-flags');
  } catch (error) {
    console.error('Failed to reset user feature preferences:', error);
  }
}

// === Feature Flag Validation ===
export function validateClusterForFeature(
  flag: FeatureFlag,
  clusterId: ClusterId,
  store: any
): { valid: boolean; reason?: string } {
  const config = getFeatureConfig(flag);
  if (!config) {
    return { valid: false, reason: `Unknown feature: ${flag}` };
  }
  
  const clusterVersion = getClusterVersion(clusterId, store);
  
  // Check version requirements
  if (!checkVersionRequirements(clusterVersion, config.requirements)) {
    const reasons = [];
    
    if (config.requirements.minRancherVersion) {
      if (!isVersionGTE(clusterVersion.rancher, config.requirements.minRancherVersion)) {
        reasons.push(`Rancher ${config.requirements.minRancherVersion}+ required (current: ${clusterVersion.rancher})`);
      }
    }
    
    if (config.requirements.minKubernetesVersion) {
      if (!isVersionGTE(clusterVersion.kubernetes, config.requirements.minKubernetesVersion)) {
        reasons.push(`Kubernetes ${config.requirements.minKubernetesVersion}+ required (current: ${clusterVersion.kubernetes})`);
      }
    }
    
    if (config.requirements.minHelmVersion && clusterVersion.helm) {
      if (!isVersionGTE(clusterVersion.helm, config.requirements.minHelmVersion)) {
        reasons.push(`Helm ${config.requirements.minHelmVersion}+ required (current: ${clusterVersion.helm})`);
      }
    }
    
    return { valid: false, reason: reasons.join(', ') };
  }
  
  return { valid: true };
}

// === Debug and Development Utilities ===
export function debugFeatureFlags(clusterId?: ClusterId, store?: any): void {
  console.group('🏁 SUSE AI Feature Flags');
  
  for (const [flagName, config] of Object.entries(RELEASE_FEATURES)) {
    const enabled = featureEnabled(flagName as FeatureFlag, clusterId, store);
    const status = enabled ? '✅ ENABLED' : '❌ DISABLED';
    const experimental = config.experimental ? ' [EXPERIMENTAL]' : '';
    const deprecated = config.deprecated ? ' [DEPRECATED]' : '';
    
    console.log(`${status} ${config.displayName}${experimental}${deprecated}`);
    
    if (!enabled && clusterId && store) {
      const validation = validateClusterForFeature(flagName as FeatureFlag, clusterId, store);
      if (!validation.valid && validation.reason) {
        console.log(`  └─ Reason: ${validation.reason}`);
      }
    }
  }
  
  console.groupEnd();
}

// Export types for use elsewhere
export type { FeatureFlag, ClusterId };