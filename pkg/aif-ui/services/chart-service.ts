import yaml from 'js-yaml';
import { log as logger } from '../utils/logger';
import { createChartValuesService } from './chart-values';
import { getClusterContext } from '../utils/cluster-operations';
import { filterAndSortVersions } from '../utils/chart-version';
import type {
  RancherStore,
  ClusterResource,
  RepositoryIndex,
  FileEntry
} from '../types/rancher-types';

// Helper functions for string matching and version sorting
function normName(s?: string): string {
  return (s || '').toLowerCase().replace(/[^a-z0-9]+/g, '');
}

function sameName(a?: string, b?: string): boolean {
  return !!a && !!b && normName(a) === normName(b);
}

// Helper functions for file content processing
function decodeMaybeB64(s?: string): string {
  if (!s || typeof s !== 'string') return '';
  try {
    const t = atob(s.replace(/\s+/g, ''));
    if (/[:\n]/.test(t)) return t; // looks like YAML
  } catch {}
  return s;
}

function textFromFileEntry(v: FileEntry): string {
  if (!v) return '';
  if (typeof v === 'string') return decodeMaybeB64(v);
  if (typeof v === 'object') {
    const candidates = [v.content, v.contents, v.data, v.base64, v.value, v.Value, v.text];
    for (const c of candidates) {
      if (typeof c === 'string' && c) return decodeMaybeB64(c);
    }
  }
  return '';
}

/**
 * Service for chart discovery and operations
 */
export class ChartService {

  /**
   * Get repository index link
   */
  private static async getRepoIndexLink($store: RancherStore, repoName: string): Promise<string | null> {
    
    const found = await getClusterContext($store, { repoName: repoName});
    if (!found) {
      logger.warn(`ClusterRepo "${repoName}" not found in any cluster`);
      return null;
    }
    const { baseApi } = found

    try {
      const repo = encodeURIComponent(repoName);

      const url = `${baseApi}/catalog.cattle.io.clusterrepos/${repo}`;
      const res = await $store.dispatch('rancher/request', { url, timeout: 20000 });

      const link = res?.data?.links?.index || res?.links?.index;

      logger.debug('Repository index link resolved', {
        component: 'ChartService',
        data: { repoName, link }
      });

      return link || null;
    } catch (err) {
      logger.error('Failed to get repository index link', err, {
        component: 'ChartService'
      });
      return null;
    }
  }

  /**
   * Get repository index data
   */
  private static async getRepoIndex($store: RancherStore, repoName: string): Promise<RepositoryIndex | null> {
    const indexLink = await this.getRepoIndexLink($store, repoName);
    if (!indexLink) return null;

    try {
      const res = await $store.dispatch('rancher/request', { url: indexLink, timeout: 20000 });
      const payload = (res?.data ?? res);

      logger.debug('Repository index fetched', {
        component: 'ChartService',
        data: { repoName, payloadType: typeof payload }
      });

      if (typeof payload === 'string') {
        return yaml.load(payload) as RepositoryIndex;
      }
      return payload as RepositoryIndex;
    } catch (err) {
      logger.error('Failed to fetch repository index', err, {
        component: 'ChartService'
      });
      return null;
    }
  }

  /**
   * List cluster repositories
   */
  private static async listClusterRepos($store: RancherStore): Promise<ClusterResource[]> {
    const { baseApi } = await getClusterContext($store);

    try {
      const res = await $store.dispatch('rancher/request', {
        url: `${baseApi}/catalog.cattle.io/v1/clusterrepos?limit=1000`,
        timeout: 20000
      });
      return res?.data?.items || res?.data || res?.items || [];
    } catch (err) {
      logger.error('Failed to list cluster repositories', err, {
        component: 'ChartService'
      });
      return [];
    }
  }

