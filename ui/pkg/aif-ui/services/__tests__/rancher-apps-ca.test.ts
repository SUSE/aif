import { describe, expect, it, vi } from 'vitest';
import { ensureClusterRepo } from '../rancher-apps';
import type { Dispatchable } from '../../types/rancher-types';

interface RequestPayload {
  url?: string;
  method?: string;
  data?: {
    type?: string;
    stringData?: Record<string, string>;
  };
}

interface RecordedCall {
  action: string;
  payload: RequestPayload;
}

interface TestStore extends Dispatchable {
  calls: RecordedCall[];
}

function storeForExistingRepo(): TestStore {
  const calls: RecordedCall[] = [];
  const responses: unknown[] = [
    {
      data: {
        items: [{
          metadata: { name: 'suse-ai-registry' },
          spec: {
            url:          'oci://registry.example.test/ai/charts',
            clientSecret: { name: 'suse-ai-registry-auth', namespace: 'cattle-system' },
          },
        }],
      },
    },
    { metadata: { name: 'suse-ai-registry-auth', resourceVersion: '7' } },
    {},
  ];
  return {
    calls,
    dispatch: vi.fn(async (action: string, payload?: unknown) => {
      calls.push({ action, payload: (payload || {}) as RequestPayload });
      return responses.shift();
    }),
  };
}

describe('ensureClusterRepo private CA', () => {
  it('writes Fleet-compatible cacerts beside basic auth data', async () => {
    const store = storeForExistingRepo();
    const ca = '-----BEGIN CERTIFICATE-----\nTEST\n-----END CERTIFICATE-----\n';

    await ensureClusterRepo(store, 'oci://registry.example.test/ai/charts', {
      username: 'robot$aif',
      password: 'secret',
      cacerts:  ca,
    });

    const secretPut = store.calls.find((call) =>
      call.payload?.method === 'PUT' && call.payload?.url?.includes('/secrets/suse-ai-registry-auth')
    );
    expect(secretPut).toBeDefined();
    expect(secretPut.payload.data.type).toBe('kubernetes.io/basic-auth');
    expect(secretPut.payload.data.stringData).toEqual({
      username: 'robot$aif',
      password: 'secret',
      cacerts:  ca,
    });
  });
});
