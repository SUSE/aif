import { describe, it, expect } from 'vitest';
import { ngcChartPageUrl } from '../ngc-links';

describe('ngcChartPageUrl', () => {
  it('builds an org-only catalog URL from an org-level helm.ngc.nvidia.com repository', () => {
    expect(ngcChartPageUrl('https://helm.ngc.nvidia.com/nvidia', 'k8s-nim-operator'))
      .toBe('https://catalog.ngc.nvidia.com/orgs/nvidia/-/helm-charts/k8s-nim-operator/-');
  });

  it('builds an org/team catalog URL from a team-level helm.ngc.nvidia.com repository', () => {
    expect(ngcChartPageUrl('https://helm.ngc.nvidia.com/nim/mit/', 'boltz2-nim'))
      .toBe('https://catalog.ngc.nvidia.com/orgs/nim/mit/helm-charts/boltz2-nim/-');
  });

  it('returns undefined for a non-NGC repository host', () => {
    expect(ngcChartPageUrl('https://charts.example.com', 'x')).toBeUndefined();
  });

  it('returns undefined when no repository URL is given', () => {
    expect(ngcChartPageUrl(undefined, 'x')).toBeUndefined();
  });
});
