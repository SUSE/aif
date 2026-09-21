import type { AppCollectionItem, ManagedRepo } from './app-collection';
import { APP_COLLECTION_REPO_URL, SUSE_REGISTRY_REPO_URL, NVIDIA_BLUEPRINT_REPO_URL, NGC_HOST } from './registry-endpoints';

const normalize = (url?: string) => (url || '').trim().replace(/\/+$/, '');

/** Resolve only within the managed snapshot. Stable source names preserve the
 * static catalog's identity when an administrator replaces its public endpoint. */
export function appRepository(app: AppCollectionItem, repos: ManagedRepo[]): ManagedRepo | undefined {
  if (app.repository_name) return repos.find(repo => repo.name === app.repository_name);
  const source = normalize(app.repository_url);
  if (!source) return undefined;
  const exact = repos.find(repo => normalize(repo.url) === source);
  if (exact) return exact;
  if (source === APP_COLLECTION_REPO_URL) return repos.find(repo => repo.name === 'application-collection');
  if (source === SUSE_REGISTRY_REPO_URL) return repos.find(repo => repo.name === 'suse-ai-registry');
  try {
    if (new URL(source).host === NGC_HOST && app.library === 'nvidia') {
      const name = source === NVIDIA_BLUEPRINT_REPO_URL ? 'nvidia-blueprints' : 'nvidia';
      // Only an actual OCI mirror replaces connected-mode team URLs. A healthy
      // public org repo must never stand in for a failed/missing runai repo.
      return repos.find(repo => repo.name === name && repo.url.startsWith('oci://'));
    }
  } catch { /* an invalid catalog URL has no installation source */ }
  return undefined;
}

/** Preserve discoverable catalog entries for a broken source, with installation
 * disabled by appRepository(). Healthy mirrors may intentionally contain a
 * subset: never add catalog entries just because they are absent from an index. */
export function includeUnavailableApps(discovered: AppCollectionItem[], curated: AppCollectionItem[], repos: ManagedRepo[]): AppCollectionItem[] {
  const key = (app: AppCollectionItem) => `${app.library || ''}/${app.slug_name}`;
  const present = new Set(discovered.map(key));
  const unavailable = curated.filter(app => {
    const repo = appRepository(app, repos);
    return repo && !repo.ready && !present.has(key(app));
  }).map(app => {
    const repo = appRepository(app, repos)!;
    return { ...app, repository_name: repo.name, repository_url: repo.url };
  });
  return [...discovered, ...unavailable];
}

export function repositoryFailureMessage(repoName: string, reason?: string): string {
  return `Chart repository "${repoName}" is not ready: ${reason || 'the repository index has not been downloaded yet.'} Open AI Factory Settings and run Test to check chart access and repository status.`;
}
