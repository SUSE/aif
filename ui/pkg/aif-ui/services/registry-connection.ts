import type { Dispatchable } from '../types/rancher-types';
import { getSettings, validateCredentials } from '../utils/operator-api';
import type { ValidateOverride, ValidateResult, ValidateResponse } from '../utils/operator-api';
import { TIMEOUT_VALUES } from '../utils/constants';
import {
  CLUSTERREPOS_URL, MANAGED_REPO_LABEL, NVIDIA_TEAM_REPO_LABEL,
  READY_CONDITION_TYPES, isRepoReady, repoNotReadyMessage,
} from './app-collection';
import {
  resolveRegistryEndpoints, NVIDIA_REPO_URL, NVIDIA_BLUEPRINT_REPO_URL,
} from './registry-endpoints';
import { requestErrorMessage } from './rancher-token';

export type RegistryTarget = 'applicationCollection' | 'suseRegistry' | 'nvidia';
export type RegistryConfiguration = Pick<ValidateOverride, 'url' | 'userSecretRef' | 'tokenSecretRef' | 'caBundleSecretRef'>;

const REPO_NAMES: Record<RegistryTarget, string[]> = {
  applicationCollection: ['application-collection'],
  suseRegistry:          ['suse-ai-registry'],
  nvidia:                ['nvidia', 'nvidia-blueprints'],
};
const REPO_API = CLUSTERREPOS_URL.split('?')[0];
const REPO_PAGE = '/c/local/apps/catalog.cattle.io.clusterrepo';

interface ClusterRepo {
  metadata: {
    name: string;
    labels?: Record<string, string>;
    generation?: number;
    resourceVersion?: string;
  };
  spec?: { url?: string; enabled?: boolean; forceUpdate?: string };
  status?: {
    indexConfigMapName?: string;
    observedGeneration?: number;
    conditions?: Array<{ type: string; status: string; message?: string }>;
  };
}

export interface ChartRepositoryStatus {
  name: string;
  url: string;
  link: string;
  state: 'ready' | 'pending' | 'failed' | 'missing';
  reason?: 'missing' | 'unmanaged' | 'disabled' | 'reconciling' | 'indexPending' | 'downloadFailed' | 'refreshRequested';
  message?: string;
  canRefresh: boolean;
}

export interface RepositoryCheck {
  repositories: ChartRepositoryStatus[];
  // undefined = could not read Settings; null = no saved Settings yet.
  appliedConfiguration?: RegistryConfiguration | null;
  settingsError?: string;
  error?: string;
  settingsPending?: boolean;
}

function normalizedUrl(url: string): string {
  return url.trim().replace(/\/+$/, '');
}

/** Compare endpoint and Secret references, never fetch or expose Secret values. */
export function registryConfigurationFingerprint(target: RegistryTarget, configuration: RegistryConfiguration): string {
  const url = resolveRegistryEndpoints({ [target]: configuration.url?.trim() })[target];
  return JSON.stringify([
    normalizedUrl(url),
    ...[configuration.userSecretRef, configuration.tokenSecretRef, configuration.caBundleSecretRef]
      .map(ref => [ref?.name || '', ref?.key || '']),
  ]);
}

function belongsToTarget(repo: ClusterRepo, target: RegistryTarget): boolean {
  if (Object.values(REPO_NAMES).some(names => names.includes(repo.metadata.name))) {
    return REPO_NAMES[target].includes(repo.metadata.name);
  }
  return (
    target === 'nvidia' && repo.metadata.labels?.[NVIDIA_TEAM_REPO_LABEL] === 'true' &&
    repo.metadata.labels?.[MANAGED_REPO_LABEL] === 'true'
  );
}

