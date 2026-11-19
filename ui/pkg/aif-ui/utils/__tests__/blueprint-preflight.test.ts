import { describe, expect, it } from 'vitest';
import { existingReleasesByComponent, missingCredentialsForBlueprint, requiredCredentialForRepo } from '../blueprint-preflight';
import type { BlueprintComponent } from '../../types/blueprint-types';
import type { RegistryStatus } from '../../types/aiworkload-types';
import type { AppCRD } from '../../types/rancher-types';

const component = (chartName: string, chartRepo = 'r'): BlueprintComponent => ({ chartRepo, chartName, chartVersion: '1.0.0' });

const app = (chart: string, version: string, namespace: string, release = chart): AppCRD => ({
  metadata: { name: release, namespace },
  spec:     { chart: { metadata: { name: chart, version } } },
});

describe('requiredCredentialForRepo', () => {
  it('gates NVIDIA Run:ai repo on the NVIDIA credential', () => {
    expect(requiredCredentialForRepo('nvidia-runai')).toBe('nvidia');
  });

  it('returns null for repos without a managed credential', () => {
    expect(requiredCredentialForRepo('custom-repo')).toBeNull();
  });
});

describe('missingCredentialsForBlueprint', () => {
  const status: RegistryStatus = { applicationCollection: true, suseRegistry: false, nvidia: false };

  it('returns only the required credentials the operator reports as unconfigured', () => {
    expect(missingCredentialsForBlueprint([component('a', 'application-collection'), component('b', 'nvidia')], status)).toEqual(['nvidia']);
  });

  it('returns nothing when the registry status is unknown', () => {
    expect(missingCredentialsForBlueprint([component('a', 'application-collection'), component('b', 'nvidia')], null)).toEqual([]);
  });

  it('skips components whose chartRepo needs no managed credential', () => {
    expect(missingCredentialsForBlueprint([component('a', 'my-custom-repo')], status)).toEqual([]);
  });

  it('reports the NVIDIA credential for a gated Run:ai component', () => {
    expect(missingCredentialsForBlueprint([component('runai-backend', 'nvidia-runai')], status)).toEqual(['nvidia']);
    expect(missingCredentialsForBlueprint([component('runai-backend', 'nvidia-runai')], { ...status, nvidia: true })).toEqual([]);
  });
});

describe('existingReleasesByComponent', () => {
  it('matches apps by chart name and carries release, version and namespace', () => {
    const got = existingReleasesByComponent(
      [component('cert-manager'), component('gpu-operator')],
      { 'c-1': [app('cert-manager', 'v1.15.0', 'security', 'certs'), app('other', '1.0.0', 'x')] },
    );

    expect(got).toEqual({
      'cert-manager': [{
        clusterId: 'c-1', release: 'certs', chart: 'cert-manager', version: 'v1.15.0', namespace: 'security',
      }],
    });
  });

  it('returns nothing when no chart matches', () => {
    expect(existingReleasesByComponent([component('gpu-operator')], { 'c-1': [app('cert-manager', '1', 'ns')] })).toEqual({});
  });

  it('lists every target cluster that has the chart', () => {
    const got = existingReleasesByComponent([component('cert-manager')], {
      'c-1': [app('cert-manager', 'v1.14.0', 'cert-manager')],
      'c-2': [],
      'c-3': [app('cert-manager', 'v1.15.0', 'security')],
    });

    expect(got['cert-manager'].map((r) => [r.clusterId, r.version, r.namespace])).toEqual([
      ['c-1', 'v1.14.0', 'cert-manager'],
      ['c-3', 'v1.15.0', 'security'],
    ]);
  });

  it('handles empty inputs and apps without chart metadata', () => {
    expect(existingReleasesByComponent([], { 'c-1': [app('cert-manager', '1', 'ns')] })).toEqual({});
    expect(existingReleasesByComponent([component('cert-manager')], {})).toEqual({});
    expect(existingReleasesByComponent([component('cert-manager')], { 'c-1': [{ metadata: { name: 'a', namespace: 'b' }, spec: {} }] })).toEqual({});
  });
});
