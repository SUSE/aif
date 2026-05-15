import { describe, it, expect } from 'vitest';

// Pure-logic helpers mirrored from settings.vue for isolated unit testing.
// Keep these in sync with the implementation in pages/settings.vue.
//
// Design intent:
//   buildSpec  → always initialises ALL sections so form fields always have a value to bind.
//   buildCrdSpec → omits sections whose fields are all blank/default so the saved CRD stays clean.
//   This asymmetry is intentional; buildCrdSpec is the canonical output gate.

function emptySpec() {
  return {
    fleet:                 { repoURL: '', branch: 'main', authType: '', credSecretRef: null },
    applicationCollection: { userSecretRef: null, tokenSecretRef: null, categories: [] },
    suseRegistry:          { userSecretRef: null, tokenSecretRef: null, refreshIntervalMinutes: 10 },
    registryEndpoints:     { suseRegistry: '', applicationCollection: '', applicationCollectionAPI: '' },
    catalogDiscovery:      { applicationCollectionMode: 'api' },
    imageRewrite:          { enabled: false, rules: [] },
  };
}

function buildSpec(crdSpec = {}) {
  const s = emptySpec();

  if (crdSpec.fleet) {
    s.fleet = {
      repoURL:       crdSpec.fleet.repoURL || '',
      branch:        crdSpec.fleet.branch || 'main',
      authType:      crdSpec.fleet.authType || '',
      credSecretRef: crdSpec.fleet.credSecretRef || null,
    };
  }
  if (crdSpec.applicationCollection) {
    s.applicationCollection = {
      userSecretRef:  crdSpec.applicationCollection.userSecretRef || null,
      tokenSecretRef: crdSpec.applicationCollection.tokenSecretRef || null,
      categories:     crdSpec.applicationCollection.categories || [],
    };
  }
  if (crdSpec.suseRegistry) {
    s.suseRegistry = {
      userSecretRef:          crdSpec.suseRegistry.userSecretRef || null,
      tokenSecretRef:         crdSpec.suseRegistry.tokenSecretRef || null,
      refreshIntervalMinutes: crdSpec.suseRegistry.refreshIntervalMinutes ?? 10,
    };
  }
  if (crdSpec.registryEndpoints) {
    s.registryEndpoints = { ...s.registryEndpoints, ...crdSpec.registryEndpoints };
  }
  if (crdSpec.catalogDiscovery) {
    s.catalogDiscovery.applicationCollectionMode =
      crdSpec.catalogDiscovery.applicationCollectionMode || 'api';
  }
  if (crdSpec.imageRewrite) {
    s.imageRewrite = {
      enabled: !!crdSpec.imageRewrite.enabled,
      rules:   (crdSpec.imageRewrite.rules || []).map((r) => ({ match: r.match, replace: r.replace })),
    };
  }

  return s;
}

