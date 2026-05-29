import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
  toBlueprintVersion,
  groupByLineage,
  sortVersionsDesc,
  selectDefaultVersion,
  readUnreachable,
  readPublisherOverride,
  parseVersion,
  compareVersions
} from '@pkg/ai-factory/utils/blueprint';

function bp({ name, lineage, version, phase, source = 'Published' }) {
  return {
    metadata: {
      name,
      labels: { 'ai.suse.com/blueprint-name': lineage }
    },
    spec: {
      blueprintName: lineage,
      version,
      useCase:       'rag',
      description:   `${ lineage } v${ version }`,
      changeDescription: '',
      components: [{ name: 'svc', kind: 'App', app: { repo: 'r', chart: 'c', version: '1.0.0' } }],
      source: source === 'WrapsVendorChart'
        ? { type: 'WrapsVendorChart', vendorChartRef: { provider: 'nvidia', repo: 'oci://x', chart: 'rag', version } }
        : { type: 'Published', publishedFrom: { bundleNamespace: 'ns', bundleName: 'b', bundleGeneration: 1 } },
      publishedBy: 'alice',
      publishedAt: '2026-05-01T00:00:00Z'
    },
    status: phase ? { phase } : {}
  };
}

describe('toBlueprintVersion', () => {
  it('maps a Published CR to a BlueprintVersion', () => {
    const v = toBlueprintVersion(bp({ name: 'rag.1.0.0', lineage: 'rag', version: '1.0.0', phase: 'Active' }));

    expect(v.id).toBe('rag.1.0.0');
    expect(v.lineage).toBe('rag');
    expect(v.version).toBe('1.0.0');
    expect(v.phase).toBe('Active');
    expect(v.origin).toBe('Published');
    expect(v.vendorChart).toBeUndefined();
    expect(v.components).toHaveLength(1);
  });

  it('defaults phase to Active when status.phase is missing', () => {
    const v = toBlueprintVersion(bp({ name: 'rag.1.0.0', lineage: 'rag', version: '1.0.0' }));

    expect(v.phase).toBe('Active');
  });

  it('captures vendorChart when source is WrapsVendorChart', () => {
    const v = toBlueprintVersion(bp({ name: 'rag.2.0.0', lineage: 'rag', version: '2.0.0', phase: 'Active', source: 'WrapsVendorChart' }));

    expect(v.origin).toBe('WrapsVendorChart');
    expect(v.vendorChart?.chart).toBe('rag');
    expect(v.vendorChart?.version).toBe('2.0.0');
  });
});

describe('sortVersionsDesc', () => {
  it('orders 1.0.0 < 1.1.0 < 1.10.0 < 2.0.0 (returns descending)', () => {
    const vs = ['1.0.0', '1.10.0', '1.1.0', '2.0.0'].map((v) =>
      toBlueprintVersion(bp({ name: `x.${ v }`, lineage: 'x', version: v, phase: 'Active' })));
    const sorted = sortVersionsDesc(vs).map((x) => x.version);

    expect(sorted).toEqual(['2.0.0', '1.10.0', '1.1.0', '1.0.0']);
  });

  it('treats missing semver parts as 0 (defense in depth — CRD enforces shape)', () => {
    const vs = ['1.0', '1.0.0', '1', '2.0.0'].map((v) =>
      toBlueprintVersion(bp({ name: `x.${ v }`, lineage: 'x', version: v, phase: 'Active' })));
    const sorted = sortVersionsDesc(vs).map((x) => x.version);

    // 2.0.0 is unambiguously largest; the three "1.x" entries all parse to [1,0,0]
    // so they end up grouped together in the tail in stable order.
    expect(sorted[0]).toBe('2.0.0');
    expect(sorted.slice(1).sort()).toEqual(['1', '1.0', '1.0.0']);
  });
});

describe('parseVersion', () => {
  it('parses canonical major.minor.patch into a numeric triple', () => {
    expect(parseVersion('1.2.3')).toEqual([1, 2, 3]);
    expect(parseVersion('10.20.30')).toEqual([10, 20, 30]);
  });

  it('fills missing parts with 0 (CRD pattern bounds this in practice)', () => {
    expect(parseVersion('1')).toEqual([1, 0, 0]);
    expect(parseVersion('1.2')).toEqual([1, 2, 0]);
    expect(parseVersion('')).toEqual([0, 0, 0]);
  });

  it('coerces non-numeric parts to 0 (defense in depth, not a contract for callers)', () => {
    // The CRD's ^\d+\.\d+\.\d+$ pattern rejects these shapes before they reach the UI.
    // These assertions pin current behavior so a future strict-parse rewrite can't
    // silently flip the upgrade-target filter from "include nothing" to "include all".
    expect(parseVersion('v1.2.3')).toEqual([0, 2, 3]);
    expect(parseVersion('1.2-beta.3')).toEqual([1, 0, 3]);
    expect(parseVersion('abc')).toEqual([0, 0, 0]);
  });
});

