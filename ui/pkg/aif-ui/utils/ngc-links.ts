import { NGC_HOST } from '../services/registry-endpoints';

/**
 * Build the public NGC catalog page URL for a Helm chart, from the chart's
 * live NGC repository URL (https://helm.ngc.nvidia.com/{org}[/{team}]).
 *
 * Returns undefined for any other host or an unparseable/missing URL, so
 * callers never link out to a page that doesn't exist (e.g. a private mirror
 * in air-gap, where the chart has no public NGC catalog page at all).
 */
export function ngcChartPageUrl(repositoryUrl: string | undefined, slug: string): string | undefined {
  if (!repositoryUrl) return undefined;

  let url: URL;
  try {
    url = new URL(repositoryUrl);
  } catch {
    return undefined;
  }

  if (url.hostname !== NGC_HOST) return undefined;

  const [org, team] = url.pathname.split('/').filter(Boolean);
  if (!org) return undefined;

  return `https://catalog.ngc.nvidia.com/orgs/${ org }/${ team || '-' }/helm-charts/${ slug }/-`;
}
