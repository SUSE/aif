import { describe, it, expect } from 'vitest';
import { namespaceLink, appLink, workloadTargetClusters, workloadRancherLinks } from '../rancher-links';
import type { AIWorkload } from '../../types/aiworkload-types';
import type { ClusterInfo } from '../../types/rancher-types';

describe('rancher URL builders', () => {
  it('builds a namespace detail path', () => {
    expect(namespaceLink('c-m-abc', 'suseai')).toBe('/c/c-m-abc/explorer/namespace/suseai');
  });

  it('builds an apps (helm release) detail path', () => {
    expect(appLink('local', 'suseai', 'llama-1'))
      .toBe('/c/local/apps/catalog.cattle.io.app/suseai/llama-1');
  });

  it('encodes path segments', () => {
    expect(namespaceLink('c/m', 'ns space')).toBe('/c/c%2Fm/explorer/namespace/ns%20space');
    expect(appLink('local', 'a/b', 'r@1')).toBe('/c/local/apps/catalog.cattle.io.app/a%2Fb/r%401');
  });
});

function appWorkload(overrides: Partial<AIWorkload> = {}): AIWorkload {
  return {
    apiVersion: 'v1', kind: 'AIWorkload',
    metadata: { name: 'w1', namespace: 'suseai' },
    spec: {
      displayName: 'W1',
      source: { sourceType: 'App', app: { chartRepo: 'r', chartName: 'llama', chartVersion: '1', release: 'llama-1' } },
      targetNamespace: 'suseai',
      targetClusters: ['local'],
    },
    ...overrides,
  } as AIWorkload;
}

function blueprintWorkload(overrides: Partial<AIWorkload> = {}): AIWorkload {
  return {
    apiVersion: 'v1', kind: 'AIWorkload',
    metadata: { name: 'w2', namespace: 'suseai' },
    spec: {
      displayName: 'W2',
      source: { sourceType: 'Blueprint', blueprint: { name: 'rag', version: '1' } },
      targetNamespace: 'suseai',
      targetClusters: ['local'],
    },
    ...overrides,
  } as AIWorkload;
}

const clusters: ClusterInfo[] = [{ id: 'local', name: 'Local', ready: true }];

describe('workloadTargetClusters', () => {
  it('returns the target clusters', () => {
    expect(workloadTargetClusters(appWorkload())).toEqual(['local']);
  });
  it('returns [] when unset', () => {
    expect(workloadTargetClusters(appWorkload({ spec: { ...appWorkload().spec, targetClusters: undefined } }))).toEqual([]);
  });
});

describe('workloadRancherLinks', () => {
  it('App: app link, single cluster, cluster display name resolved', () => {
    const links = workloadRancherLinks(appWorkload(), clusters);
    expect(links.primary).toMatchObject({ kind: 'app', disabled: false });
    expect(links.primary.targets).toEqual([{
      clusterId: 'local', clusterName: 'Local', url: '/c/local/apps/catalog.cattle.io.app/suseai/llama-1',
    }]);
  });

  it('Blueprint: primary is namespace kind, links to the namespace-detail URL', () => {
    const links = workloadRancherLinks(blueprintWorkload(), clusters);
    expect(links.primary.kind).toBe('namespace');
    expect(links.primary.disabled).toBe(false);
    // Rancher's pods-list namespace filter is not URL-addressable -> namespace detail.
    expect(links.primary.targets[0].url).toBe('/c/local/explorer/namespace/suseai');
  });

  it('App without a recorded release: falls back to the namespace link, stays enabled', () => {
    const w = appWorkload();
    (w.spec.source.app as any).release = '';
    const links = workloadRancherLinks(w, clusters);
    expect(links.primary).toMatchObject({ kind: 'app', disabled: false });
    expect(links.primary.targets[0].url).toBe('/c/local/explorer/namespace/suseai');
  });

  it('no target cluster: link disabled with reason', () => {
    const w = appWorkload({ spec: { ...appWorkload().spec, targetClusters: [] } });
    const links = workloadRancherLinks(w, clusters);
    expect(links.primary).toMatchObject({ disabled: true, disabledReason: 'noTargetCluster' });
  });

  it('no namespace: link disabled with reason', () => {
    const w = appWorkload({
      metadata: { name: 'w1', namespace: '' },
      spec:     { ...appWorkload().spec, targetNamespace: '' },
    });
    const links = workloadRancherLinks(w, clusters);
    expect(links.primary).toMatchObject({ disabled: true, disabledReason: 'noNamespace' });
  });

  it('multiple clusters: one target per cluster, unknown id falls back to raw id as name', () => {
    const w = appWorkload({ spec: { ...appWorkload().spec, targetClusters: ['local', 'c-m-xyz'] } });
    const links = workloadRancherLinks(w, clusters);
    expect(links.primary.targets.map(t => t.clusterName)).toEqual(['Local', 'c-m-xyz']);
    expect(links.primary.targets).toHaveLength(2);
  });
});

describe('workloadRancherLinks: navigate-always (status is not gated)', () => {
  const twoClusters: ClusterInfo[] = [
    { id: 'local', name: 'Local', ready: true },
    { id: 'c-m-xyz', name: 'Downstream', ready: true },
  ];

  type ClusterStatus = NonNullable<AIWorkload['status']>['clusterStatuses'];
  function statusOf(clusterStatuses: ClusterStatus): Partial<AIWorkload> {
    return { status: { clusterStatuses } };
  }

  it('Pending single cluster: still enabled and navigable', () => {
    const w = appWorkload(statusOf([{ clusterId: 'local', phase: 'Pending' }]));
    const links = workloadRancherLinks(w, clusters);
    expect(links.primary.disabled).toBe(false);
    expect(links.primary.targets[0].url).toBe('/c/local/apps/catalog.cattle.io.app/suseai/llama-1');
  });

  it('cluster absent from a populated clusterStatuses: still gets a navigable target', () => {
    const w = appWorkload({
      spec:   { ...appWorkload().spec, targetClusters: ['local', 'c-m-xyz'] },
      status: { clusterStatuses: [{ clusterId: 'local', phase: 'Running' }] },
    });
    const links = workloadRancherLinks(w, twoClusters);
    expect(links.primary.disabled).toBe(false);
    expect(links.primary.targets.map(t => t.clusterId)).toEqual(['local', 'c-m-xyz']);
  });

  it('all target clusters Pending: link stays enabled for every cluster', () => {
    const w = appWorkload({
      spec:   { ...appWorkload().spec, targetClusters: ['local', 'c-m-xyz'] },
      status: { clusterStatuses: [{ clusterId: 'local', phase: 'Pending' }, { clusterId: 'c-m-xyz', phase: 'Pending' }] },
    });
    const links = workloadRancherLinks(w, twoClusters);
    expect(links.primary.disabled).toBe(false);
    expect(links.primary.targets).toHaveLength(2);
  });
});
