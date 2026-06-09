import { log as logger } from '../utils/logger';
import { createErrorHandler } from '../utils/error-handler';
import type {
  RancherStore,
  ClusterInfo,
  ClusterResource,
  AppCRD,
  ListResponse
} from '../types/rancher-types';

/**
 * Service for cluster and namespace operations
 */
export class ClusterService {

  /**
   * Get list of all clusters
   */
  static async getClusters($store: RancherStore): Promise<ClusterInfo[]> {
    try {
      const rows = await Promise.race([
        $store.dispatch('management/findAll', { type: 'cluster' }),
        new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), 20000))
      ]) as ClusterResource[];
      return (rows || []).map((c: ClusterResource) => ({
        id: c.id,
        name: c.metadata?.name || c.id
      }));
    } catch {
      const res = await $store.dispatch('rancher/request', {
        url: '/v1/management.cattle.io.clusters?limit=2000',
        timeout: 20000
      });
      const items = res?.data?.data || res?.data || [];
      return (items || []).map((c: ClusterResource) => ({
        id: c?.metadata?.name || c?.id,
        name: c?.metadata?.name || c?.id
      })).filter((x: ClusterInfo) => !!x.id);
    }
  }

  /**
   * Ensure namespace exists in cluster, create if missing
   */
  static async ensureNamespace(
    $store: RancherStore,
    clusterId: string,
    namespace: string
  ): Promise<void> {
    const getUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces/${encodeURIComponent(namespace)}`;

    try {
      await $store.dispatch('rancher/request', { url: getUrl, timeout: 20000 });
      logger.debug('Namespace exists', {
        component: 'ClusterService',
        data: { clusterId, namespace }
      });
    } catch {
      logger.info('Creating namespace', {
        component: 'ClusterService',
        data: { clusterId, namespace }
      });

      const createUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/api/v1/namespaces`;
      await $store.dispatch('rancher/request', {
        url: createUrl,
        method: 'POST',
        data: {
          apiVersion: 'v1',
          kind: 'Namespace',
          metadata: { name: namespace }
        },
        timeout: 20000
      });
    }
  }

  /**
   * Check if app exists in cluster namespace
   */
  static async appExists(
    $store: RancherStore,
    clusterId: string,
    namespace: string,
    release: string
  ): Promise<boolean> {
    const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/apis/catalog.cattle.io/v1/namespaces/${encodeURIComponent(namespace)}/apps/${encodeURIComponent(release)}`;

    try {
      await $store.dispatch('rancher/request', { url, timeout: 20000 });
      return true;
    } catch {
      return false;
    }
  }

  /**
   * List all catalog apps in a cluster
   */
  static async listCatalogApps($store: RancherStore, clusterId: string): Promise<AppCRD[]> {
    try {
      const res = await $store.dispatch('rancher/request', {
        url: `/k8s/clusters/${encodeURIComponent(clusterId)}/apis/catalog.cattle.io/v1/apps?limit=2000`,
        timeout: 20000
      });

      const items = res?.data?.items || res?.data || res?.items || [];

      logger.debug('Listed catalog apps', {
        component: 'ClusterService',
        data: { clusterId, count: items.length }
      });

      return Array.isArray(items) ? items : [];
    } catch (err) {
      logger.error('Failed to list catalog apps', err, {
        component: 'ClusterService'
      });
      return [];
    }
  }
}

// Legacy exports for backward compatibility
export const getClusters = ClusterService.getClusters;
export const ensureNamespace = ClusterService.ensureNamespace;
export const appExists = ClusterService.appExists;
export const listCatalogApps = ClusterService.listCatalogApps;