import { describe, expect, it, vi } from 'vitest';
import { buildLabRows, setLabEnabled, labName, canEdit, addLabCatalog } from '../labs';

const cat = (name: string, entries: any[]): any => ({ metadata: { name }, spec: {}, status: { entries } });
const lab = (name: string, spec: any, status?: any): any => ({ metadata: { name }, spec, status, save: vi.fn() });

describe('buildLabRows', () => {
  const catalogs = [
    cat('partner', [{ name: 'demo', displayName: 'Demo', extension: { version: '0.2.0' } }]),
    cat('builtin', [{ name: 'token-factory', displayName: 'Token Factory', maturity: 'preview' }]),
  ];

  it('puts builtin first, then catalogs, with source and derived names', () => {
    const rows = buildLabRows(catalogs, []);

    expect(rows.map((r) => [r.name, r.source, r.enabled, r.phase])).toEqual([
      ['token-factory', 'builtin', false, 'Disabled'],
      ['partner-demo', 'catalog', false, 'Disabled'],
    ]);
    expect(rows[1].version).toBe('0.2.0');
  });

  it('joins Labs by catalogRef and prefers resolvedVersion', () => {
    const l = lab('token-factory', { enabled: true, catalogRef: { catalog: 'builtin', entry: 'token-factory' } },
      { phase: 'Ready', resolvedVersion: '1.0.0' });
    const rows = buildLabRows(catalogs, [l]);

    expect(rows[0]).toMatchObject({ enabled: true, phase: 'Ready', version: '1.0.0', lab: l });
    expect(rows).toHaveLength(2);
  });

  it('appends Labs without catalogRef as custom', () => {
    const l = lab('mine', { enabled: true, displayName: 'Mine', extension: { version: '3' } });
    const rows = buildLabRows(catalogs, [l]);

    expect(rows[2]).toMatchObject({ name: 'mine', source: 'custom', displayName: 'Mine', version: '3' });
  });

  it('lab name rule', () => {
    expect(labName('builtin', 'x')).toBe('x');
    expect(labName('p', 'x')).toBe('p-x');
    expect(labName('p', 'a'.repeat(80))).toHaveLength(63);
  });
});

describe('setLabEnabled', () => {
  it('creates the Lab when none exists', async () => {
    const save = vi.fn();
    const store = { dispatch: vi.fn().mockResolvedValue({ save }) };
    const [row] = buildLabRows([cat('partner', [{ name: 'demo' }])], []);

    await setLabEnabled(store, row, true);
    expect(store.dispatch).toHaveBeenCalledWith('management/create', {
      type:     'ai-factory.suse.com.lab',
      metadata: { name: 'partner-demo' },
      spec:     { enabled: true, catalogRef: { catalog: 'partner', entry: 'demo' } },
    });
    expect(save).toHaveBeenCalled();
  });

  it('toggles spec.enabled on an existing Lab', async () => {
    const l = lab('token-factory', { enabled: true, catalogRef: { catalog: 'builtin', entry: 'token-factory' } });
    const store = { dispatch: vi.fn() };
    const [row] = buildLabRows([cat('builtin', [{ name: 'token-factory' }])], [l]);

    await setLabEnabled(store, row, false);
    expect(l.spec.enabled).toBe(false);
    expect(l.save).toHaveBeenCalled();
    expect(store.dispatch).not.toHaveBeenCalled();
  });
});

describe('catalog helpers', () => {
  it('addLabCatalog creates and saves', async () => {
    const save = vi.fn();
    const store = { dispatch: vi.fn().mockResolvedValue({ save }) };

    await addLabCatalog(store, 'p', 'https://x/c.json');
    expect(store.dispatch.mock.calls[0][1]).toMatchObject({ metadata: { name: 'p' }, spec: { url: 'https://x/c.json' } });
    expect(save).toHaveBeenCalled();
  });

  it('canEdit reads schema methods', () => {
    const mk = (schema: any) => ({ getters: { 'management/schemaFor': () => schema } });

    expect(canEdit(mk({ collectionMethods: ['GET', 'POST'], resourceMethods: ['GET', 'PUT'] }), 't')).toBe(true);
    expect(canEdit(mk({ collectionMethods: ['GET'], resourceMethods: ['GET', 'PUT'] }), 't')).toBe(false);
    expect(canEdit(mk(undefined), 't')).toBe(false);
    expect(canEdit(mk({ resourceMethods: ['DELETE'] }), 't', 'delete')).toBe(true);
  });
});
