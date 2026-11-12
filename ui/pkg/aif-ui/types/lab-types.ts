export const LAB_TYPE = 'ai-factory.suse.com.lab';
export const LAB_CATALOG_TYPE = 'ai-factory.suse.com.labcatalog';
export const BUILTIN_CATALOG = 'builtin';

export type LabPhase = 'Disabled' | 'Pending' | 'Installing' | 'Ready' | 'Failed';

export interface LabUi { label?: string; icon?: string; routeName?: string }

export interface LabCatalogEntry {
  name:         string;
  displayName?: string;
  description?: string;
  maturity?:    string;
  source?:      { kind?: string; helm?: { chartURL?: string; version?: string } };
  extension?:   { name?: string; version?: string };
  ui?:          LabUi;
}

/** LabCatalog object as exposed by steve (model proxies spec/status/metadata). */
export interface LabCatalog {
  metadata: { name: string };
  spec:     { url?: string; displayName?: string };
  status?:  {
    phase?:       'Ready' | 'Error' | string;
    message?:     string;
    lastFetched?: string;
    skipped?:     number;
    entries?:     LabCatalogEntry[];
  };
  save?:   () => Promise<any>;
  remove?: () => Promise<any>;
}

/** Lab object as exposed by steve. */
export interface Lab {
  metadata: { name: string };
  spec:     {
    enabled?:     boolean;
    catalogRef?:  { catalog: string; entry: string };
    source?:      LabCatalogEntry['source'];
    extension?:   LabCatalogEntry['extension'];
    displayName?: string;
    description?: string;
    maturity?:    string;
    ui?:          LabUi;
  };
  status?: { phase?: LabPhase; message?: string; resolvedVersion?: string };
  save?:   () => Promise<any>;
}

export interface LabRow {
  /** Lab object name (existing Lab, or the name that will be created) */
  name:        string;
  displayName: string;
  description: string;
  maturity:    string;
  enabled:     boolean;
  phase:       LabPhase;
  message?:    string;
  version?:    string;
  source:      'builtin' | 'catalog' | 'custom';
  catalog?:    string;
  entry?:      string;
  lab?:        Lab;
}