function buildCrdSpec(spec) {
  const out = {};

  if (spec.fleet.repoURL || spec.fleet.credSecretRef?.name) {
    out.fleet = {};
    if (spec.fleet.repoURL) out.fleet.repoURL = spec.fleet.repoURL;
    if (spec.fleet.branch) out.fleet.branch = spec.fleet.branch;
    if (spec.fleet.authType) out.fleet.authType = spec.fleet.authType;
    if (spec.fleet.credSecretRef?.name) out.fleet.credSecretRef = spec.fleet.credSecretRef;
  }

  const ac = spec.applicationCollection;

  if (ac.userSecretRef?.name || ac.tokenSecretRef?.name || ac.categories.length) {
    out.applicationCollection = {};
    if (ac.userSecretRef?.name) out.applicationCollection.userSecretRef = ac.userSecretRef;
    if (ac.tokenSecretRef?.name) out.applicationCollection.tokenSecretRef = ac.tokenSecretRef;
    if (ac.categories.length) out.applicationCollection.categories = ac.categories;
  }

  const sr = spec.suseRegistry;

  if (sr.userSecretRef?.name || sr.tokenSecretRef?.name || sr.refreshIntervalMinutes !== 10) {
    out.suseRegistry = { refreshIntervalMinutes: sr.refreshIntervalMinutes };
    if (sr.userSecretRef?.name) out.suseRegistry.userSecretRef = sr.userSecretRef;
    if (sr.tokenSecretRef?.name) out.suseRegistry.tokenSecretRef = sr.tokenSecretRef;
  }

  const re = spec.registryEndpoints;

  if (re.suseRegistry || re.applicationCollection || re.applicationCollectionAPI) {
    out.registryEndpoints = {};
    if (re.suseRegistry) out.registryEndpoints.suseRegistry = re.suseRegistry;
    if (re.applicationCollection) out.registryEndpoints.applicationCollection = re.applicationCollection;
    if (re.applicationCollectionAPI) out.registryEndpoints.applicationCollectionAPI = re.applicationCollectionAPI;
  }

  if (spec.catalogDiscovery.applicationCollectionMode !== 'api') {
    out.catalogDiscovery = { applicationCollectionMode: spec.catalogDiscovery.applicationCollectionMode };
  }

  if (spec.imageRewrite.enabled || spec.imageRewrite.rules.length) {
    out.imageRewrite = {
      enabled: spec.imageRewrite.enabled,
      rules:   spec.imageRewrite.rules.filter((r) => r.match && r.replace),
    };
  }

  return out;
}

function toSelectorValue(ref) {
  if (!ref?.name) return undefined;

  return { valueFrom: { secretKeyRef: ref } };
}

function fromSelectorValue(val) {
  return val?.valueFrom?.secretKeyRef || null;
}

// ─── buildSpec ─────────────────────────────────────────────────────────────

describe('buildSpec', () => {
  it('returns emptySpec defaults when given empty object', () => {
    const s = buildSpec({});

    expect(s.fleet.branch).toBe('main');
    expect(s.fleet.repoURL).toBe('');
    expect(s.suseRegistry.refreshIntervalMinutes).toBe(10);
    expect(s.catalogDiscovery.applicationCollectionMode).toBe('api');
    expect(s.imageRewrite.enabled).toBe(false);
  });

  it('maps fleet fields from CRD', () => {
    const s = buildSpec({
      fleet: {
        repoURL:       'https://github.com/org/repo.git',
        branch:        'develop',
        authType:      'ssh',
        credSecretRef: { name: 'git-creds', key: 'token' },
      },
    });

    expect(s.fleet.repoURL).toBe('https://github.com/org/repo.git');
    expect(s.fleet.branch).toBe('develop');
    expect(s.fleet.authType).toBe('ssh');
    expect(s.fleet.credSecretRef).toStrictEqual({ name: 'git-creds', key: 'token' });
  });

  it('maps applicationCollection from CRD', () => {
    const s = buildSpec({
      applicationCollection: {
        userSecretRef:  { name: 'appcol-user', key: 'username' },
        tokenSecretRef: { name: 'appcol-token', key: 'token' },
        categories:     ['ai', 'nlp'],
      },
    });

    expect(s.applicationCollection.userSecretRef).toStrictEqual({ name: 'appcol-user', key: 'username' });
    expect(s.applicationCollection.tokenSecretRef).toStrictEqual({ name: 'appcol-token', key: 'token' });
    expect(s.applicationCollection.categories).toStrictEqual(['ai', 'nlp']);
  });

  it('maps suseRegistry from CRD', () => {
    const s = buildSpec({
      suseRegistry: {
        userSecretRef:          { name: 'reg-user', key: 'username' },
        tokenSecretRef:         { name: 'reg-token', key: 'token' },
        refreshIntervalMinutes: 30,
      },
    });

    expect(s.suseRegistry.refreshIntervalMinutes).toBe(30);
    expect(s.suseRegistry.userSecretRef).toStrictEqual({ name: 'reg-user', key: 'username' });
  });

  it('uses refreshIntervalMinutes default of 10 when field is absent', () => {
    const s = buildSpec({ suseRegistry: {} });

    expect(s.suseRegistry.refreshIntervalMinutes).toBe(10);
  });

  it('maps registryEndpoints from CRD (partial override)', () => {
    const s = buildSpec({
      registryEndpoints: { suseRegistry: 'my-registry.internal' },
    });

    expect(s.registryEndpoints.suseRegistry).toBe('my-registry.internal');
    expect(s.registryEndpoints.applicationCollection).toBe('');
  });

  it('maps imageRewrite rules from CRD', () => {
    const s = buildSpec({
      imageRewrite: {
        enabled: true,
        rules:   [{ match: 'nvcr.io', replace: 'registry.suse.com' }],
      },
    });

    expect(s.imageRewrite.enabled).toBe(true);
    expect(s.imageRewrite.rules).toStrictEqual([{ match: 'nvcr.io', replace: 'registry.suse.com' }]);
  });
});

