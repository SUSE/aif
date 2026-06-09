/**
 * Chart Values Service - Simplified API patterns
 * Replaces complex fallback chains with simple, reliable endpoints
 */

import type { RancherStore } from '../types/rancher-types';
import { logger } from '../utils/logger';
import { getClusterContext } from '../utils/cluster-operations';

/**
 * Extract a file from a tar.gz buffer by filename suffix (e.g., 'values.yaml', 'chart.yaml')
 */
export async function extractFileFromTarGz(buffer: ArrayBuffer, filenameSuffix: string): Promise<string | null> {
  try {
    let tarBuffer = buffer;

    // Try to decompress if gzipped
    if (typeof (window as any).DecompressionStream === 'function') {
      try {
        const ds = new (window as any).DecompressionStream('gzip');
        const stream = new Response(new Blob([buffer]).stream().pipeThrough(ds));
        tarBuffer = await stream.arrayBuffer();
      } catch {
        // Might not be gzipped, try as-is
      }
    }

    // Parse tar archive
    const view = new DataView(tarBuffer);
    const decoder = new TextDecoder('utf-8');
    let offset = 0;
    const blockSize = 512;
    const suffixLower = filenameSuffix.toLowerCase();

    while (offset + blockSize <= tarBuffer.byteLength) {
      // Check for end of archive
      let isEmpty = true;
      for (let i = 0; i < blockSize; i++) {
        if (view.getUint8(offset + i) !== 0) {
          isEmpty = false;
          break;
        }
      }
      if (isEmpty) break;

      // Read filename
      const nameBytes = new Uint8Array(tarBuffer, offset, 100);
      let nameEnd = 0;
      while (nameEnd < nameBytes.length && nameBytes[nameEnd] !== 0) nameEnd++;
      const filename = decoder.decode(nameBytes.subarray(0, nameEnd));

      // Read file size (octal)
      const sizeBytes = new Uint8Array(tarBuffer, offset + 124, 12);
      let sizeEnd = 0;
      while (sizeEnd < sizeBytes.length && sizeBytes[sizeEnd] !== 0) sizeEnd++;
      const sizeStr = decoder.decode(sizeBytes.subarray(0, sizeEnd)).trim();
      const fileSize = parseInt(sizeStr.replace(/[^0-7]/g, ''), 8) || 0;

      // Check if this is the target file
      if (filename.toLowerCase().endsWith(suffixLower)) {
        const fileData = new Uint8Array(tarBuffer, offset + blockSize, fileSize);
        return decoder.decode(fileData);
      }

      offset += blockSize + Math.ceil(fileSize / blockSize) * blockSize;
    }
  } catch { /* extraction failed */ }
  return null;
}

export class ChartValuesService {
  private store: RancherStore;

  constructor(store: RancherStore) {
    this.store = store;
  }

  /**
   * Get default values for a chart using working API patterns
   * Uses proven approaches: files link, file link, and tar.gz fallback
   */
  async getDefaultValues(repo: string, chart: string, version: string): Promise<string> {
    try {
      logger.debug('Fetching chart default values', {
        component: 'ChartValuesService',
        action: 'getDefaultValues',
        data: { repo, chart, version }
      });

      // Try 1: ?link=files approach (fastest when it works)
      let values = await this.tryFilesLink(repo, chart, version);
      if (values) return values;

      // Try 2: ?link=file approach (direct values.yaml fetch)
      values = await this.tryFileLink(repo, chart, version);
      if (values) return values;

      // Try 3: ?link=chart tar.gz approach (most reliable fallback)
      values = await this.tryTarGzLink(repo, chart, version);
      if (values) return values;

      // Fallback: return minimal template
      logger.warn('All chart value fetching methods failed, using minimal template', {
        component: 'ChartValuesService',
        action: 'fallback',
        data: { repo, chart, version }
      });

      return this.getMinimalValuesTemplate(chart);

    } catch (error) {
      logger.warn('Failed to fetch chart values, using minimal template', {
        component: 'ChartValuesService',
        action: 'error',
        data: { error: error instanceof Error ? error.message : String(error) }
      });

      return this.getMinimalValuesTemplate(chart);
    }
  }

