import { LAB_TYPE, LAB_CATALOG_TYPE, BUILTIN_CATALOG } from '../types/lab-types';
import type { Lab, LabCatalog, LabRow } from '../types/lab-types';

// Store API (@rancher/shell plugins/dashboard-store): actions findAll/create, getters all/schemaFor.
export function loadLabs(store: any): Promise<Lab[]> {
  return store.dispatch('management/findAll', { type: LAB_TYPE });
}

export function loadLabCatalogs(store: any): Promise<LabCatalog[]> {
  return store.dispatch('management/findAll', { type: LAB_CATALOG_TYPE });
}

/** FNV-1a 32-bit, hex: short, stable, no crypto needed (names only). */
function shortHash(s: string): string {
  let h = 0x811c9dc5;

  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }

  return (h >>> 0).toString(16).padStart(8, '0');
}

/**
 * Lab object name chosen by the UI for a catalog entry: a readable
 * "<catalog>-<entry>" prefix plus a hash of the pair. The hash keeps names unique
 * ("a-b"+"c" vs "a"+"b-c", builtin vs a custom Lab of the same name) and the
 * prefix is trimmed so the child "lab-<name>" stays a valid 63-char name.
 */
export function labName(catalog: string, entry: string): string {
  const prefix = `${ catalog }-${ entry }`.slice(0, 50).replace(/-+$/, '');

  return `${ prefix }-${ shortHash(`${ catalog }/${ entry }`) }`;
}

/** Version a catalog entry offers: chart version for Helm, else extension version. */
export function entryVersion(e: { source?: { helm?: { version?: string } }; extension?: { version?: string } }): string | undefined {
  return e.source?.helm?.version || e.extension?.version;
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
        version:     lab?.status?.resolvedVersion || entryVersion(e),
        updateAvailable: lab?.status?.updateAvailable,
        offered:     entryVersion(e),
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
      version:     lab.status?.resolvedVersion || entryVersion(lab.spec || {}),
      updateAvailable: lab.status?.updateAvailable,
      source:      ref ? (ref.catalog === BUILTIN_CATALOG ? 'builtin' : 'catalog') : 'custom',
      catalog:     ref?.catalog,
      entry:       ref?.entry,
      lab,
    });
  }

  return rows;
}

/**
 * Saves a change to an existing Lab model, restoring the store copy if the save
 * fails so the UI never shows a state the cluster rejected.
 */
async function saveLab(lab: Lab, change: (l: Lab) => void): Promise<void> {
  const before = JSON.parse(JSON.stringify(lab.spec));

  change(lab);
  try {
    await lab.save?.();
  } catch (e) {
    lab.spec = before;
    throw e;
  }
}

/** Enabling accepts the version the catalog offers now; later catalog changes wait for acceptLabUpdate. */
export async function setLabEnabled(store: any, row: LabRow, enabled: boolean): Promise<void> {
  if (row.lab) {
    await saveLab(row.lab, (l) => {
      // Re-enabling installs from scratch (the child was removed), so accept what
      // the catalog offers now.
      if (enabled && !l.spec.enabled && l.spec.catalogRef && row.offered) {
        l.spec.catalogRef.version = row.offered;
      }
      l.spec.enabled = enabled;
    });

    return;
  }
  if (!row.catalog || !row.entry) throw new Error('Lab has no catalog entry');
  const catalogRef: Record<string, string> = { catalog: row.catalog, entry: row.entry };

  if (row.offered) catalogRef.version = row.offered;
  // Copy the entry's labels so the Lab still reads well if its catalog goes away.
  const spec: Record<string, unknown> = { enabled, catalogRef };

  if (row.displayName) spec.displayName = row.displayName;
  if (row.description) spec.description = row.description;
  if (row.maturity) spec.maturity = row.maturity;
  const model = await store.dispatch('management/create', {
    type:     LAB_TYPE,
    metadata: { name: row.name },
    spec,
  });

  await model.save();
}

/** Accepts the version the catalog now offers (status.updateAvailable); the operator then upgrades. */
export async function acceptLabUpdate(row: LabRow): Promise<void> {
  const version = row.updateAvailable;

  if (!row.lab?.spec.catalogRef || !version) throw new Error('No update available');
  await saveLab(row.lab, (l) => {
    l.spec.catalogRef!.version = version;
  });
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

/**
 * Deletes a Lab. Works without the Lab controller (Labs turned off): the child
 * InstallAIExtension is owned by the Lab, so garbage collection deletes it and the
 * InstallAIExtension controller's finalizer uninstalls the release and UIPlugin.
 */
export function removeLab(model: Lab): Promise<any> {
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
