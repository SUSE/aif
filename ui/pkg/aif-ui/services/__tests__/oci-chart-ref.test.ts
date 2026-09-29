import { describe, it, expect } from 'vitest';

import { ociChartRef } from '../fleet-bundle';

describe('ociChartRef', () => {
  it('appends the chart segment for a namespace-style repo', () => {
    expect(ociChartRef('oci://dp.apps.rancher.io/charts', 'milvus'))
      .toBe('oci://dp.apps.rancher.io/charts/milvus');
  });

  it('does not double a single-chart repo that already ends in the chart name', () => {
    expect(ociChartRef('oci://ghcr.io/nvidia/openshell/helm-chart', 'helm-chart'))
      .toBe('oci://ghcr.io/nvidia/openshell/helm-chart');
  });

  it('does not double a single-chart workspace repo', () => {
    expect(ociChartRef('oci://ghcr.io/nvidia/openshell/openshell-workspace', 'openshell-workspace'))
      .toBe('oci://ghcr.io/nvidia/openshell/openshell-workspace');
  });

  it('normalizes a trailing slash before appending', () => {
    expect(ociChartRef('oci://dp.apps.rancher.io/charts/', 'milvus'))
      .toBe('oci://dp.apps.rancher.io/charts/milvus');
  });

  it('normalizes multiple trailing slashes before appending', () => {
    expect(ociChartRef('oci://dp.apps.rancher.io/charts//', 'milvus'))
      .toBe('oci://dp.apps.rancher.io/charts/milvus');
  });

  it('leaves the repo URL untouched when the chart name is empty', () => {
    expect(ociChartRef('oci://ghcr.io/nvidia/openshell/helm-chart', ''))
      .toBe('oci://ghcr.io/nvidia/openshell/helm-chart');
  });
});
