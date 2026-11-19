import type { ComponentValuesFrom } from '../types/blueprint-types';
import type { useT } from '../composables/useT';
import {
  DNS_LABEL_PATTERN, DNS_LABEL_MAX, DNS_SUBDOMAIN_PATTERN, DNS_SUBDOMAIN_MAX, CONFIG_MAP_KEY_PATTERN, CONFIG_MAP_KEY_MAX,
} from '../types/blueprint-types';

// Mirrors the CRD's MaxItems on components[].valuesFrom.
export const VALUES_FROM_MAX = 8;

export type ValuesFromKind = 'Secret' | 'ConfigMap';

// Editable form of one components[].valuesFrom entry; blank optional fields are ''.
export interface ValuesFromRow {
  kind:      ValuesFromKind;
  name:      string;
  namespace: string;
  key:       string;
}

export interface ValuesFromSource {
  kind:       ValuesFromKind;
  name:       string;
  namespace?: string;
  key?:       string;
}

export interface ValuesFromRowError {
  index:  number;
  field:  'name' | 'namespace' | 'key';
  reason: 'required' | 'invalid' | 'notAllowed';
}

export function emptyValuesFromRow(): ValuesFromRow {
  return {
    kind: 'Secret', name: '', namespace: '', key: '',
  };
}

export function describeValuesFrom(vf?: ComponentValuesFrom[]): ValuesFromSource[] {
  return (vf || []).flatMap((entry) => {
    const ref = entry.secretKeyRef || entry.configMapKeyRef;

    if (!ref) return [];
    const src: ValuesFromSource = { kind: entry.secretKeyRef ? 'Secret' : 'ConfigMap', name: ref.name };

    if (ref.namespace) src.namespace = ref.namespace;
    if (ref.key) src.key = ref.key;

    return [src];
  });
}

export function valuesFromToRows(vf?: ComponentValuesFrom[]): ValuesFromRow[] {
  return describeValuesFrom(vf).map((s) => ({
    kind: s.kind, name: s.name, namespace: s.namespace || '', key: s.key || '',
  }));
}

function trimmed(row: ValuesFromRow) {
  return { name: row.name.trim(), namespace: row.namespace.trim(), key: row.key.trim() };
}

function isBlank(row: ValuesFromRow): boolean {
  const { name, namespace, key } = trimmed(row);

  return !name && !namespace && !key;
}

// rowsToValuesFrom returns undefined when no source is left, so a component
// without valuesFrom serializes exactly as before.
export function rowsToValuesFrom(rows: ValuesFromRow[]): ComponentValuesFrom[] | undefined {
  const out = rows.filter((r) => !isBlank(r)).map((r) => {
    const { name, namespace, key } = trimmed(r);
    const ref: ComponentValuesFrom['secretKeyRef'] = { name };

    if (namespace) ref.namespace = namespace;
    if (key) ref.key = key;

    return r.kind === 'Secret' ? { secretKeyRef: ref } : { configMapKeyRef: ref };
  });

  return out.length ? out : undefined;
}

function isConfigMapKey(key: string): boolean {
  return key.length <= CONFIG_MAP_KEY_MAX && CONFIG_MAP_KEY_PATTERN.test(key) && key !== '.' && !key.startsWith('..');
}

// validateValuesFromRows returns the first problem (row position in `rows`),
// mirroring the operator API's checks; completely empty rows are ignored.
// allowedNamespaces, when given, limits a non-empty namespace to that set.
export function validateValuesFromRows(rows: ValuesFromRow[], allowedNamespaces?: Set<string>): ValuesFromRowError | null {
  for (const [index, row] of rows.entries()) {
    if (isBlank(row)) continue;
    const { name, namespace, key } = trimmed(row);

    if (!name) return { index, field: 'name', reason: 'required' };
    if (name.length > DNS_SUBDOMAIN_MAX || !DNS_SUBDOMAIN_PATTERN.test(name)) return { index, field: 'name', reason: 'invalid' };
    if (namespace && (namespace.length > DNS_LABEL_MAX || !DNS_LABEL_PATTERN.test(namespace))) return { index, field: 'namespace', reason: 'invalid' };
    if (namespace && allowedNamespaces && !allowedNamespaces.has(namespace)) return { index, field: 'namespace', reason: 'notAllowed' };
    if (key && !isConfigMapKey(key)) return { index, field: 'key', reason: 'invalid' };
  }

  return null;
}

export interface ComponentValuesFromError extends ValuesFromRowError {
  component: string;
  namespace?: string;
  allowed?:   string[];
}

// First valuesFrom problem across components, in component order. Like the
// operator, a namespace must be empty (the release namespace) or one some
// component of the Blueprint deploys to; components without a targetNamespace
// add none, since the install namespace isn't known when saving.
export function validateComponentsValuesFrom(
  components: { chartName: string; targetNamespace?: string; valuesFrom?: ComponentValuesFrom[] }[],
): ComponentValuesFromError | null {
  const allowed = new Set(components.map((c) => c.targetNamespace?.trim() || '').filter(Boolean));

  for (const c of components) {
    const rows = valuesFromToRows(c.valuesFrom);
    const err = validateValuesFromRows(rows, allowed);

    if (err?.reason === 'notAllowed') {
      return {
        ...err, component: c.chartName, namespace: rows[err.index].namespace.trim(), allowed: [...allowed].sort(),
      };
    }
    if (err) return { ...err, component: c.chartName };
  }

  return null;
}

export function valuesFromErrorMessage(t: ReturnType<typeof useT>, err: ComponentValuesFromError): string {
  const args = { component: err.component, row: String(err.index + 1) };

  if (err.field === 'name') {
    return err.reason === 'required'
      ? t('suseai.wizard.errors.valuesFromNameRequired', '{component}: values source {row} needs a name.', args)
      : t('suseai.wizard.errors.valuesFromNameInvalid', '{component}: the name of values source {row} must be a valid Kubernetes object name (lowercase letters, digits, "-" and ".", max 253 characters).', args);
  }
  if (err.reason === 'notAllowed') {
    const nsArgs = { ...args, namespace: err.namespace || '' };

    return err.allowed?.length
      ? t('suseai.wizard.errors.valuesFromNamespaceNotAllowed', '{component}: values source {row} reads from namespace {namespace}, which no application of this Blueprint deploys to. Use {allowed}, or leave Namespace empty for the release namespace.', { ...nsArgs, allowed: err.allowed.join(', ') })
      : t('suseai.wizard.errors.valuesFromNamespaceNotAllowedNone', '{component}: values source {row} reads from namespace {namespace}, which no application of this Blueprint deploys to. Leave Namespace empty to use the release namespace.', nsArgs);
  }
  if (err.field === 'namespace') {
    return t('suseai.wizard.errors.valuesFromNamespaceInvalid', '{component}: the namespace of values source {row} must be a valid namespace name (lowercase letters, digits and "-", max 63 characters).', args);
  }

  return t('suseai.wizard.errors.valuesFromKeyInvalid', '{component}: the key of values source {row} may contain only letters, digits, "-", "_" and "." (max 253 characters) and must not start with "..".', args);
}
