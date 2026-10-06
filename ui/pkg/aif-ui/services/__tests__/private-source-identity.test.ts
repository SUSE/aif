import { describe, expect, it, vi } from 'vitest';

const { ensureRegistrySecretSimple } = vi.hoisted(() => ({
  ensureRegistrySecretSimple: vi.fn().mockResolvedValue('private-registry-pull'),
}));

vi.mock('../rancher-apps', () => ({ ensureRegistrySecretSimple }));

import { getLibraryForClusterRepo } from '../app-collection';
import { ensureAppCollectionPullSecrets, registryHostFromRepoURL } from '../fleet-bundle';

describe('private source identity', () => {
  it('classifies well-known sources independently of their configured endpoint', () => {
    expect(getLibraryForClusterRepo(
      'suse-ai-registry',
      'oci://harbor.airgap.test/mirrors/suse-ai',
    )).toBe('suse-ai');
    expect(getLibraryForClusterRepo(
      'nvidia-blueprints',
      'oci://harbor.airgap.test/mirrors/nvidia',
    )).toBe('nvidia');
  });

  it('treats custom-labelled repos as custom even when they reuse a built-in URL', () => {
    const custom = { 'ai-factory.suse.com/custom-repo': 'true' };

    expect(getLibraryForClusterRepo('my-appco', 'oci://dp.apps.rancher.io/charts', custom)).toBeUndefined();
    expect(getLibraryForClusterRepo('my-suse-registry', 'oci://registry.suse.com/ai/charts', custom)).toBeUndefined();
    // Without the label, URL discovery still applies.
    expect(getLibraryForClusterRepo('connected', 'oci://dp.apps.rancher.io/charts')).toBe('suse-ai');
  });

  it('extracts a registry host, including a private port, from OCI URLs', () => {
    expect(registryHostFromRepoURL('oci://harbor.airgap.test:5443/charts')).toBe('harbor.airgap.test:5443');
    expect(registryHostFromRepoURL('not a URL')).toBe('');
  });

  it('scopes App Collection pull credentials to the configured mirror host', async() => {
    const dispatch = vi.fn(async(action: string, payload: any) => {
      if (action === 'management/find') {
        return {
          spec: {
            url:          'oci://harbor.airgap.test:5443/charts/appco',
            clientSecret: { name: 'appco-auth', namespace: 'cattle-system' },
          },
        };
      }
      if (action === 'rancher/request' && payload.url.includes('/secrets/appco-auth')) {
        return {
          kind: 'Secret',
          data: {
            username: btoa('robot'),
            password: btoa('secret'),
          },
        };
      }
      throw new Error(`unexpected dispatch: ${ action } ${ payload?.url || '' }`);
    });
    const names: string[] = [];

    await ensureAppCollectionPullSecrets(
      { dispatch },
      'workload-system',
      ['local'],
      names,
    );

    expect(ensureRegistrySecretSimple).toHaveBeenCalledWith(
      { dispatch },
      'local',
      'workload-system',
      'harbor.airgap.test:5443',
      'harbor-airgap-test-5443',
      'robot',
      'secret',
    );
    expect(names).toEqual(['private-registry-pull']);
  });
});
