import { describe, expect, it, vi } from 'vitest';
import {
  buildLabRows, setLabEnabled, acceptLabUpdate, labName, canEdit, addLabCatalog,
} from '../labs';

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
      [labName('builtin', 'token-factory'), 'builtin', false, 'Disabled'],
      [labName('partner', 'demo'), 'catalog', false, 'Disabled'],
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
    expect(labName('p', 'x')).toMatch(/^p-x-[0-9a-f]{8}$/);
    expect(labName('p', 'x')).toBe(labName('p', 'x'));
    // no collision between pairs that join to the same string
    expect(labName('a-b', 'c')).not.toBe(labName('a', 'b-c'));
    // trimmed, never ends the prefix with '-', and child "lab-<name>" fits 63 chars
    const long = labName('p', `${ 'a'.repeat(47) }-${ 'b'.repeat(30) }`);

    expect(long.length + 'lab-'.length).toBeLessThanOrEqual(63);
    expect(long).not.toMatch(/--/);
    expect(long).toMatch(/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/);
  });
});

describe('setLabEnabled', () => {
  it('creates the Lab when none exists', async () => {
    const save = vi.fn();
    const store = { dispatch: vi.fn().mockResolvedValue({ save }) };
    const [row] = buildLabRows([cat('partner', [{
      name: 'demo', displayName: 'Demo', maturity: 'preview', source: { helm: { version: '1.2.0' } },
    }])], []);

    await setLabEnabled(store, row, true);
    expect(store.dispatch).toHaveBeenCalledWith('management/create', {
      type:     'ai-factory.suse.com.lab',
      metadata: { name: labName('partner', 'demo') },
      spec:     {
        enabled: true, displayName: 'Demo', maturity: 'preview', catalogRef: { catalog: 'partner', entry: 'demo', version: '1.2.0' },
      },
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

  it('re-enabling accepts the offered version', async () => {
    const l = lab('x', { enabled: false, catalogRef: { catalog: 'builtin', entry: 'tf', version: '1' } });
    const [row] = buildLabRows([cat('builtin', [{ name: 'tf', source: { helm: { version: '2' } } }])], [l]);

    await setLabEnabled({}, row, true);
    expect(l.spec).toMatchObject({ enabled: true, catalogRef: { version: '2' } });
  });

  it('restores the model when the save is rejected', async () => {
    const l = lab('x', { enabled: false, catalogRef: { catalog: 'builtin', entry: 'tf', version: '1' } });

    l.save.mockRejectedValue(new Error('forbidden'));
    const [row] = buildLabRows([cat('builtin', [{ name: 'tf', source: { helm: { version: '2' } } }])], [l]);

    await expect(setLabEnabled({}, row, true)).rejects.toThrow('forbidden');
    expect(l.spec).toEqual({ enabled: false, catalogRef: { catalog: 'builtin', entry: 'tf', version: '1' } });
  });
});

describe('acceptLabUpdate', () => {
  it('sets catalogRef.version to the offered update', async () => {
    const l = lab('x', { enabled: true, catalogRef: { catalog: 'builtin', entry: 'tf', version: '1' } },
      { phase: 'Ready', resolvedVersion: '1', updateAvailable: '2' });
    const [row] = buildLabRows([cat('builtin', [{ name: 'tf', source: { helm: { version: '2' } } }])], [l]);

    expect(row).toMatchObject({ version: '1', updateAvailable: '2' });
    await acceptLabUpdate(row);
    expect(l.spec.catalogRef.version).toBe('2');
    expect(l.save).toHaveBeenCalled();
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