function expectedUrl(target: RegistryTarget, name: string, configuration?: RegistryConfiguration | null): string | undefined {
  if (!configuration) return undefined;
  if (target !== 'nvidia' || configuration.url) return configuration.url;
  if (name === 'nvidia') return NVIDIA_REPO_URL;
  if (name === 'nvidia-blueprints') return NVIDIA_BLUEPRINT_REPO_URL;
  return undefined; // Connected-mode team repositories have individual URLs.
}

function repositoryStatus(name: string, repo?: ClusterRepo, endpoint?: string): ChartRepositoryStatus {
  const result: ChartRepositoryStatus = {
    name,
    url: repo?.spec?.url || '',
    link: repo ? `${REPO_PAGE}/${encodeURIComponent(name)}` : REPO_PAGE,
    state: 'missing',
    reason: 'missing',
    canRefresh: false,
  };
  if (!repo) return result;
  if (repo.metadata.labels?.[MANAGED_REPO_LABEL] !== 'true') {
    return { ...result, state: 'failed', reason: 'unmanaged' };
  }
  if (repo.spec?.enabled === false) {
    return { ...result, state: 'failed', reason: 'disabled' };
  }
  result.canRefresh = true;
  // A saved endpoint change or refresh must not inherit the old index's green
  // status while Rancher/the Settings reconciler has not processed it yet.
  if ((endpoint && normalizedUrl(endpoint) !== normalizedUrl(result.url)) ||
      (repo.metadata.generation ?? 0) > (repo.status?.observedGeneration ?? 0)) {
    return { ...result, state: 'pending', reason: 'reconciling' };
  }
  const conditions = (repo.status?.conditions || []).filter(c => READY_CONDITION_TYPES.includes(c.type));
  if (conditions.some(c => c.status === 'False')) {
    return { ...result, state: 'failed', reason: 'downloadFailed', message: repoNotReadyMessage(repo) };
  }
  if (conditions.some(c => c.status !== 'True') || !isRepoReady(repo)) {
    return { ...result, state: 'pending', reason: 'indexPending', message: repoNotReadyMessage(repo) };
  }
  return { ...result, state: 'ready', reason: undefined };
}

async function probeForm(target: RegistryTarget, configuration: RegistryConfiguration, settings: ReturnType<typeof getSettings>): Promise<ValidateResponse> {
  const skipped = (message: string): ValidateResponse => ({ results: [{ target, status: 'skipped', message }] });
  if (![configuration.userSecretRef, configuration.tokenSecretRef].every(ref => ref?.name && ref?.key)) {
    return skipped('Select a complete username and token Secret reference to test authentication.');
  }
  // The operator treats null refs/empty URLs as "use saved values", not removal.
  // Empty credentials must not silently test saved credentials, and removing a
  // saved CA cannot be represented by this API until the user applies the form.
  if (!configuration.caBundleSecretRef) {
    try {
      const saved = await settings;
      if (saved?.spec?.[target]?.caBundleSecretRef) {
        return skipped('Apply the removal of the saved CA bundle before testing authentication.');
      }
    } catch (e) {
      if ((e as { status?: number })?.status !== 404) {
        return skipped('Could not verify saved CA settings. Restore access to Settings and test again.');
      }
    }
  }
  // In connected mode NVIDIA authentication is against nvcr.io, not its public
  // Helm repositories. Make defaults explicit so clearing a mirror tests them.
  const url = resolveRegistryEndpoints({ [target]: configuration.url })[target] || 'https://nvcr.io';
  return validateCredentials({ targets: [target], overrides: { [target]: { ...configuration, url } } });
}

/** Read-only: the form's authentication probe and Rancher's saved repositories
 * are independent checks. A failure of either must not hide the other's result. */