  /**
   * Try fetching via ?link=files approach
   */
  private async tryFilesLink(repo: string, chart: string, version: string): Promise<string | null> {

    const found = await getClusterContext(this.store, {repoName: repo});
    if (!found) {
      logger.warn(`ClusterRepo "${repo}" not found in any cluster`);
      return null;
    }

    try {
      const { baseApi } = found;
      const url = `${baseApi}/catalog.cattle.io.clusterrepos/${encodeURIComponent(repo)}?link=files&chartName=${encodeURIComponent(chart)}&version=${encodeURIComponent(version)}`;
      const response = await this.store.dispatch('rancher/request', { url, timeout: 20000 });
      const filesDetail = response?.data ?? response;

      if (filesDetail && typeof filesDetail === 'object') {
        const values = this.extractValuesFromFiles(filesDetail);
        if (values) {
          logger.debug('Found values via files approach', {
            component: 'ChartValuesService',
            action: 'success',
            data: { method: 'files', length: values.length }
          });
          return values;
        }
      }
    } catch (error) {
      // Silently continue to next method
    }
    return null;
  }

  /**
   * Try fetching via ?link=file approach for values.yaml directly
   */
  private async tryFileLink(repo: string, chart: string, version: string): Promise<string | null> {
    const filenames = ['values.yaml', 'values.yml'];
    const found = await getClusterContext(this.store, { repoName: repo});
    if (!found) {
      logger.warn(`ClusterRepo "${repo}" not found in any cluster`);
      return null;
    }

    for (const filename of filenames) {
      try {
        const { baseApi } = found;
        const url = `${baseApi}/catalog.cattle.io.clusterrepos/${encodeURIComponent(repo)}?link=file&chartName=${encodeURIComponent(chart)}&version=${encodeURIComponent(version)}&name=${encodeURIComponent(filename)}`;
        const response = await this.store.dispatch('rancher/request', { url, timeout: 20000 });
        const text = this.extractTextFromFileEntry(response?.data ?? response);

        if (text && text.includes(':')) { // Basic YAML validation
          logger.debug('Found values via file approach', {
            component: 'ChartValuesService',
            action: 'success',
            data: { method: 'file', filename, length: text.length }
          });
          return text;
        }
      } catch (error) {
        // Continue trying other filenames
      }
    }
    return null;
  }

  /**
   * Try fetching via ?link=chart tar.gz approach
   */
  private async tryTarGzLink(repo: string, chart: string, version: string): Promise<string | null> {
    const found = await getClusterContext(this.store, { repoName: repo});
    if (!found) {
      logger.warn(`ClusterRepo "${repo}" not found in any cluster`);
      return null;
    }

    try {
      const { baseApi } = found;
      const url = `${baseApi}/catalog.cattle.io.clusterrepos/${encodeURIComponent(repo)}?link=chart&chartName=${encodeURIComponent(chart)}&version=${encodeURIComponent(version)}`;
      const response = await this.store.dispatch('rancher/request', {
        url,
        responseType: 'arraybuffer',
        headers: { Accept: 'application/gzip, application/x-gzip, application/octet-stream' },
        timeout: 20000
      });

      const buffer = response?.data ?? response;
      if (buffer instanceof ArrayBuffer) {
        // Try values.yaml first, then values.yml
        let values = await extractFileFromTarGz(buffer, 'values.yaml');
        if (!values) {
          values = await extractFileFromTarGz(buffer, 'values.yml');
        }
        if (values) {
          logger.debug('Found values via tar.gz approach', {
            component: 'ChartValuesService',
            action: 'success',
            data: { method: 'tar.gz', length: values.length }
          });
          return values;
        }
      }
    } catch (error) {
      // Expected to fail sometimes
    }
    return null;
  }

  /**
   * Extract values.yaml from files structure
   */
  private extractValuesFromFiles(files: any): string | null {
    if (!files) return null;

    // Handle object format
    if (!Array.isArray(files) && typeof files === 'object') {
      for (const key of Object.keys(files)) {
        if (key.toLowerCase().endsWith('values.yaml') || key.toLowerCase().endsWith('values.yml')) {
          return this.extractTextFromFileEntry(files[key]);
        }
      }
    }

    // Handle array format
    if (Array.isArray(files)) {
      const valuesFile = files.find((file: any) =>
        file?.name &&
        (file.name.toLowerCase().endsWith('values.yaml') || file.name.toLowerCase().endsWith('values.yml'))
      );
      if (valuesFile) {
        return this.extractTextFromFileEntry(valuesFile);
      }
    }

    return null;
  }

