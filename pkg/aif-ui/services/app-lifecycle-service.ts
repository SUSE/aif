/**
 * @fileoverview Application lifecycle service for SUSE AI extension
 * Handles installation, upgrading, and deletion of applications through Rancher's app management system.
 * This service provides a high-level interface for app lifecycle operations while handling
 * the complexity of Rancher's API calls and error handling.
 */

import { log as logger } from '../utils/logger';
import { createErrorHandler, handleSimpleError } from '../utils/error-handler';
import type {
  RancherStore,
  AppCRD,
  InstallationPayload
} from '../types/rancher-types';

/**
 * Reference to a Helm chart in a cluster repository
 * @interface ChartRef
 */
export interface ChartRef {
  /** Name of the ClusterRepo containing the chart */
  repoName: string;
  /** Name of the chart within the repository */
  chartName: string;
  /** SemVer version of the chart */
  version: string;
}

/**
 * Service for managing application lifecycle operations
 *
 * This service provides high-level methods for installing, upgrading, and deleting
 * applications in Rancher-managed clusters. It handles the complexity of Rancher's
 * API calls, error handling, and state management.
 *
 * @example
 * ```typescript
 * // Install an application
 * await AppLifecycleService.createOrUpgradeApp(
 *   store,
 *   'c-m-xyz',
 *   'default',
 *   'my-app',
 *   { repoName: 'suseai', chartName: 'llama-cpp', version: '1.0.0' },
 *   { cpu: '2', memory: '4Gi' }
 * );
 *
 * // Wait for installation to complete
 * const app = await AppLifecycleService.waitForAppInstall(
 *   store,
 *   'c-m-xyz',
 *   'default',
 *   'my-app'
 * );
 *
 * // Delete the application
 * await AppLifecycleService.deleteApp(store, 'c-m-xyz', 'default', 'my-app');
 * ```
 */
export class AppLifecycleService {

