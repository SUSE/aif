import { LAB_TYPE, LAB_CATALOG_TYPE, BUILTIN_CATALOG } from '../types/lab-types';
import type { Lab, LabCatalog, LabRow } from '../types/lab-types';

// Store API (@rancher/shell plugins/dashboard-store): actions findAll/create, getters all/schemaFor.
export function loadLabs(store: any): Promise<Lab[]> {
  return store.dispatch('management/findAll', { type: LAB_TYPE });
}

export function loadLabCatalogs(store: any): Promise<LabCatalog[]> {
  return store.dispatch('management/findAll', { type: LAB_CATALOG_TYPE });
}

/** Lab object name chosen by the UI for a catalog entry. */
export function labName(catalog: string, entry: string): string {
  return catalog === BUILTIN_CATALOG ? entry : `${ catalog }-${ entry }`.slice(0, 63);
}

/** Pure: catalogs (builtin first) x entries, joined with Labs by catalogRef, plus unmatched Labs. */
export function buildLabRows(catalogs: LabCatalog[], labs: Lab[]): LabRow[] {
  const rows: LabRow[] = [];
  const used = new Set<Lab>();
  const sorted = [...catalogs].sort((a, b) => {
    const ab = a.metadata.name === BUILTIN_CATALOG ? 0 : 1;
    const bb = b.metadata.name === BUILTIN_CATALOG ? 0 : 1;

    return ab - bb;
  });

  for (const cat of sorted) {
    const cname = cat.metadata.name;

    for (const e of cat.status?.entries || []) {
      const lab = labs.find((l) => l.spec?.catalogRef?.catalog === cname && l.spec.catalogRef.entry === e.name);

      if (lab) used.add(lab);
      rows.push({
        name:        lab?.metadata.name || labName(cname, e.name),
        displayName: e.displayName || e.name,
        description: e.description || '',
        maturity:    e.maturity || '',
        enabled:     !!lab?.spec.enabled,
        phase:       lab?.status?.phase || 'Disabled',
        message:     lab?.status?.message,
        version:     lab?.status?.resolvedVersion || e.extension?.version || e.source?.helm?.version,
        source:      cname === BUILTIN_CATALOG ? 'builtin' : 'catalog',
        catalog:     cname,
        entry:       e.name,
        lab,
      });
    }
  }

  for (const lab of labs) {
    if (used.has(lab)) continue;
    const ref = lab.spec?.catalogRef;

    rows.push({
      name:        lab.metadata.name,
      displayName: lab.spec?.displayName || lab.metadata.name,
      description: lab.spec?.description || '',
      maturity:    lab.spec?.maturity || '',
      enabled:     !!lab.spec?.enabled,
      phase:       lab.status?.phase || 'Disabled',
      message:     lab.status?.message,
      version:     lab.status?.resolvedVersion || lab.spec?.extension?.version || lab.spec?.source?.helm?.version,
      source:      ref ? (ref.catalog === BUILTIN_CATALOG ? 'builtin' : 'catalog') : 'custom',
      catalog:     ref?.catalog,
      entry:       ref?.entry,
      lab,
    });
  }

  return rows;
}

export async function setLabEnabled(store: any, row: LabRow, enabled: boolean): Promise<void> {
  if (row.lab) {
    row.lab.spec.enabled = enabled;
    await row.lab.save?.();

    return;
  }
  if (!row.catalog || !row.entry) throw new Error('Lab has no catalog entry');
  const model = await store.dispatch('management/create', {
    type:     LAB_TYPE,
    metadata: { name: row.name },
    spec:     { enabled, catalogRef: { catalog: row.catalog, entry: row.entry } },
  });

  await model.save();
}

export async function addLabCatalog(store: any, name: string, url: string): Promise<void> {
  const model = await store.dispatch('management/create', {
    type:     LAB_CATALOG_TYPE,
    metadata: { name },
    spec:     { url },
  });

  await model.save();
}

export function removeLabCatalog(model: LabCatalog): Promise<any> {
  return model.remove!();
}

/** Schema based permission check (steve exposes collectionMethods/resourceMethods). */
export function canEdit(store: any, type: string, verb: 'edit' | 'delete' = 'edit'): boolean {
  const schema = store?.getters?.['management/schemaFor']?.(type, false, false);

  if (!schema) return false;
  const coll: string[] = schema.collectionMethods || [];
  const res: string[] = schema.resourceMethods || [];

  if (verb === 'delete') return res.includes('DELETE');

  return coll.includes('POST') && res.includes('PUT');
}