// ─── buildCrdSpec ──────────────────────────────────────────────────────────

describe('buildCrdSpec', () => {
  it('omits fleet when repoURL and credSecretRef are both blank', () => {
    const spec = emptySpec();
    const out = buildCrdSpec(spec);

    expect(out.fleet).toBeUndefined();
  });

  it('includes fleet when repoURL is set', () => {
    const spec = emptySpec();

    spec.fleet.repoURL = 'https://github.com/org/repo.git';
    spec.fleet.branch = 'main';
    spec.fleet.authType = 'token';
    const out = buildCrdSpec(spec);

    expect(out.fleet.repoURL).toBe('https://github.com/org/repo.git');
    expect(out.fleet.branch).toBe('main');
    expect(out.fleet.authType).toBe('token');
  });

  it('includes fleet.credSecretRef only when name is set', () => {
    const spec = emptySpec();

    spec.fleet.repoURL = 'https://github.com/org/repo.git';
    spec.fleet.credSecretRef = { name: 'git-creds', key: 'token' };
    const out = buildCrdSpec(spec);

    expect(out.fleet.credSecretRef).toStrictEqual({ name: 'git-creds', key: 'token' });
  });

  it('omits suseRegistry when all fields are at their defaults', () => {
    const spec = emptySpec();
    const out = buildCrdSpec(spec);

    expect(out.suseRegistry).toBeUndefined();
  });

  it('writes suseRegistry when refreshIntervalMinutes differs from default', () => {
    const spec = emptySpec();

    spec.suseRegistry.refreshIntervalMinutes = 30;
    const out = buildCrdSpec(spec);

    expect(out.suseRegistry.refreshIntervalMinutes).toBe(30);
  });

  it('writes suseRegistry when a secret ref is set', () => {
    const spec = emptySpec();

    spec.suseRegistry.userSecretRef = { name: 'reg-user', key: 'username' };
    const out = buildCrdSpec(spec);

    expect(out.suseRegistry.userSecretRef).toStrictEqual({ name: 'reg-user', key: 'username' });
    expect(out.suseRegistry.refreshIntervalMinutes).toBe(10);
  });

  it('includes applicationCollection when only userSecretRef is set', () => {
    const spec = emptySpec();

    spec.applicationCollection.userSecretRef = { name: 'user-secret', key: 'username' };
    const out = buildCrdSpec(spec);

    expect(out.applicationCollection.userSecretRef).toStrictEqual({ name: 'user-secret', key: 'username' });
    expect(out.applicationCollection.tokenSecretRef).toBeUndefined();
  });

  it('omits registryEndpoints when all are blank', () => {
    const spec = emptySpec();
    const out = buildCrdSpec(spec);

    expect(out.registryEndpoints).toBeUndefined();
  });

  it('includes registryEndpoints when any field is set', () => {
    const spec = emptySpec();

    spec.registryEndpoints.suseRegistry = 'my-registry.internal';
    const out = buildCrdSpec(spec);

    expect(out.registryEndpoints.suseRegistry).toBe('my-registry.internal');
    expect(out.registryEndpoints.applicationCollection).toBeUndefined();
  });

  it('omits catalogDiscovery when mode is default api', () => {
    const spec = emptySpec();
    const out = buildCrdSpec(spec);

    expect(out.catalogDiscovery).toBeUndefined();
  });

  it('includes catalogDiscovery when mode is non-default', () => {
    const spec = emptySpec();

    spec.catalogDiscovery.applicationCollectionMode = 'registry-fallback';
    const out = buildCrdSpec(spec);

    expect(out.catalogDiscovery.applicationCollectionMode).toBe('registry-fallback');
  });

  it('omits imageRewrite when disabled and no rules', () => {
    const spec = emptySpec();
    const out = buildCrdSpec(spec);

    expect(out.imageRewrite).toBeUndefined();
  });

  it('includes imageRewrite when enabled', () => {
    const spec = emptySpec();

    spec.imageRewrite.enabled = true;
    spec.imageRewrite.rules = [{ match: 'nvcr.io', replace: 'registry.suse.com' }];
    const out = buildCrdSpec(spec);

    expect(out.imageRewrite.enabled).toBe(true);
    expect(out.imageRewrite.rules).toStrictEqual([{ match: 'nvcr.io', replace: 'registry.suse.com' }]);
  });

  it('filters imageRewrite rules with blank match or replace', () => {
    const spec = emptySpec();

    spec.imageRewrite.enabled = true;
    spec.imageRewrite.rules = [
      { match: 'nvcr.io', replace: 'registry.suse.com' },
      { match: '', replace: 'registry.suse.com' },
      { match: 'nvcr.io', replace: '' },
    ];
    const out = buildCrdSpec(spec);

    expect(out.imageRewrite.rules).toHaveLength(1);
    expect(out.imageRewrite.rules[0].match).toBe('nvcr.io');
  });
});