export async function checkRegistryConnection(store: Dispatchable, target: RegistryTarget, configuration: RegistryConfiguration): Promise<{
  authentication: ValidateResult;
  chartRepositories: RepositoryCheck;
}> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), TIMEOUT_VALUES.READ);
  const settingsRequest = getSettings(controller.signal).finally(() => clearTimeout(timer));
  const [probe, settings, repos] = await Promise.allSettled([
    probeForm(target, configuration, settingsRequest),
    settingsRequest,
    store.dispatch('rancher/request', { url: CLUSTERREPOS_URL, timeout: TIMEOUT_VALUES.READ }),
  ]);
  const authentication: ValidateResult = probe.status === 'fulfilled'
    ? (probe.value.results || []).find(r => r.target === target) || { target, status: 'error', message: 'No authentication result returned.' }
    : { target, status: 'error', message: requestErrorMessage(probe.reason) };
  const chartRepositories: RepositoryCheck = { repositories: [] };
  if (settings.status === 'fulfilled') {
    const spec = settings.value?.spec || {};
    chartRepositories.settingsPending = (settings.value?.metadata?.generation ?? 0) >
      (settings.value?.status?.observedGeneration ?? 0);
    chartRepositories.appliedConfiguration = {
      ...spec[target], url: resolveRegistryEndpoints(spec.registryEndpoints)[target],
    };
  } else if (settings.reason?.status === 404) {
    chartRepositories.appliedConfiguration = null;
  } else {
    chartRepositories.settingsError = requestErrorMessage(settings.reason);
  }
  if (repos.status === 'rejected') {
    chartRepositories.error = requestErrorMessage(repos.reason);
    return { authentication, chartRepositories };
  }
  const body = repos.value?.data ?? repos.value;
  const items: ClusterRepo[] = body?.items ?? body;
  if (!Array.isArray(items)) {
    chartRepositories.error = 'Invalid ClusterRepo list response.';
    return { authentication, chartRepositories };
  }
  const selected = items.filter(repo => repo?.metadata?.name && belongsToTarget(repo, target));
  const names = [...new Set([...REPO_NAMES[target], ...selected.map(repo => repo.metadata.name)])];
  chartRepositories.repositories = names.map(name => {
    const status = repositoryStatus(
      name, selected.find(repo => repo.metadata.name === name),
      expectedUrl(target, name, chartRepositories.appliedConfiguration),
    );
    return chartRepositories.settingsPending && status.state === 'ready'
      ? { ...status, state: 'pending', reason: 'reconciling' } : status;
  });
  return { authentication, chartRepositories };
}

/** Explicit user action, equivalent to Rancher's Refresh. Re-read ownership and
 * use resourceVersion so a concurrent replacement/edit cannot be overwritten.
 * Acceptance is NOT readiness: only a subsequent Test observes the download. */
export async function refreshChartRepository(store: Dispatchable, target: RegistryTarget, name: string): Promise<ChartRepositoryStatus> {
  const url = `${REPO_API}/${encodeURIComponent(name)}`;
  const response = await store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.READ });
  const repo: ClusterRepo = response?.data ?? response;
  if (repo?.metadata?.name !== name || !belongsToTarget(repo, target) ||
      repo.metadata.labels?.[MANAGED_REPO_LABEL] !== 'true' || repo.spec?.enabled === false ||
      !repo.metadata.resourceVersion) {
    throw new Error('Only an enabled, AI Factory-managed repository for this registry can be refreshed.');
  }
  const previous = Date.parse(repo.spec?.forceUpdate || '');
  // metav1.Time is second-precision. Make rapid repeated refreshes distinct.
  const next = Math.max(Date.now(), Number.isFinite(previous) ? previous + 1000 : 0);
  await store.dispatch('rancher/request', {
    url,
    method: 'PATCH',
    headers: { 'Content-Type': 'application/merge-patch+json' },
    data: {
      metadata: { resourceVersion: repo.metadata.resourceVersion },
      spec: { forceUpdate: new Date(next).toISOString().replace(/\.\d+Z$/, 'Z') },
    },
    timeout: TIMEOUT_VALUES.MUTATION,
  });
  return { ...repositoryStatus(name, repo), state: 'pending', reason: 'refreshRequested', message: undefined };
}
