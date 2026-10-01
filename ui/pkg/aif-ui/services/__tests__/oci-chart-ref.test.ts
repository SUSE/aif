import { describe, expect, it } from 'vitest';
import { buildFleetBundleYAML, ociChartRef } from '../fleet-bundle';

describe('ociChartRef', () => {
  it.each([
    ['namespace-style repo appends chart segment', 'oci://registry.example.com/charts', 'app', 'oci://registry.example.com/charts/app'],
    ['single-chart repo already ending in chart name is not doubled', 'oci://registry.example.com/project/app', 'app', 'oci://registry.example.com/project/app'],
    ['last segment only partially matching chart name still appends', 'oci://registry.example.com/project/my-app', 'app', 'oci://registry.example.com/project/my-app/app'],
    ['trailing slash is normalized before appending', 'oci://registry.example.com/charts/', 'app', 'oci://registry.example.com/charts/app'],
    ['trailing slash on a single-chart repo is normalized', 'oci://registry.example.com/project/app/', 'app', 'oci://registry.example.com/project/app'],
    ['empty chart name leaves the repo URL untouched', 'oci://registry.example.com/project/app', '', 'oci://registry.example.com/project/app'],
  ])('%s', (_name, repoURL, chart, want) => {
    expect(ociChartRef(repoURL, chart)).toBe(want);
  });
});

describe('buildFleetBundleYAML OCI repo', () => {
  const base = {
    bundleName:       'app-default',
    release:          'app',
    chartVersion:     '1.0.0',
    helmSecretName:   null,
    values:           {},
    pullSecretNames:  [],
    targetClusterIds: ['local'],
    targetNamespace:  'default',
  };

  it('does not double the chart segment for a single-chart repo', () => {
    const doc = JSON.parse(buildFleetBundleYAML({
      ...base, chartName: 'app', chartRepoUrl: 'oci://registry.example.com/project/app',
    }));

    expect(doc.spec.helm.repo).toBe('oci://registry.example.com/project/app');
    expect(doc.spec.helm.chart).toBeUndefined();
  });

  it('appends the chart segment for a namespace-style repo', () => {
    const doc = JSON.parse(buildFleetBundleYAML({
      ...base, chartName: 'app', chartRepoUrl: 'oci://registry.example.com/charts',
    }));

    expect(doc.spec.helm.repo).toBe('oci://registry.example.com/charts/app');
  });
});
