import { beforeEach, describe, expect, it, vi } from 'vitest';
import { buildFleetBundleYAML, createFleetBundle } from '../fleet-bundle';
import { ensureRegistrySecretSimple } from '../rancher-apps';

vi.mock('../rancher-apps', () => ({
  ensureRegistrySecretSimple: vi.fn(async (_store, _cluster, _ns, _host, slug: string) => `suse-ai-pull-secret-${ slug }`),
}));

const COMBINED = 'suse-ai-pull-combined';
const BROWSER_CREATED = 'suse-ai-pull-secret-registry-suse-com';
const STALE_LEGACY = 'suse-ai-pull-secret-dp-apps-rancher-io';

// fakeStore answers the ClusterRepo and auth-secret reads createFleetBundle
// makes, and records the HelmOp bodies it writes.
function fakeStore() {
  const helmOps: Record<string, any>[] = [];
  const dispatch = vi.fn(async (action: string, req: any) => {
    if (action === 'management/find') {
      if (req.id === 'suse-registry') {
        return { spec: { url: 'oci://registry.suse.com/ai/charts', clientSecret: { name: 'repo-auth', namespace: 'cattle-system' } } };
      }
      throw new Error('not found');
    }
    if (req.url.includes('/secrets/repo-auth')) {
      return { kind: 'Secret', data: { username: btoa('user'), password: btoa('pass') } };
    }
    if (req.url.includes('/helmops')) {
      if (req.method === 'PUT' || req.method === 'POST') helmOps.push(req.data);
      return { metadata: { resourceVersion: '1' } };
    }
    return {};
  });
  return { store: { dispatch }, helmOps };
}

describe('Fleet pull secret values', () => {
  beforeEach(() => vi.mocked(ensureRegistrySecretSimple).mockClear());

  it.each(['network-operator', 'gpu-operator', 'k8s-nim-operator'])('uses %s value shapes in both Fleet and GitOps output', async (chartName) => {
    const { store, helmOps } = fakeStore();
    const params = {
      bundleName: 'operator-test', release: 'operator-test', chartRepo: 'nvidia', chartName,
      chartVersion: '1.0.0', chartRepoUrl: 'https://helm.ngc.nvidia.com/nvidia',
      values: {}, targetClusterIds: ['local'], targetNamespace: 'ns', library: 'nvidia' as const,
    };
    await createFleetBundle(store, params);
    const gitOps = JSON.parse(buildFleetBundleYAML({ ...params, helmSecretName: null, pullSecretNames: [] }));

    expect(helmOps).toHaveLength(1);
    for (const output of [helmOps[0], gitOps]) {
      const values = output.spec.helm.values;
      expect(values.imagePullSecrets).toEqual([{ name: 'ngc-secret' }]);
      if (chartName === 'k8s-nim-operator') {
        expect(values.operator?.image?.pullSecrets).toEqual(['ngc-secret']);
      } else {
        expect(values.operator?.image).toBeUndefined();
      }
      if (chartName === 'network-operator') {
        expect(values.operator?.imagePullSecrets).toEqual(['ngc-secret']);
      }
    }
    expect(params.values).toEqual({});
  });

  it('createFleetBundle keeps user and browser-created pull secrets and drops stale legacy ones', async () => {
    const { store, helmOps } = fakeStore();
    await createFleetBundle(store, {
      bundleName:                'suse-ai-app-ns',
      release:                   'app',
      chartRepo:                 'suse-registry',
      chartName:                 'app',
      chartVersion:              '1.0.0',
      chartRepoUrl:              'oci://registry.suse.com/ai/charts',
      values:                    { imagePullSecrets: [{ name: 'my-mirror' }, { name: STALE_LEGACY }], global: { storageClass: 'fast' } },
      additionalPullSecretNames: [COMBINED],
      targetClusterIds:          ['local'],
      targetNamespace:           'ns',
      library:                   'suse-ai',
    } as any);

    expect(ensureRegistrySecretSimple).toHaveBeenCalled();
    expect(helmOps).toHaveLength(1);
    const values = helmOps[0].spec.helm.values;
    expect(values.imagePullSecrets).toEqual([{ name: 'my-mirror' }, { name: COMBINED }, { name: BROWSER_CREATED }]);
    expect(values.global).toEqual({ storageClass: 'fast', imagePullSecrets: [{ name: COMBINED }, { name: BROWSER_CREATED }] });
  });

  it('buildFleetBundleYAML keeps user and requested pull secrets and drops stale legacy ones', () => {
    const doc = JSON.parse(buildFleetBundleYAML({
      bundleName:       'app-ns',
      release:          'app',
      chartName:        'app',
      chartVersion:     '1.0.0',
      chartRepoUrl:     'oci://registry.suse.com/ai/charts',
      helmSecretName:   null,
      values:           { global: { imagePullSecrets: ['my-mirror', STALE_LEGACY] } },
      pullSecretNames:  [BROWSER_CREATED],
      targetClusterIds: ['local'],
      targetNamespace:  'ns',
      library:          'suse-ai',
    }));

    const values = doc.spec.helm.values;
    expect(values.global.imagePullSecrets).toEqual(['my-mirror', { name: COMBINED }, { name: BROWSER_CREATED }]);
    expect(values.imagePullSecrets).toEqual([{ name: COMBINED }, { name: BROWSER_CREATED }]);
  });
});
