import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { test } from 'node:test';

const read = (path) => readFileSync(new URL(`../${ path }`, import.meta.url), 'utf8');
const maskPayloads = (source) => Array.from(
  source.matchAll(/url\("data:image\/svg\+xml;base64,([^"]+)"\)/g),
  (match) => match[1]
);

// [pageId, componentName, title, l10nKey]
// l10nKey differs from pageId when the slug contains hyphens (camelCase in yaml/templates)
const pages = [
  ['overview',        'OverviewPage',        'Overview',        'overview'],
  ['apps',            'AppsPage',            'Apps',            'apps'],
  ['blueprints',      'BlueprintsPage',      'Blueprints',      'blueprints'],
  ['workloads',       'WorkloadsPage',       'Workloads',       'workloads'],
  ['settings',        'SettingsPage',        'Settings',        'settings']
];

// Convert a kebab page slug to the UPPER_SNAKE constant name used in PAGE_IDS
const toConst = (id) => id.toUpperCase().replace(/-/g, '_');

test('P6-1 type constants define product, pages, and CRD types', () => {
  const source = read('config/types.ts');

  assert.match(source, /PRODUCT_NAME\s*=\s*'ai-factory'/);
  assert.match(source, /MANAGEMENT_CLUSTER\s*=\s*'local'/);

  for (const [page] of pages) {
    assert.match(source, new RegExp(`${ toConst(page) }:\\s*'${ page }'`));
  }

  for (const crd of ['blueprint', 'workload', 'settings']) {
    assert.match(source, new RegExp(`ai\\.suse\\.com\\.${ crd }`));
  }
});

test('P6-1 product registration exposes flat navigation', () => {
  const source = read('config/aif-product.ts');

  assert.match(source, /product\(\{/);
  assert.match(source, /inStore:\s*'management'/);
  assert.match(source, /isMultiClusterApp:\s*true/);
  assert.match(source, /showClusterSwitcher:\s*false/);

  // Pin complete navigation order per P6-1-refactor acceptance criteria
  assert.match(source, /basicType\(\[\s*PAGE_IDS\.OVERVIEW,\s*PAGE_IDS\.APPS,\s*PAGE_IDS\.BLUEPRINTS,\s*PAGE_IDS\.WORKLOADS,\s*PAGE_IDS\.SETTINGS\s*\]\)/);

  assert.match(source, /basicType\(\[CRD_TYPES\.BLUEPRINT,\s*CRD_TYPES\.WORKLOAD,\s*CRD_TYPES\.SETTINGS\]\)/);
  assert.match(source, /configureType\(CRD_TYPES\.BLUEPRINT,\s*\{[^}]*isCreatable:\s*false/s);
  assert.match(source, /configureType\(CRD_TYPES\.BLUEPRINT,\s*\{[^}]*isRemovable:\s*false/s);
  assert.match(source, /configureType\(CRD_TYPES\.SETTINGS,\s*\{[^}]*isRemovable:\s*false/s);
  assert.doesNotMatch(source, /weightGroup/);

  for (const [page,, , l10nKey] of pages) {
    assert.match(source, new RegExp(`PAGE_IDS\\.${ toConst(page) }`));
    assert.match(source, new RegExp(`aif\\.nav\\.${ l10nKey }`));
  }

  assert.match(source, /weight:\s*page\.weight/);
});

test('P6-1 routes map every page to a lazy-loaded component', () => {
  const source = read('routing/index.ts');

  for (const [page] of pages) {
    assert.match(source, new RegExp(`\\$\\{\\s*PRODUCT_NAME\\s*\\}-c-cluster-\\$\\{\\s*PAGE_IDS\\.${ toConst(page) }\\s*\\}`));
    assert.match(source, new RegExp(`/c/:cluster/\\$\\{\\s*PRODUCT_NAME\\s*\\}/\\$\\{\\s*PAGE_IDS\\.${ toConst(page) }\\s*\\}`));
    assert.match(source, new RegExp(`import\\('\\.\\./pages/${ page }\\.vue'\\)`));
    assert.match(source, new RegExp(`pageId:\\s*PAGE_IDS\\.${ toConst(page) }`));
  }
});

test('P6-1 l10n and placeholder pages cover all navigation entries', () => {
  const l10n = read('l10n/en-us.yaml');

  assert.match(l10n, /label:\s*'SUSE AI Factory'/);

  for (const [page, componentName, title, l10nKey] of pages) {
    const component = read(`pages/${ page }.vue`);

    assert.match(l10n, new RegExp(`${ l10nKey }:\\s*'`));
    assert.match(l10n, new RegExp(`title:\\s*'${ title }'`));
    assert.match(component, new RegExp(`name:\\s*'${ componentName }'`));
    assert.match(component, new RegExp(`aif\\.pages\\.${ l10nKey }\\.title`));
  }
});

test('P6-1 entry point wires product, routes, and localization', () => {
  const source = read('index.ts');

  assert.match(source, /import routes from '\.\/routing'/);
  assert.match(source, /import '\.\/style\/brand\.css'/);
  assert.doesNotMatch(source, /SteveFactory/);
  assert.doesNotMatch(source, /addDashboardStore/);
  assert.match(source, /plugin\.addProduct\(require\('\.\/config\/aif-product'\)\)/);
  assert.match(source, /plugin\.addRoutes\(routes\)/);
  assert.match(source, /plugin\.addL10n\('en-us',\s*require\('\.\/l10n\/en-us\.yaml'\)\)/);
});

test('P6-1 package metadata exposes a local extension tile icon', () => {
  const pkg = JSON.parse(read('package.json'));
  const entry = read('index.ts');

  assert.equal(pkg.icon, './assets/logo.svg');
  const iconPath = pkg.icon.replace(/^\.\//, '');
  const iconUrl = new URL(`../${ iconPath }`, import.meta.url);

  assert.ok(existsSync(iconUrl));
  assert.match(read(iconPath), /fill="#30BA78"/);
  assert.match(entry, /plugin\.metadata\s*=\s*\{\s*\.\.\.require\('\.\/package\.json'\),\s*icon:\s*require\('\.\/assets\/logo\.svg'\)\s*\}/s);
});

test('P6-1 brand CSS registers the AI Factory sidebar icon', () => {
  const source = read('style/brand.css');
  const payloads = maskPayloads(source);
  const svg = Buffer.from(payloads[0], 'base64').toString('utf8');

  assert.match(source, /\.icon-ai-factory::before/);
  assert.doesNotMatch(source, /\.icon-suseai::before/);
  assert.match(source, /background-color:\s*currentColor/);
  assert.match(source, /-webkit-mask:\s*url\("data:image\/svg\+xml;base64,/);
  assert.match(source, /mask:\s*url\("data:image\/svg\+xml;base64,/);
  assert.equal(payloads.length, 2);
  assert.equal(payloads[0], payloads[1]);
  assert.match(svg, /viewBox="28 2 80 44"/);
  assert.match(svg, /fill="#fff"/);
  assert.match(svg, /M101\.408/);
  assert.doesNotMatch(svg, /viewBox="0 0 24 24"/);
});

test('P6-1 mock API stub exports future resource groups', () => {
  const source = read('utils/mock-api.ts');

  for (const group of ['blueprints', 'workloads', 'apps', 'settings']) {
    assert.match(source, new RegExp(`${ group }: \\{`));
  }

  assert.match(source, /USE_MOCK_API/);
});
