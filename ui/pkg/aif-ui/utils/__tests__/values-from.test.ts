import { describe, expect, it } from 'vitest';
import {
  valuesFromToRows, rowsToValuesFrom, validateValuesFromRows, validateComponentsValuesFrom, describeValuesFrom, emptyValuesFromRow, valuesFromErrorMessage,
} from '../values-from';
import type { ValuesFromRow } from '../values-from';
import type { ComponentValuesFrom } from '../../types/blueprint-types';

const row = (r: Partial<ValuesFromRow>): ValuesFromRow => ({ ...emptyValuesFromRow(), ...r });

const runai: ComponentValuesFrom[] = [
  { secretKeyRef: { name: 'runai-cluster-values', namespace: 'runai-backend', key: 'values.yaml' } },
  { configMapKeyRef: { name: 'runai-config' } },
];

describe('valuesFromToRows / rowsToValuesFrom', () => {
  it('maps sources to rows with blank optional fields', () => {
    expect(valuesFromToRows(runai)).toEqual([
      { kind: 'Secret', name: 'runai-cluster-values', namespace: 'runai-backend', key: 'values.yaml' },
      { kind: 'ConfigMap', name: 'runai-config', namespace: '', key: '' },
    ]);
  });

  it('returns no rows for no sources', () => {
    expect(valuesFromToRows(undefined)).toEqual([]);
    expect(valuesFromToRows([])).toEqual([]);
  });

  it('round-trips without adding fields', () => {
    expect(rowsToValuesFrom(valuesFromToRows(runai))).toEqual(runai);
  });

  it('trims fields and omits blank namespace and key', () => {
    expect(rowsToValuesFrom([row({ kind: 'ConfigMap', name: ' cfg ', namespace: '  ', key: ' ' })]))
      .toEqual([{ configMapKeyRef: { name: 'cfg' } }]);
  });

  it('drops rows that are completely empty', () => {
    expect(rowsToValuesFrom([row({}), row({ name: 's' }), row({ kind: 'ConfigMap', name: ' ' })]))
      .toEqual([{ secretKeyRef: { name: 's' } }]);
  });

  it('returns undefined when no rows remain', () => {
    expect(rowsToValuesFrom([])).toBeUndefined();
    expect(rowsToValuesFrom([row({})])).toBeUndefined();
  });
});

describe('validateValuesFromRows', () => {
  it('accepts valid rows and ignores empty ones', () => {
    expect(validateValuesFromRows([
      row({ name: 'runai.cluster-values', namespace: 'runai-backend', key: 'values_1.yaml' }),
      row({}),
    ])).toBeNull();
  });

  it.each([
    [{ namespace: 'ns' }, 'name', 'required'],
    [{ key: 'values.yaml' }, 'name', 'required'],
    [{ name: 'Upper' }, 'name', 'invalid'],
    [{ name: 'bad_name' }, 'name', 'invalid'],
    [{ name: '-lead' }, 'name', 'invalid'],
    [{ name: 'a..b' }, 'name', 'invalid'],
    [{ name: 'a'.repeat(254) }, 'name', 'invalid'],
    [{ name: 's', namespace: 'with.dot' }, 'namespace', 'invalid'],
    [{ name: 's', namespace: 'Upper' }, 'namespace', 'invalid'],
    [{ name: 's', namespace: 'n'.repeat(64) }, 'namespace', 'invalid'],
    [{ name: 's', key: 'has/slash' }, 'key', 'invalid'],
    [{ name: 's', key: 'has space' }, 'key', 'invalid'],
    [{ name: 's', key: '.' }, 'key', 'invalid'],
    [{ name: 's', key: '..values' }, 'key', 'invalid'],
    [{ name: 's', key: 'k'.repeat(254) }, 'key', 'invalid'],
  ] as const)('rejects %j: %s %s', (fields, field, reason) => {
    expect(validateValuesFromRows([row(fields)])).toEqual({ index: 0, field, reason });
  });

  it('accepts names, namespaces and keys at their length limits', () => {
    expect(validateValuesFromRows([row({ name: 'a'.repeat(253), namespace: 'n'.repeat(63), key: 'k'.repeat(253) })])).toBeNull();
  });

  it('reports the position of the first invalid row', () => {
    expect(validateValuesFromRows([row({ name: 'ok' }), row({}), row({ name: 'Bad' })]))
      .toEqual({ index: 2, field: 'name', reason: 'invalid' });
  });

  it('validates trimmed values', () => {
    expect(validateValuesFromRows([row({ name: ' ok ', namespace: ' ns ', key: ' k ' })])).toBeNull();
  });
});