  /**
   * Create or upgrade an application in a Rancher-managed cluster
   *
   * This method handles both installation and upgrade scenarios:
   * - If the app doesn't exist, it will be installed
   * - If the app exists, it will be upgraded
   * - Uses Rancher's cluster repository API for operations
   *
   * @param $store - Rancher store instance for API calls
   * @param clusterId - Target cluster ID (e.g., 'c-m-xyz')
   * @param namespace - Kubernetes namespace for the application
   * @param releaseName - Unique name for the Helm release
   * @param chart - Chart reference including repository, chart name, and version
   * @param values - Helm values to customize the installation
   * @param preferredAction - Preferred action if the operation is ambiguous
   * @throws Error if installation/upgrade fails or cluster is unreachable
   *
   * @example
   * ```typescript
   * await AppLifecycleService.createOrUpgradeApp(
   *   store,
   *   'c-m-xyz',
   *   'suseai-apps',
   *   'llama-model-1',
   *   { repoName: 'suseai', chartName: 'llama-cpp', version: '1.2.0' },
   *   { cpu: '4', memory: '8Gi', modelSize: 'large' }
   * );
   * ```
   */
  static async createOrUpgradeApp(
    $store: RancherStore,
    clusterId: string,
    namespace: string,
    releaseName: string,
    chart: ChartRef,
    values: Record<string, unknown>,
    preferredAction: 'install' | 'upgrade' = 'install'
  ): Promise<{ upgraded: boolean }> {

    const errorHandler = createErrorHandler($store, 'AppLifecycleService');

    logger.info('Starting app lifecycle operation', {
      component: 'AppLifecycleService',
      data: {
        clusterId,
        namespace,
        releaseName,
        chart,
        preferredAction,
        valuesSize: JSON.stringify(values || {}).length
      }
    });

    const clusterReposUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/v1/catalog.cattle.io.clusterrepos/${chart.repoName}?action=${preferredAction}`;
    const appsUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/apis/catalog.cattle.io/v1/namespaces/${encodeURIComponent(namespace)}/apps`;
    const appUrl = `${appsUrl}/${encodeURIComponent(releaseName)}`;

    try {
      logger.debug('Fetching projects for cluster', {
        component: 'AppLifecycleService',
        data: { clusterId }
      });

      const charts = [
        {
          chartName: chart.chartName,
          version: chart.version,
          releaseName,
          annotations: {
            'catalog.cattle.io/ui-source-repo-type': 'cluster',
            'catalog.cattle.io/ui-source-repo': chart.repoName
          },
          values
        }
      ];

      const appPayload = {
        apiVersion: 'catalog.cattle.io/v1',
        kind: 'App',
        metadata: {
          namespace,
          name: releaseName,
          labels: { 'catalog.cattle.io/cluster-repo-name': chart.repoName },
          resourceVersion: undefined as string | undefined
        },
        spec: {
          chart: {
            metadata: {
              name: chart.chartName,
              version: chart.version,
            }
          },
          name: releaseName,
          namespace: namespace,
          values,
        },
      };

      // For upgrade actions, use the clusterRepo action directly
      if (preferredAction === 'upgrade') {
        logger.info('Performing upgrade via clusterRepo action', {
          component: 'AppLifecycleService',
          data: { releaseName }
        });

        const upgradeData = {
          charts,
          namespace,
          clusterId,
          wait: true,
          timeout: '600s',
          noHooks: false,
          disableOpenAPIValidation: false,
          skipCRDs: false
        };

        try {
          const upgradeResult = await $store.dispatch('rancher/request', {
            method: 'post',
            url: clusterReposUrl,
            data: upgradeData,
            timeout: 20000
          });

          logger.info('App upgrade successful', {
            component: 'AppLifecycleService',
            data: { releaseName }
          });

          return { upgraded: true };
        } catch (upgradeError: unknown) {
          const standardError = errorHandler.handleApiError(upgradeError, 'upgrade', { releaseName, namespace });
          throw new Error(`Failed to upgrade app: ${standardError.message}`);
        }
      }

      // For install actions, check if app exists first and use upgrade if it does
      try {
        logger.debug('Checking for existing app', {
          component: 'AppLifecycleService',
          data: { namespace, releaseName }
        });

        await $store.dispatch('rancher/request', { url: appUrl, timeout: 20000 });

        // App exists — use clusterRepo upgrade action to re-trigger Helm
        logger.info('App exists, performing upgrade via clusterRepo action', {
          component: 'AppLifecycleService',
          data: { releaseName }
        });

        const upgradeUrl = `/k8s/clusters/${encodeURIComponent(clusterId)}/v1/catalog.cattle.io.clusterrepos/${chart.repoName}?action=upgrade`;
        const upgradeData = {
          charts,
          namespace,
          clusterId,
          wait: true,
          timeout: '600s',
          noHooks: false,
          disableOpenAPIValidation: false,
          skipCRDs: false
        };

        try {
          await $store.dispatch('rancher/request', {
            method: 'post',
            url: upgradeUrl,
            data: upgradeData,
            timeout: 20000
          });

          logger.info('App upgrade successful', {
            component: 'AppLifecycleService',
            data: { releaseName }
          });

          return { upgraded: true };
        } catch (upgradeError: unknown) {
          const upgradeStandardError = errorHandler.handleApiError(upgradeError, 'upgrade', { releaseName, namespace });
          throw new Error(`Failed to upgrade app: ${upgradeStandardError.message}`);
        }
      } catch (e: unknown) {
        const standardError = errorHandler.normalizeError(e);

        if (standardError.status === 404) {
          logger.info('App does not exist, performing install', {
            component: 'AppLifecycleService',
            data: { releaseName }
          });

          const installData = {
            charts,
            namespace,
            clusterId,
            wait: true,
            timeout: '600s',
            noHooks: false,
            disableOpenAPIValidation: false,
            skipCRDs: false
          };

          try {
            await $store.dispatch('rancher/request', {
              method: 'post',
              url: clusterReposUrl,
              data: installData,
              timeout: 20000
            });

            logger.info('App install successful', {
              component: 'AppLifecycleService',
              data: { releaseName }
            });
          } catch (installError: unknown) {
            const installStandardError = errorHandler.handleApiError(installError, 'install', { releaseName, namespace });
            throw new Error(`Failed to install app: ${installStandardError.message}`);
          }
        } else {
          // For non-404 errors during app check, handle and re-throw
          errorHandler.handleApiError(e, 'check-app', { releaseName, namespace, status: standardError.status });
          throw e; // Re-throw original error to be caught by outer handler
        }
      }
    } catch (projectError: unknown) {
      // Only handle if this is a new error, not a re-thrown error from inner catch
      if (projectError instanceof Error && projectError.message.includes('Failed to')) {
        // Already handled, just re-throw
        throw projectError;
      }
      const standardError = errorHandler.handleApiError(projectError, 'fetch-projects', { operation: 'fetch projects' });
      throw new Error(`Failed to fetch projects: ${standardError.message}`);
    }

    logger.info('App lifecycle operation completed', {
      component: 'AppLifecycleService',
      data: { releaseName }
    });

    return { upgraded: false };
  }

