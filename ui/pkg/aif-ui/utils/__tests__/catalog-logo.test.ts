import { describe, expect, it } from 'vitest';
import { browserSafeCatalogLogo } from '../catalog-logo';

describe('browserSafeCatalogLogo', () => {
  it.each([
    'https://apps.rancher.io/logos/ollama.png',
    'http://gitea.internal/logo.png',
    '//cdn.example.test/logo.svg',
    '/assets/catalog/ollama.svg',
    'logo.png',
    'javascript:alert(1)',
    'data:image/svg+xml;base64,PHN2Zy8+',
  ])('rejects an automatic external or ambiguous request: %s', (value) => {
    expect(browserSafeCatalogLogo(value)).toBeUndefined();
  });

  it('allows a self-contained raster image', () => {
    expect(browserSafeCatalogLogo('data:image/png;base64,iVBORw0KGgo='))
      .toBe('data:image/png;base64,iVBORw0KGgo=');
  });
});
