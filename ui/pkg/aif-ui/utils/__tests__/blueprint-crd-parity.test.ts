import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import yaml from 'js-yaml';
import { browserSafeBlueprintIcon } from '../catalog-logo';
import { BLUEPRINT_ORIGINS } from '../../types/blueprint-types';

// The UI must never render an icon the API server would reject, and the CRD
// must reject what the UI treats as unsafe at the scheme level. The UI adds
// host rules (IP literals, private names) on top, so UI ⊆ CRD, not equality.
// ci-aif-extension.yml also triggers on the CRD file so this runs on CRD-only changes.
const crd = yaml.load(readFileSync(path.resolve(__dirname, '../../../../../charts/aif-operator/crds/ai-factory.suse.com_blueprints.yaml'), 'utf8')) as any;
const specProps = crd.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties;
const iconSchema = specProps.icon;
const crdPattern = new RegExp(iconSchema.pattern);

const corpus = [
  'data:image/png;base64,iVBORw0KGgo=',
  'data:image/jpeg;base64,/9j/4AAQ',
  'data:image/svg+xml;base64,PHN2Zy8+',
  'DATA:IMAGE/PNG;base64,iVBORw0KGgo=',
  'data:image/PNG;base64,iVBORw0KGgo=',
  'data:image/png;BASE64,iVBORw0KGgo=',
  'https://raw.githubusercontent.com/SUSE/partner-blueprints/main/partners/acme/logo.png',
  'https://partner.example.com./logo.png',
  'https://10.0.0.1/logo.png',
  'https://',
  'https:///logo.png',
  'https://partner.example.com/a b.png',
  ' https://partner.example.com/logo.png',
  'http://partner.example.com/logo.png',
  'javascript:alert(1)',
  `data:image/png;base64,${ 'A'.repeat(16384) }`,
];

describe('blueprint icon CRD parity', () => {
  it('limits icon size to 16 KB', () => {
    expect(iconSchema.maxLength).toBe(16384);
  });

  it.each(corpus)('UI accepts only what the CRD accepts: %s', (value) => {
    if (browserSafeBlueprintIcon(value)) expect(crdPattern.test(value) && value.length <= iconSchema.maxLength).toBe(true);
  });

  it.each([
    'http://partner.example.com/logo.png',
    'data:image/svg+xml;base64,PHN2Zy8+',
    'javascript:alert(1)',
  ])('CRD rejects %s', (value) => {
    expect(crdPattern.test(value)).toBe(false);
  });
});

// The UI's source list drives the badge and the TS type; it must match the
// CRD enum exactly or the UI offers a value the API server rejects.
describe('blueprint source CRD parity', () => {
  it('matches the CRD source enum', () => {
    expect([...BLUEPRINT_ORIGINS].sort()).toEqual([...specProps.source.enum].sort());
  });
});
