import type { StaticAppCatalog } from '../types/app-types';
import type { AppCollectionItem } from './app-collection';
import { log as logger } from '../utils/logger';

/**
 * Validate that the catalog structure matches StaticAppCatalog interface
 */
export function validateCatalogStructure(catalog: any): asserts catalog is StaticAppCatalog {
  if (!catalog || typeof catalog !== 'object') {
    throw new Error('Invalid catalog: not an object');
  }
  if (!Array.isArray(catalog['suse-ai'])) {
    throw new Error('Invalid catalog: suse-ai is not an array');
  }
  if (!Array.isArray(catalog['nvidia'])) {
    throw new Error('Invalid catalog: nvidia is not an array');
  }

  logger.debug('Catalog structure validated successfully', {
    component: 'StaticCatalog',
    data: {
      suseAiCount: catalog['suse-ai'].length,
      nvidiaCount: catalog['nvidia'].length
    }
  });
}

/**
 * Transform static catalog into separate arrays with library tags
 */
export function transformStaticCatalog(catalog: StaticAppCatalog): {
  suseAi: AppCollectionItem[];
  nvidia: AppCollectionItem[];
} {
  logger.debug('Transforming static catalog', {
    component: 'StaticCatalog'
  });

  const suseAi = catalog['suse-ai'].map(app => ({
    ...app,
    library: 'suse-ai' as const
  }));

  const nvidia = catalog['nvidia'].map(app => ({
    ...app,
    library: 'nvidia' as const
  }));

  logger.info('Static catalog transformed successfully', {
    component: 'StaticCatalog',
    data: {
      suseAiCount: suseAi.length,
      nvidiaCount: nvidia.length
    }
  });

  return { suseAi, nvidia };
}

/**
 * Fetch static catalog from remote URL or bundled default
 */
export async function fetchStaticCatalog(): Promise<StaticAppCatalog> {
  const remoteUrl = process.env.VUE_APP_CATALOG_URL;

  logger.debug('Fetching static catalog', {
    component: 'StaticCatalog',
    data: { remoteUrl: remoteUrl || 'bundled' }
  });

  // Try remote URL if configured
  if (remoteUrl) {
    try {
      logger.debug('Attempting to fetch remote catalog', {
        component: 'StaticCatalog',
        data: { url: remoteUrl }
      });

      const response = await fetch(remoteUrl);
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      const catalog = await response.json();
      validateCatalogStructure(catalog);

      logger.info('Remote catalog fetched successfully', {
        component: 'StaticCatalog',
        data: { url: remoteUrl }
      });

      return catalog;
    } catch (err: any) {
      logger.error('Failed to fetch remote catalog, falling back to bundled', err, {
        component: 'StaticCatalog',
        data: { url: remoteUrl }
      });
      // Fall through to bundled catalog
    }
  }

  // Load bundled catalog
  logger.debug('Using bundled catalog', {
    component: 'StaticCatalog'
  });

  const catalog = require('../assets/app-catalog.json') as StaticAppCatalog;
  validateCatalogStructure(catalog);

  logger.info('Bundled catalog loaded successfully', {
    component: 'StaticCatalog'
  });

  return catalog;
}