  /**
   * Find chart in repository by slug name
   */
  static async findChartInRepo(
    $store: RancherStore,
    _repoClusterId: string,
    repoName: string,
    slug: string
  ): Promise<{ chartName: string; version: string } | null> {
    try {
      const index = await this.getRepoIndex($store, repoName);
      const names = index?.entries ? Object.keys(index.entries) : [];
      const match = names.find((n: string) => sameName(n, slug));

      if (match && index) {
        const latest = filterAndSortVersions((index.entries[match] || []).map((v: { version: string }) => v.version))[0];

        if (latest) {
          logger.info('Chart found in repository', {
            component: 'ChartService',
            data: { repoName, slug, chartName: match, version: latest }
          });

          return { chartName: match, version: latest };
        }
      }

      return null;
    } catch (err) {
      logger.error('Failed to find chart in repository', err, {
        component: 'ChartService'
      });
      return null;
    }
  }

  /**
   * List available versions for a chart
   */
  static async listChartVersions(
    $store: RancherStore,
    _repoClusterId: string,
    repoName: string,
    chartName: string
  ): Promise<string[]> {
    try {
      const index = await this.getRepoIndex($store, repoName);
      const names = index?.entries ? Object.keys(index.entries) : [];
      const match = names.find((n: string) => sameName(n, chartName));

      if (match && index) {
        const out = filterAndSortVersions((index.entries[match] || []).map((v: { version: string }) => v.version));

        logger.debug('Chart versions listed', {
          component: 'ChartService',
          data: { chartName: match, count: out.length }
        });

        return out;
      }

      return [];
    } catch (err) {
      logger.error('Failed to list chart versions', err, {
        component: 'ChartService'
      });
      return [];
    }
  }

  /**
   * Fetch default values for a chart
   */
  static async fetchChartDefaultValues(
    $store: RancherStore,
    _repoClusterId: string,
    repoName: string,
    chartName: string,
    version: string
  ): Promise<string> {
    try {
      // Use simplified ChartValuesService instead of complex fallback chains
      const chartValuesService = createChartValuesService($store);
      return await chartValuesService.getDefaultValues(repoName, chartName, version);
    } catch (err) {
      logger.error('Failed to fetch chart default values', err, {
        component: 'ChartService'
      });
      return '';
    }
  }

  /**
   * Infer appropriate cluster repository for a chart
   */
  static async inferClusterRepoForChart(
    $store: RancherStore,
    chartName: string,
    preferVersion?: string
  ): Promise<string | null> {
    try {
      const repos = await this.listClusterRepos($store);
      let best: string | null = null;

      for (const r of repos) {
        const name = r?.metadata?.name;
        if (!name) continue;

        try {
          const index = await this.getRepoIndex($store, name);
          const entries = index?.entries || {};
          const foundKey = Object.keys(entries).find((k) => sameName(k, chartName));
          if (!foundKey) continue;

          if (preferVersion) {
            const versions: string[] = (entries[foundKey] || [])
              .map((e: { version: string }) => e?.version)
              .filter(Boolean);
            if (versions.includes(preferVersion)) {
              logger.info('Perfect chart repository match found', {
                component: 'ChartService',
                data: { chartName, repoName: name, version: preferVersion }
              });
              return name; // perfect match
            }
          }

          if (!best) best = name; // fallback
        } catch {
          // Continue to next repo on error
        }
      }

      if (best) {
        logger.info('Fallback chart repository found', {
          component: 'ChartService',
          data: { chartName, repoName: best }
        });
      }

      return best;
    } catch (err) {
      logger.error('Failed to infer cluster repository for chart', err, {
        component: 'ChartService'
      });
      return null;
    }
  }
}

// Legacy exports for backward compatibility
export const findChartInRepo = ChartService.findChartInRepo;
export const listChartVersions = ChartService.listChartVersions;
export const fetchChartDefaultValues = ChartService.fetchChartDefaultValues;
export const inferClusterRepoForChart = ChartService.inferClusterRepoForChart;