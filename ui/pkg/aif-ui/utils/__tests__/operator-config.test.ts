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

  it('creates the ConfigMap with no ownership metadata when it does not exist yet', async () => {
    const calls = stubFetch(() => ({ ok: false, status: 404, json: async () => null }));

    await saveOperatorConfig('aif-operator', 'aif-operator');

    const post = calls.find((c) => c.method === 'POST');
    expect(post).toBeDefined();
    expect(post?.body?.metadata).toEqual({ name: 'aif-ui-config', namespace: 'cattle-ui-plugin-system' });
    expect(post?.body?.data).toEqual({ operatorNamespace: 'aif-operator', operatorService: 'aif-operator' });
  });

  // SUSEAI-1039 regression: an earlier version of this fix hand-stamped Helm
  // ownership metadata on create, but the update path below did a PUT that
  // omitted labels/annotations entirely — and PUT is a full-object replace, so
  // the very next save silently stripped whatever ownership metadata a Helm
  // install had since applied. The mock here must include labels/annotations
  // on the "existing" object, or this test can't catch that regression.
  it('carries forward existing labels and annotations on update instead of dropping them', async () => {
    const calls = stubFetch(() => ({
      ok:     true,
      status: 200,
      json:   async () => ({
        metadata: {
          resourceVersion: '7',
          labels:          { 'app.kubernetes.io/managed-by': 'Helm' },
          annotations:     {
            'meta.helm.sh/release-name':      'aif-ui-server',
            'meta.helm.sh/release-namespace': 'cattle-ui-plugin-system',
          },
        },
        data: { existingKey: 'keep-me' },
      }),
    }));

    await saveOperatorConfig('aif-operator', 'aif-operator');

    const put = calls.find((c) => c.method === 'PUT');
    expect(put).toBeDefined();
    expect(put?.body?.metadata).toEqual({
      name:      'aif-ui-config',
      namespace: 'cattle-ui-plugin-system',
      labels:    { 'app.kubernetes.io/managed-by': 'Helm' },
      annotations: {
        'meta.helm.sh/release-name':      'aif-ui-server',
        'meta.helm.sh/release-namespace': 'cattle-ui-plugin-system',
      },
      resourceVersion: '7',
    });
    expect(put?.body?.data).toEqual({
      existingKey:       'keep-me',
      operatorNamespace: 'aif-operator',
      operatorService:   'aif-operator',
    });
  });

  it('update on an object with no existing labels/annotations does not invent any', async () => {
    const calls = stubFetch(() => ({
      ok:     true,
      status: 200,
      json:   async () => ({ metadata: { resourceVersion: '7' }, data: { existingKey: 'keep-me' } }),
    }));

    await saveOperatorConfig('aif-operator', 'aif-operator');

    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.body?.metadata).toEqual({ name: 'aif-ui-config', namespace: 'cattle-ui-plugin-system', resourceVersion: '7' });
  });
});