describe('validateComponentsValuesFrom', () => {
  it('reports the first component with an invalid source', () => {
    expect(validateComponentsValuesFrom([
      { chartName: 'a', valuesFrom: runai },
      { chartName: 'b', targetNamespace: 'runai-backend' },
      { chartName: 'c', valuesFrom: [{ configMapKeyRef: { name: 'ok', namespace: 'Bad_NS' } }] },
    ])).toEqual({
      component: 'c', index: 0, field: 'namespace', reason: 'invalid',
    });
  });

  it('accepts components with valid or no sources', () => {
    expect(validateComponentsValuesFrom([{ chartName: 'a', valuesFrom: runai }, { chartName: 'b', targetNamespace: ' runai-backend ' }])).toBeNull();
  });

  it('allows the namespace of any component of the Blueprint', () => {
    expect(validateComponentsValuesFrom([
      { chartName: 'runai-cluster', targetNamespace: 'runai', valuesFrom: [{ secretKeyRef: { name: 'v', namespace: 'runai' } }] },
      { chartName: 'control-plane', targetNamespace: 'runai-backend', valuesFrom: runai },
    ])).toBeNull();
  });

  it('rejects a namespace no component deploys to, listing the allowed ones', () => {
    expect(validateComponentsValuesFrom([
      { chartName: 'b', targetNamespace: 'runai-backend' },
      { chartName: 'a', targetNamespace: 'runai', valuesFrom: [{ secretKeyRef: { name: 'v', namespace: 'kube-system' } }] },
    ])).toEqual({
      component: 'a', index: 0, field: 'namespace', reason: 'notAllowed', namespace: 'kube-system', allowed: ['runai', 'runai-backend'],
    });
  });

  it('adds no namespace for components without a targetNamespace', () => {
    expect(validateComponentsValuesFrom([
      { chartName: 'a', valuesFrom: [{ configMapKeyRef: { name: 'v', namespace: 'apps' } }] },
    ])).toEqual({
      component: 'a', index: 0, field: 'namespace', reason: 'notAllowed', namespace: 'apps', allowed: [],
    });
  });
});

describe('valuesFromErrorMessage', () => {
  const t = ((_key: string, fallback: string, args: Record<string, string> = {}) => fallback.replace(/\{(\w+)\}/g, (_m, k) => args[k] ?? `{${ k }}`)) as unknown as Parameters<typeof valuesFromErrorMessage>[0];

  it('names the namespace and the allowed ones', () => {
    expect(valuesFromErrorMessage(t, {
      component: 'a', index: 0, field: 'namespace', reason: 'notAllowed', namespace: 'kube-system', allowed: ['runai', 'runai-backend'],
    })).toBe('a: values source 1 reads from namespace kube-system, which no application of this Blueprint deploys to. Use runai, runai-backend, or leave Namespace empty for the release namespace.');
  });

  it('only suggests the release namespace when no component has a targetNamespace', () => {
    expect(valuesFromErrorMessage(t, {
      component: 'a', index: 1, field: 'namespace', reason: 'notAllowed', namespace: 'apps', allowed: [],
    })).toBe('a: values source 2 reads from namespace apps, which no application of this Blueprint deploys to. Leave Namespace empty to use the release namespace.');
  });
});

describe('describeValuesFrom', () => {
  it('describes each source with its kind', () => {
    expect(describeValuesFrom(runai)).toEqual([
      { kind: 'Secret', name: 'runai-cluster-values', namespace: 'runai-backend', key: 'values.yaml' },
      { kind: 'ConfigMap', name: 'runai-config' },
    ]);
  });

  it('skips entries without a ref and handles none', () => {
    expect(describeValuesFrom([{}])).toEqual([]);
    expect(describeValuesFrom(undefined)).toEqual([]);
  });
});
