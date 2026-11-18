import {
  describe, it, expect, vi, beforeEach,
} from 'vitest';
import {
  getCatalog, getRegistryStatus, getSettings, getVersion, validateChartAccess, validateCredentials,
} from '../operator-api';
import { operatorFetch, toOperatorError } from '../operator-config';

vi.mock('../operator-config', async(importOriginal) => ({
  ...(await importOriginal<typeof import('../operator-config')>()),
  operatorFetch: vi.fn(),
}));

describe('operator-api timeouts', () => {
  beforeEach(() => {
    vi.mocked(operatorFetch).mockReset().mockResolvedValue({});
  });

  function lastOptions(): any {
    const calls = vi.mocked(operatorFetch).mock.calls;

    return calls[calls.length - 1][1];
  }

  it.each([
    ['getRegistryStatus default', () => getRegistryStatus(), 30000],
    ['getRegistryStatus custom', () => getRegistryStatus(5000), 5000],
    ['getVersion default', () => getVersion(), 5000],
    ['getVersion custom', () => getVersion(1000), 1000],
    ['getCatalog default', () => getCatalog(), 25000],
    ['getSettings custom', () => getSettings(8000), 8000],
    ['validateCredentials default', () => validateCredentials({}), 20000],
    ['validateCredentials custom', () => validateCredentials({}, 3000), 3000],
    ['validateChartAccess', () => validateChartAccess({ target: 't', configuration: { url: 'oci://x' } }), 25000],
  ])('%s passes its timeout to operatorFetch and no abort signal', async(_name, call, timeout) => {
    await call();

    expect(lastOptions().timeout).toBe(timeout);
    expect(lastOptions()).not.toHaveProperty('signal');
  });

  it('getSettings() without a timeout sets none', async() => {
    await getSettings();

    expect(lastOptions()?.timeout).toBeUndefined();
  });

  it('surfaces an expired timeout as the TIMEOUT unreachable OperatorError', async() => {
    // What operatorFetch throws when axios gives up on the request.
    vi.mocked(operatorFetch).mockRejectedValue(
      toOperatorError(Object.assign(new Error('timeout of 5000ms exceeded'), { code: 'ECONNABORTED' })),
    );

    await expect(getVersion()).rejects.toMatchObject({ status: 0, code: 'TIMEOUT', unreachable: true });
  });
});

describe('validateChartAccess', () => {
  beforeEach(() => {
    vi.mocked(operatorFetch).mockReset().mockResolvedValue({ results: [] });
  });

  function postedBody(): any {
    const [, options] = vi.mocked(operatorFetch).mock.calls[0];

    return JSON.parse(options!.body as string);
  }

  it('posts only the fields the backend accepts, dropping custom-repo form extras', async() => {
    // The custom-repo form configuration carries fields (type, gitRepo, branch,
    // credSecretRef, insecureSkipVerify) that the validate-chart-access endpoint
    // rejects via DisallowUnknownFields. They must never reach the wire.
    await validateChartAccess({
      target:        'customRepo',
      chartName:     'grafana',
      configuration: {
        url:                'https://grafana.github.io/helm-charts',
        userSecretRef:      null,
        tokenSecretRef:     null,
        caBundleSecretRef:  null,
        // excess properties present on the real form object at runtime
        type:               'helm',
        gitRepo:            '',
        branch:             '',
        credSecretRef:      null,
        insecureSkipVerify: false,
      } as any,
    });

    const body = postedBody();

    expect(Object.keys(body.configuration).sort()).toEqual(
      ['caBundleSecretRef', 'tokenSecretRef', 'url', 'userSecretRef'].sort(),
    );
    expect(body.configuration.url).toBe('https://grafana.github.io/helm-charts');
    expect(body.target).toBe('customRepo');
    expect(body.chartName).toBe('grafana');
  });

  it('forwards the custom repo name so the operator can match the saved repo', async() => {
    await validateChartAccess({
      target:        'customRepo',
      chartName:     'grafana',
      configuration: { url: 'https://charts.internal', name: 'mirror' },
    });

    expect(postedBody().configuration.name).toBe('mirror');
  });

  it('preserves secret references that the backend does accept', async() => {
    await validateChartAccess({
      target:        'customRepo',
      chartName:     'redis',
      configuration: {
        url:               'oci://registry-1.docker.io/alinewerner',
        userSecretRef:     { name: 'creds', key: 'username' },
        tokenSecretRef:    { name: 'creds', key: 'password' },
        caBundleSecretRef: null,
      },
    });

    const body = postedBody();

    expect(body.configuration.userSecretRef).toEqual({ name: 'creds', key: 'username' });
    expect(body.configuration.tokenSecretRef).toEqual({ name: 'creds', key: 'password' });
  });
});
