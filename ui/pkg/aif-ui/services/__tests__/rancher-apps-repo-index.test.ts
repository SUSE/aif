import { describe, expect, it, vi } from 'vitest';
import { findChartInRepo, listChartVersions } from '../rancher-apps';
import type { Dispatchable } from '../../types/rancher-types';

const REPO_NAME = 'suse-ai-registry';
const REPO_URL = `/v1/catalog.cattle.io.clusterrepos/${REPO_NAME}`;
const INDEX_URL = `${REPO_URL}?link=index`;
const INDEX = {
  entries: {
    qdrant: [{ version: '1.0.0' }, { version: '1.2.0' }],
  },
};

interface RepoStatus {
  indexConfigMapName?: string;
  conditions?: Array<{ type: string; status: string; message?: string }>;
}

function makeStore(status?: RepoStatus, options: {
  wrapped?: boolean;
  indexPayload?: unknown;
  indexError?: unknown;
  metadataError?: Error;
} = {}) {
  let metadataReads = 0;
  const repo = {
    metadata: { name: REPO_NAME },
    status,
    links: { index: INDEX_URL },
  };

  return {
    dispatch: vi.fn(async (action: string, { url }: { url?: string } = {}) => {
      if (action === 'management/findAll') return [{ id: 'local' }];
      if (action === 'rancher/request' && url === REPO_URL) {
        metadataReads++;
        // getClusterContext discovers the repo before the index loader reads it.
        if (metadataReads > 1 && options.metadataError) throw options.metadataError;
        return options.wrapped ? { data: repo } : repo;
      }
      if (action === 'rancher/request' && url === INDEX_URL) {
        if (options.indexError) throw options.indexError;
        return options.indexPayload ?? INDEX;
      }
      throw new Error(`Unexpected request: ${action} ${url}`);
    }),
  };
}

function expectNoIndexRequest(store: ReturnType<typeof makeStore>) {
  expect(store.dispatch).not.toHaveBeenCalledWith('rancher/request', expect.objectContaining({ url: INDEX_URL }));
}

describe('install wizard chart lookup', () => {
  it.each(['kubeflow', 'litellm', 'qdrant'])('reports a pending index for %s before requesting it', async (chart) => {
    const store = makeStore({ conditions: [{ type: 'OCIDownloaded', status: 'True' }] }, {
      indexError: { message: 'configmaps "" not found' },
    });

    await expect(findChartInRepo(store, 'local', REPO_NAME, chart)).rejects.toThrow(
      `Chart repository "${REPO_NAME}" is not ready: the repository index has not been downloaded yet.`,
    );
    expectNoIndexRequest(store);
  });

  it('can load the chart on a later attempt once the index is ready', async () => {
    const status: RepoStatus = {};
    const store = makeStore(status);

    await expect(findChartInRepo(store, 'local', REPO_NAME, 'qdrant')).rejects.toThrow('is not ready');
    expectNoIndexRequest(store);

    status.indexConfigMapName = 'repo-index';

    await expect(findChartInRepo(store, 'local', REPO_NAME, 'qdrant')).resolves.toEqual({
      chartName: 'qdrant', version: '1.2.0',
    });
  });
});

const lookups: Array<[string, (store: Dispatchable) => Promise<unknown>]> = [
  ['chart lookup', (store) => findChartInRepo(store, 'local', REPO_NAME, 'qdrant')],
  ['version lookup', (store) => listChartVersions(store, 'local', REPO_NAME, 'qdrant')],
];

describe.each(lookups)('%s repository readiness', (_name, lookup) => {
  it.each(['OCIDownloaded', 'Downloaded', 'FollowerDownloaded'])(
    'surfaces the %s failure even when an old index exists', async (type) => {
      const store = makeStore({
        indexConfigMapName: 'old-index',
        conditions: [{ type, status: 'False', message: '401 Unauthorized' }],
      }, { wrapped: true });

      await expect(lookup(store)).rejects.toThrow(
        `Chart repository "${REPO_NAME}" is not ready: 401 Unauthorized`,
      );
      expectNoIndexRequest(store);
    },
  );

  it('explains a repository whose status has not been populated', async () => {
    const store = makeStore();

    await expect(lookup(store)).rejects.toThrow('the repository index has not been downloaded yet');
    expectNoIndexRequest(store);
  });

  it('preserves metadata request errors', async () => {
    const error = new Error('Forbidden: cannot read repository');
    const store = makeStore({ indexConfigMapName: 'repo-index' }, { metadataError: error });

    await expect(lookup(store)).rejects.toBe(error);
    expectNoIndexRequest(store);
  });
});

describe('ready repository indexes', () => {
  it.each([
    { name: 'direct', wrapped: false, indexPayload: INDEX },
    { name: 'wrapped', wrapped: true, indexPayload: { data: INDEX } },
    { name: 'YAML', wrapped: true, indexPayload: 'entries:\n  qdrant:\n    - version: 1.0.0\n    - version: 1.2.0\n' },
  ])('loads a $name response, including indexes without download conditions', async ({ wrapped, indexPayload }) => {
    const store = makeStore({ indexConfigMapName: 'repo-index' }, { wrapped, indexPayload });

    await expect(findChartInRepo(store, 'local', REPO_NAME, 'qdrant')).resolves.toEqual({
      chartName: 'qdrant', version: '1.2.0',
    });
    await expect(listChartVersions(store, 'local', REPO_NAME, 'qdrant')).resolves.toEqual(['1.2.0', '1.0.0']);
  });
});