// ─── categoriesString ──────────────────────────────────────────────────────

describe('categoriesString computed', () => {
  function joinCategories(categories) {
    return (categories || []).join(', ');
  }

  function splitCategories(val) {
    return val ? val.split(',').map((s) => s.trim()).filter(Boolean) : [];
  }

  it('joins array to comma-separated string', () => {
    expect(joinCategories(['ai', 'nlp', 'vision'])).toBe('ai, nlp, vision');
  });

  it('returns empty string for empty array', () => {
    expect(joinCategories([])).toBe('');
  });

  it('splits comma-separated string to array', () => {
    expect(splitCategories('ai, nlp, vision')).toStrictEqual(['ai', 'nlp', 'vision']);
  });

  it('trims whitespace from entries', () => {
    expect(splitCategories('  ai  ,  nlp  ')).toStrictEqual(['ai', 'nlp']);
  });

  it('filters blank entries', () => {
    expect(splitCategories('ai,,vision')).toStrictEqual(['ai', 'vision']);
  });

  it('returns empty array for empty string', () => {
    expect(splitCategories('')).toStrictEqual([]);
  });
});

// ─── SecretSelector format conversion ─────────────────────────────────────

describe('toSelectorValue / fromSelectorValue', () => {
  it('wraps a CRD SecretKeySelector into SecretSelector format', () => {
    const ref = { name: 'my-secret', key: 'token' };

    expect(toSelectorValue(ref)).toStrictEqual({
      valueFrom: { secretKeyRef: { name: 'my-secret', key: 'token' } },
    });
  });

  it('returns undefined when ref is null', () => {
    expect(toSelectorValue(null)).toBeUndefined();
  });

  it('returns undefined when ref has no name', () => {
    expect(toSelectorValue({ name: '', key: 'token' })).toBeUndefined();
  });

  it('extracts CRD SecretKeySelector from SecretSelector output', () => {
    const selectorVal = { valueFrom: { secretKeyRef: { name: 'my-secret', key: 'token' } } };

    expect(fromSelectorValue(selectorVal)).toStrictEqual({ name: 'my-secret', key: 'token' });
  });

  it('returns null when SecretSelector value is undefined', () => {
    expect(fromSelectorValue(undefined)).toBeNull();
  });

  it('round-trips: toSelectorValue then fromSelectorValue returns original ref', () => {
    const ref = { name: 'my-secret', key: 'token' };

    expect(fromSelectorValue(toSelectorValue(ref))).toStrictEqual(ref);
  });
});