  /**
   * Extract text content from various file entry formats
   */
  private extractTextFromFileEntry(entry: any): string | null {
    if (!entry) return null;

    // Try different property names for the content
    const candidates = [
      entry.content,
      entry.data,
      entry.text,
      entry
    ];

    for (const candidate of candidates) {
      if (typeof candidate === 'string' && candidate.trim()) {
        // Try to decode if it appears to be base64
        try {
          if (candidate.match(/^[A-Za-z0-9+/=]+$/)) {
            const decoded = atob(candidate);
            if (decoded.includes(':')) return decoded;
          }
        } catch {
          // Not base64, continue
        }

        // Return as-is if it looks like YAML
        if (candidate.includes(':')) return candidate;
      }
    }

    return null;
  }

  /**
   * Generate minimal values template (only when we can't fetch real values)
   */
  private getMinimalValuesTemplate(chartName: string): string {
    return `# Values for ${chartName}
# Unable to fetch default values - please configure as needed

# Common configuration options:
# image:
#   repository: ""
#   tag: ""
#   pullPolicy: IfNotPresent

# resources:
#   limits:
#     cpu: 1000m
#     memory: 1Gi
#   requests:
#     cpu: 100m
#     memory: 128Mi

# service:
#   type: ClusterIP
#   port: 80
`;
  }

  /**
   * Simple error handling with user feedback
   */
  private handleError(message: string, error: unknown, context?: Record<string, unknown>) {
    const errorMessage = error instanceof Error ? error.message : String(error);

    if (process.env.NODE_ENV === 'development') {
      logger.warn(`[ChartValuesService] ${message}`, {
        component: 'ChartValuesService',
        action: 'error',
        data: { error: errorMessage, context }
      });
    }

    // Proper error reporting to user via Rancher's notification system
    this.store.dispatch('growl/error', {
      title: 'Chart Values Error',
      message: `${message}. Please try again or contact support if the issue persists.`
    });
  }


  /**
   * Validate chart values YAML
   */
  async validateValues(valuesYaml: string): Promise<{ valid: boolean; error?: string }> {
    try {
      if (!valuesYaml.trim()) {
        return { valid: true }; // Empty values are valid
      }

      // Use Rancher's YAML validation if available
      const response = await this.store.dispatch('rancher/request', {
        url: '/v1/management.cattle.io/validate-yaml',
        method: 'POST',
        data: { yaml: valuesYaml },
        timeout: 20000
      });

      return { valid: response?.valid !== false };
    } catch (error) {
      // Fallback to basic YAML parsing validation
      try {
        const yaml = await import('js-yaml');
        yaml.load(valuesYaml);
        return { valid: true };
      } catch (yamlError) {
        return {
          valid: false,
          error: yamlError instanceof Error ? yamlError.message : 'Invalid YAML format'
        };
      }
    }
  }

  /**
   * Get available chart versions for a repository
   */
  async getChartVersions(repo: string, chart: string): Promise<string[]> {
    const found = await getClusterContext(this.store, { repoName: repo});
    if (!found) {
      logger.warn(`ClusterRepo "${repo}" not found in any cluster`);
      return [];
    }

    try {
      const { baseApi } = found;
      const response = await this.store.dispatch('rancher/request', {
        url: `${baseApi}/catalog.cattle.io.clusterrepos/${encodeURIComponent(repo)}/charts/${encodeURIComponent(chart)}/versions`,
        timeout: 20000
      });

      const versions = response?.data || response || [];
      return Array.isArray(versions) ? versions.map((v: { version?: string }) => v.version).filter((v): v is string => Boolean(v)) : [];
    } catch (error) {
      logger.warn('Failed to fetch chart versions', {
        component: 'ChartValuesService',
        action: 'getChartVersions',
        data: { repo, chart, error: error instanceof Error ? error.message : String(error) }
      });
      return [];
    }
  }

  /**
   * Simple retry logic for critical operations
   */
  private async withRetry<T>(
    operation: () => Promise<T>,
    maxRetries = 2,
    delayMs = 1000
  ): Promise<T> {
    let lastError: unknown;

    for (let attempt = 0; attempt <= maxRetries; attempt++) {
      try {
        return await operation();
      } catch (error) {
        lastError = error;

        if (attempt < maxRetries) {
          logger.debug(`Retrying operation (attempt ${attempt + 1}/${maxRetries})`, {
            component: 'ChartValuesService',
            action: 'retry',
            data: { error: error instanceof Error ? error.message : String(error) }
          });
          await new Promise(resolve => setTimeout(resolve, delayMs));
        }
      }
    }

    throw lastError;
  }
}

/**
 * Factory function to create ChartValuesService instance
 */
export function createChartValuesService(store: RancherStore): ChartValuesService {
  return new ChartValuesService(store);
}