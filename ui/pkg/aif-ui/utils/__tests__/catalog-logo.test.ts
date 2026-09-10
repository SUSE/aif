// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { browserSafeCatalogLogo, resolveCatalogLogo, onCatalogLogoError } from '../catalog-logo';

const ollama = { library: 'suse-ai', slug_name: 'ollama' };
const inline = 'data:image/png;base64,iVBORw0KGgo=';

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

describe('resolveCatalogLogo', () => {
  it('prefers a supplied raster data URL over the bundled logo', () => {
    expect(resolveCatalogLogo({ ...ollama, logo_url: inline })).toBe(inline);
  });

  it.each([
    undefined,
    '',
    '/logos/ollama.png',
    'https://apps.rancher.io/logos/ollama.png',
    'https://api.apps.rancher.io/logos/ollama.png',
    'https://mirror.internal/logos/ollama.png',
    '//external.example/logo.png',
    'javascript:alert(1)',
    'data:image/svg+xml;base64,PHN2Zy8+',
  ])('uses a self-contained bundled logo for unavailable metadata: %s', (logo_url) => {
    const logo = resolveCatalogLogo({ ...ollama, logo_url });
    expect(logo).toMatch(/^data:image\/png;base64,/);
    expect(browserSafeCatalogLogo(logo)).toBe(logo);
  });

  it.each(['nvidia', 'custom', undefined, 'constructor', '__proto__'])('keeps library identity: %s', (library) => {
    expect(resolveCatalogLogo({ ...ollama, library })).toBeUndefined();
  });

  it.each(['new-app', 'constructor', '__proto__'])('leaves unknown apps to the caller placeholder: %s', (slug_name) => {
    expect(resolveCatalogLogo({ ...ollama, slug_name })).toBeUndefined();
  });

  it('accepts inline logos for apps absent from the manifest', () => {
    expect(resolveCatalogLogo({ library: 'custom', slug_name: 'new-app', logo_url: inline })).toBe(inline);
  });
});

describe('catalog logo load failures', () => {
  const placeholder = '/assets/generic-app.svg';

  it('advances from an undecodable inline image to the bundle, then placeholder, without looping', () => {
    const app = { ...ollama, logo_url: inline };
    const image = document.createElement('img');
    image.src = resolveCatalogLogo(app)!;
    image.addEventListener('error', event => onCatalogLogoError(event, app, placeholder));
    const setSource = vi.spyOn(image, 'src', 'set');

    image.dispatchEvent(new Event('error'));
    expect(image.getAttribute('src')).toBe(resolveCatalogLogo(ollama));
    image.dispatchEvent(new Event('error'));
    expect(image.getAttribute('src')).toBe(placeholder);
    image.dispatchEvent(new Event('error'));
    expect(image.getAttribute('src')).toBe(placeholder);
    expect(setSource).toHaveBeenCalledTimes(2);
  });

  it('does not retry the bundle when the supplied logo is already the bundled image', () => {
    const app = { ...ollama, logo_url: resolveCatalogLogo(ollama) };
    const image = document.createElement('img');
    image.src = resolveCatalogLogo(app)!;
    image.addEventListener('error', event => onCatalogLogoError(event, app, placeholder));

    image.dispatchEvent(new Event('error'));
    expect(image.getAttribute('src')).toBe(placeholder);
  });

  it('uses the placeholder directly when there is no bundled logo', () => {
    const app = { library: 'custom', slug_name: 'new-app', logo_url: inline };
    const image = document.createElement('img');
    image.src = inline;
    image.addEventListener('error', event => onCatalogLogoError(event, app, placeholder));

    image.dispatchEvent(new Event('error'));
    expect(image.getAttribute('src')).toBe(placeholder);
  });
});

describe('bundled logo manifest', () => {
  const manifest: {
    apps: Record<string, Record<string, number>>;
    images: string[];
  } = require('../../assets/catalog-logos.json');
  const sources: Record<string, Record<string, string>> = require('../../../../scripts/catalog-logo-sources.json');
  const catalog = require('../../../../../operator/internal/catalog/default-catalog.json');

  it('covers every recorded source and curated SUSE AI app', () => {
    expect(Object.keys(manifest.apps)).toEqual(Object.keys(sources));
    for (const [library, apps] of Object.entries(sources)) {
      expect(Object.keys(manifest.apps[library]).sort()).toEqual(Object.keys(apps).sort());
      for (const slug_name of Object.keys(apps)) {
        expect(resolveCatalogLogo({ library, slug_name }), slug_name).toMatch(/^data:image\/png;base64,/);
      }
    }
    for (const app of catalog['suse-ai']) {
      expect(manifest.apps['suse-ai'][app.slug_name], app.slug_name).toBeTypeOf('number');
    }
  });

  it('contains only bounded raster images that survive catalog URL filtering', () => {
    expect(Buffer.byteLength(`${JSON.stringify(manifest, null, 2)}\n`)).toBeLessThanOrEqual(500 * 1024);
    expect(new Set(manifest.images).size).toBe(manifest.images.length);
    for (const [index, logo] of manifest.images.entries()) {
      expect(browserSafeCatalogLogo(logo), `image ${index}`).toBe(logo);
      const png = Buffer.from(logo.split(',')[1], 'base64');
      expect(png.subarray(0, 8).toString('hex'), `image ${index}`).toBe('89504e470d0a1a0a');
      expect(png.length, `image ${index}`).toBeLessThanOrEqual(32 * 1024);
      expect(png.readUInt32BE(16), `image ${index} width`).toBeGreaterThan(0);
      expect(png.readUInt32BE(16), `image ${index} width`).toBeLessThanOrEqual(128);
      expect(png.readUInt32BE(20), `image ${index} height`).toBeGreaterThan(0);
      expect(png.readUInt32BE(20), `image ${index} height`).toBeLessThanOrEqual(128);
    }
  });
});
