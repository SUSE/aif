import { describe, expect, it, vi } from 'vitest';
import { ensureClusterRepo } from '../rancher-apps';
import type { Dispatchable } from '../../types/rancher-types';

interface RequestPayload {
  url?: string;
  method?: string;
  data?: {
    spec?: {
      url?: string;
    };
  };
}

interface RecordedCall {
  action: string;
  payload: RequestPayload;
}

interface TestStore extends Dispatchable {
  calls: RecordedCall[];
}

describe('ensureClusterRepo stable aliases', () => {
  it('selects the preferred logical name when multiple repos share a mirrored URL', async () => {
    const calls: RecordedCall[] = [];
    const responses: unknown[] = [
      {
        data: {
          items: [
            {
              metadata: { name: 'registry-internal-nvidia' },
              spec:     { url: 'oci://registry.internal/nvidia' },
            },
            {
              metadata: { name: 'nvidia-blueprints' },
              spec: {
                url:          'https://helm.ngc.nvidia.com/nvidia/blueprint',
                clientSecret: { name: 'nvidia-blueprints-auth', namespace: 'cattle-system' },
              },
            },
          ],
        },
      },
      { metadata: { name: 'nvidia-blueprints-auth', resourceVersion: '3' } },
      {},
      {
        metadata: { name: 'nvidia-blueprints', resourceVersion: '4' },
        spec:     { url: 'https://helm.ngc.nvidia.com/nvidia/blueprint' },
      },
      {},
    ];
    const store: TestStore = {
      calls,
      dispatch: vi.fn(async (action: string, payload?: unknown) => {
        calls.push({ action, payload: (payload || {}) as RequestPayload });
        return responses.shift();
      }),
    };

    const name = await ensureClusterRepo(
      store,
      'oci://registry.internal/nvidia',
      { username: 'robot', password: 'secret' },
      'nvidia-blueprints',
    );

    expect(name).toBe('nvidia-blueprints');
    const secretPut = calls.find((call) => call.payload.method === 'PUT');
    expect(secretPut?.payload.url).toContain('/secrets/nvidia-blueprints-auth');
    const repoPut = calls.find((call) =>
      call.payload.method === 'PUT' && call.payload.url?.includes('/clusterrepos/nvidia-blueprints')
    );
    expect(repoPut?.payload.data?.spec?.url).toBe('oci://registry.internal/nvidia');
  });
});