describe('compareVersions', () => {
  it('returns negative when a < b, zero when equal, positive when a > b', () => {
    expect(compareVersions('1.0.0', '2.0.0')).toBeLessThan(0);
    expect(compareVersions('1.0.0', '1.0.0')).toBe(0);
    expect(compareVersions('2.0.0', '1.0.0')).toBeGreaterThan(0);
  });

  it('compares numerically (1.10.0 > 1.9.0), not lexically', () => {
    expect(compareVersions('1.10.0', '1.9.0')).toBeGreaterThan(0);
    expect(compareVersions('1.2.10', '1.2.9')).toBeGreaterThan(0);
  });

  it('treats malformed inputs as 0.0.0 (matches parseVersion contract)', () => {
    // If a malformed string ever reaches this comparator it will appear strictly
    // less than any non-zero version on the other side — keeps the upgrade-target
    // filter from accidentally promoting garbage to "newer than current."
    expect(compareVersions('abc', '0.0.1')).toBeLessThan(0);
    expect(compareVersions('1.0.0', 'abc')).toBeGreaterThan(0);
    expect(compareVersions('abc', 'xyz')).toBe(0);
  });
});

describe('groupByLineage', () => {
  it('groups by spec.blueprintName and sorts versions descending', () => {
    const crs = [
      bp({ name: 'rag.1.0.0', lineage: 'rag', version: '1.0.0', phase: 'Active' }),
      bp({ name: 'rag.2.0.0', lineage: 'rag', version: '2.0.0', phase: 'Active' }),
      bp({ name: 'vision.1.0.0', lineage: 'vision', version: '1.0.0', phase: 'Active' })
    ];
    const lineages = groupByLineage(crs);

    expect(lineages.map((l) => l.lineage).sort()).toEqual(['rag', 'vision']);
    const rag = lineages.find((l) => l.lineage === 'rag');

    expect(rag.versions.map((v) => v.version)).toEqual(['2.0.0', '1.0.0']);
    expect(rag.latestActive.version).toBe('2.0.0');
  });

  it('falls back to the blueprint-name label when spec.blueprintName is missing', () => {
    const cr = bp({ name: 'rag.1.0.0', lineage: 'rag', version: '1.0.0', phase: 'Active' });

    delete cr.spec.blueprintName;
    const lineages = groupByLineage([cr]);

    expect(lineages).toHaveLength(1);
    expect(lineages[0].lineage).toBe('rag');
  });

  it('returns [] for empty input', () => {
    expect(groupByLineage([])).toEqual([]);
  });
});

describe('selectDefaultVersion', () => {
  function lineage(versions) {
    const vs = versions.map(([v, phase]) => toBlueprintVersion(bp({ name: `x.${ v }`, lineage: 'x', version: v, phase })));

    return { lineage: 'x', versions: sortVersionsDesc(vs), latestActive: vs.find((x) => x.phase === 'Active') };
  }

  it('prefers latest Active', () => {
    const l = lineage([['1.0.0', 'Active'], ['2.0.0', 'Active'], ['3.0.0', 'Deprecated']]);

    expect(selectDefaultVersion(l).version).toBe('2.0.0');
  });

  it('falls back to latest Deprecated when no Active', () => {
    const l = lineage([['1.0.0', 'Deprecated'], ['2.0.0', 'Withdrawn']]);

    expect(selectDefaultVersion(l).version).toBe('1.0.0');
  });

  it('falls back to latest Withdrawn when all Withdrawn', () => {
    const l = lineage([['1.0.0', 'Withdrawn'], ['2.0.0', 'Withdrawn']]);

    expect(selectDefaultVersion(l).version).toBe('2.0.0');
  });
});

describe('readUnreachable', () => {
  it('returns true when CatalogRefreshReady is False', () => {
    const settings = { status: { conditions: [{ type: 'CatalogRefreshReady', status: 'False', reason: 'RegistryUnreachable' }] } };

    expect(readUnreachable(settings)).toBe(true);
  });

  it('returns false when CatalogRefreshReady is True', () => {
    const settings = { status: { conditions: [{ type: 'CatalogRefreshReady', status: 'True' }] } };

    expect(readUnreachable(settings)).toBe(false);
  });

  it('returns false when the condition is absent', () => {
    expect(readUnreachable({ status: { conditions: [] } })).toBe(false);
  });

  it('returns false when settings is null/undefined', () => {
    expect(readUnreachable(null)).toBe(false);
    expect(readUnreachable(undefined)).toBe(false);
  });
});

describe('readPublisherOverride', () => {
  const originalLocalStorage = globalThis.localStorage;

  beforeEach(() => {
    globalThis.localStorage = {
      _data: {},
      getItem(k) { return this._data[k] ?? null; },
      setItem(k, v) { this._data[k] = String(v); },
      removeItem(k) { delete this._data[k]; }
    };
  });

  afterEach(() => { globalThis.localStorage = originalLocalStorage; });

  it('defaults to false when no override is set', () => {
    expect(readPublisherOverride().value).toBe(false);
  });

  it('flips to true when aifPublisherOverride === "1"', () => {
    globalThis.localStorage.setItem('aifPublisherOverride', '1');
    expect(readPublisherOverride().value).toBe(true);
  });
});
