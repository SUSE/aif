import {
  describe, it, expect, vi, beforeEach,
} from 'vitest';
import { validateChartAccess } from '../operator-api';
import { operatorFetch } from '../operator-config';

vi.mock('../operator-config', () => ({ operatorFetch: vi.fn() }));

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
