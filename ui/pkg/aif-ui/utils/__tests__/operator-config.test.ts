import { afterEach, describe, expect, it, vi } from 'vitest';
import { saveOperatorConfig, invalidateOperatorConfig } from '../operator-config';

interface RecordedCall {
  method: string;
  body?: any;
}

function stubFetch(getResponse: () => { ok: boolean; status: number; json: () => Promise<any> }) {
  const calls: RecordedCall[] = [];

  vi.stubGlobal('fetch', vi.fn(async (_url: string, opts: RequestInit = {}) => {
    const method = opts.method || 'GET';

    calls.push({ method, body: opts.body ? JSON.parse(opts.body as string) : undefined });

    if (method === 'GET') return getResponse();
    return { ok: true, status: method === 'POST' ? 201 : 200, json: async () => ({}) };
  }));

  return calls;
}

describe('saveOperatorConfig', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    invalidateOperatorConfig();
  });

  // SUSEAI-1039: a ConfigMap this function creates without Helm's ownership
  // stamps permanently blocks a later `helm install`/`upgrade` of the
  // aif-ui-server release with "invalid ownership metadata".
  it('stamps Helm ownership metadata when creating the ConfigMap for the first time', async () => {
    const calls = stubFetch(() => ({ ok: false, status: 404, json: async () => null }));

    await saveOperatorConfig('aif-operator', 'aif-operator');

    const post = calls.find((c) => c.method === 'POST');
    expect(post).toBeDefined();
    expect(post?.body?.metadata).toMatchObject({
      name:      'aif-ui-config',
      namespace: 'cattle-ui-plugin-system',
      labels:    { 'app.kubernetes.io/managed-by': 'Helm' },
      annotations: {
        'meta.helm.sh/release-name':      'aif-ui-server',
        'meta.helm.sh/release-namespace': 'cattle-ui-plugin-system',
      },
    });
    expect(post?.body?.data).toEqual({ operatorNamespace: 'aif-operator', operatorService: 'aif-operator' });
  });

  it('does not touch ownership metadata when updating an existing ConfigMap', async () => {
    const calls = stubFetch(() => ({
      ok:     true,
      status: 200,
      json:   async () => ({ metadata: { resourceVersion: '7' }, data: { existingKey: 'keep-me' } }),
    }));

    await saveOperatorConfig('aif-operator', 'aif-operator');

    const put = calls.find((c) => c.method === 'PUT');
    expect(put).toBeDefined();
    expect(put?.body?.metadata).toEqual({ name: 'aif-ui-config', namespace: 'cattle-ui-plugin-system', resourceVersion: '7' });
    expect(put?.body?.data).toEqual({
      existingKey:      'keep-me',
      operatorNamespace: 'aif-operator',
      operatorService:   'aif-operator',
    });
  });
});