  /**
   * Wait for application installation or upgrade to complete
   *
   * Polls the application's status until the installation is complete or timeout is reached.
   * Monitors the observedGeneration vs generation to determine completion state.
   *
   * @param $store - Rancher store instance for API calls
   * @param clusterId - Target cluster ID
   * @param namespace - Kubernetes namespace containing the application
   * @param releaseName - Name of the Helm release to monitor
   * @param timeoutMs - Maximum time to wait in milliseconds (default: 90 seconds)
   * @returns Promise resolving to the complete App CRD
   * @throws Error if timeout is reached or installation fails
   *
   * @example
   * ```typescript
   * const app = await AppLifecycleService.waitForAppInstall(
   *   store,
   *   'c-m-xyz',
   *   'suseai-apps',
   *   'llama-model-1',
   *   120000 // 2 minutes timeout
   * );
   * console.log('App status:', app.status.summary.state);
   * ```
   */
  static async waitForAppInstall(
    $store: RancherStore,
    clusterId: string,
    namespace: string,
    releaseName: string,
    timeoutMs = 90_000,
    isRetry = false
  ): Promise<AppCRD> {
    const errorHandler = createErrorHandler($store, 'AppLifecycleService');
    const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/apis/catalog.cattle.io/v1/namespaces/${encodeURIComponent(namespace)}/apps/${encodeURIComponent(releaseName)}`;
    const start = Date.now();
    let lastErr: unknown = null;

    logger.info('Waiting for app install to complete', {
      component: 'AppLifecycleService',
      data: { clusterId, namespace, releaseName, timeoutMs }
    });

    let initialObs = -1;
    let initialState = '';

    for (;;) {
      let app: any = null;

      try {
        const r = await $store.dispatch('rancher/request', { url, timeout: 20000 });
        app = (r?.data ?? r) || {};
      } catch (e: unknown) {
        lastErr = e;
        const standardError = errorHandler.normalizeError(e);

        if (standardError.status && standardError.status !== 404) {
          logger.warn('Non-404 error during app wait', {
            component: 'AppLifecycleService',
            data: { releaseName, statusCode: standardError.status }
          });
        }
      }

      if (app) {
        const gen = app?.metadata?.generation ?? 0;
        const obs = app?.status?.observedGeneration ?? 0;
        const sum = app?.status?.summary || {};
        const state = sum?.state || app?.status?.conditions?.find((c: { type: string; status: string }) => c?.type === 'Ready')?.status || 'Unknown';

        if (initialObs < 0) {
          initialObs = obs;
          initialState = (state || '').toLowerCase();
        }

        logger.debug('App status check', {
          component: 'AppLifecycleService',
          data: {
            releaseName, generation: gen, observedGeneration: obs, initialObs, state,
            'metadata.state': app?.metadata?.state,
            'status.summary': sum,
            'status.conditions': app?.status?.conditions
          }
        });

        if (obs >= gen) {
          // On retry/upgrade, wait for observedGeneration to increment
          // to avoid reading stale status from the previous operation
          const isStale = isRetry && obs <= initialObs;
          if (!isStale) {
            const lowerState = (state || '').toLowerCase();
            if (lowerState === 'failed' || lowerState === 'error') {
              const errMsg = app?.metadata?.state?.message
                || (typeof sum?.error === 'string' ? sum.error : null)
                || `Helm install failed (state: ${state})`;
              logger.error('App install failed', {
                component: 'AppLifecycleService',
                data: { releaseName, state, errMsg }
              });
              throw new Error(errMsg);
            }

            // Only return for terminal success states; keep polling for transitional states
            const transitional = ['installing', 'pending', 'pending-install', 'pending-upgrade', 'pending-rollback'];
            if (!transitional.includes(lowerState)) {
              logger.info('App install completed successfully', {
                component: 'AppLifecycleService',
                data: { releaseName }
              });
              return app;
            }
          }
        }
      }

      if (Date.now() - start > timeoutMs) {
        const msg = lastErr ? handleSimpleError(lastErr, 'App did not appear in time') : 'App did not appear in time';
        throw new Error(msg);
      }
      await new Promise(r => setTimeout(r, 1500));
    }
  }

  /**
   * Delete an application from a Rancher-managed cluster
   *
   * Uninstalls the application by calling Rancher's uninstall action.
   * This will remove the Helm release and associated Kubernetes resources.
   *
   * @param $store - Rancher store instance for API calls
   * @param clusterId - Target cluster ID
   * @param namespace - Kubernetes namespace containing the application
   * @param releaseName - Name of the Helm release to delete
   * @param repoName - Optional repository name for logging/context
   * @throws Error if deletion fails or application is not found
   *
   * @example
   * ```typescript
   * await AppLifecycleService.deleteApp(
   *   store,
   *   'c-m-xyz',
   *   'suseai-apps',
   *   'llama-model-1',
   *   'suseai'
   * );
   * ```
   */
  static async deleteApp(
    $store: RancherStore,
    clusterId: string,
    namespace: string,
    releaseName: string,
    repoName?: string
  ): Promise<void> {
    try {
      const url = `/k8s/clusters/${encodeURIComponent(clusterId)}/v1/catalog.cattle.io.apps/${encodeURIComponent(namespace)}/${encodeURIComponent(releaseName)}?action=uninstall`;

      logger.info('Deleting app', {
        component: 'AppLifecycleService',
        data: { clusterId, namespace, releaseName, repoName }
      });

      await $store.dispatch('rancher/request', {
        url,
        method: 'POST',
        data: { timeout: '600s' },
        timeout: 20000
      });

      // Wait a bit for the deletion to process
      await new Promise(resolve => setTimeout(resolve, 5000));

      logger.info('App deleted successfully', {
        component: 'AppLifecycleService',
        data: { releaseName }
      });
    } catch (e: unknown) {
      const errorMsg = handleSimpleError(e, 'Failed to delete app');
      logger.error('Failed to delete app', e, {
        component: 'AppLifecycleService',
        data: { releaseName, error: errorMsg }
      });
      throw e;
    }
  }
}

// Legacy exports for backward compatibility
export const createOrUpgradeApp = AppLifecycleService.createOrUpgradeApp;
export const waitForAppInstall = AppLifecycleService.waitForAppInstall;
export const deleteApp = AppLifecycleService.deleteApp;