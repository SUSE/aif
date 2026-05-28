/**
 * Central type constants for SUSE AI Factory extension.
 */

export const PRODUCT_NAME = 'ai-factory';
export const MANAGEMENT_CLUSTER = 'local';

// Operator service coordinates used to build the Rancher proxy base URL.
// Adjust OPERATOR_SERVICE to match your Helm release name if it differs from 'aif-operator'.
export const OPERATOR_NAMESPACE = 'aif';
export const OPERATOR_SERVICE   = 'aif-operator';
export const OPERATOR_PORT      = 8080;

export const PAGE_IDS = {
  OVERVIEW:        'overview',
  APPS:            'apps',
  BLUEPRINTS:      'blueprints',
  BUNDLES:         'bundles',
  WORKLOADS:       'workloads',
  PENDING_REVIEWS: 'pending-reviews',
  SETTINGS:        'settings'
} as const;

export const CRD_TYPES = {
  BUNDLE: 'ai.suse.com.bundle',
  BLUEPRINT: 'ai.suse.com.blueprint',
  WORKLOAD: 'ai.suse.com.workload',
  SETTINGS: 'ai.suse.com.settings'
} as const;

export type PageId = typeof PAGE_IDS[keyof typeof PAGE_IDS];
export type CrdType = typeof CRD_TYPES[keyof typeof CRD_TYPES];
