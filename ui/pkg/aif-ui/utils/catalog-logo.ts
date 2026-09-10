const bundledLogos: {
  apps: Readonly<Record<string, Readonly<Record<string, number>>>>;
  images: readonly string[];
} = require('../assets/catalog-logos.json');

export interface CatalogLogo {
  library?: string;
  slug_name: string;
  logo_url?: string;
}

// Catalog metadata is controlled by chart publishers or an administrator. Do
// not turn an absolute logo URL into an automatic browser request: disconnected
// sites would leak egress attempts and render broken images. A self-contained
// raster data URL remains usable; callers provide a bundled fallback for every
// network URL, including relative paths that could redirect off-cluster.
export function browserSafeCatalogLogo(logo?: string): string | undefined {
  const value = logo?.trim();
  if (!value) return undefined;
  if (/^data:image\/(?:png|gif|jpeg|webp);base64,[a-z0-9+/=]+$/i.test(value)) return value;
  return undefined;
}

function bundledCatalogLogo(app: CatalogLogo): string | undefined {
  if (!app.library || !Object.prototype.hasOwnProperty.call(bundledLogos.apps, app.library)) return undefined;
  const library = bundledLogos.apps[app.library];
  if (!Object.prototype.hasOwnProperty.call(library, app.slug_name)) return undefined;
  const index = library[app.slug_name];
  if (!Object.prototype.hasOwnProperty.call(bundledLogos.images, index)) return undefined;
  return bundledLogos.images[index];
}

// Resolve by stable app identity, independently of repository URLs and curated
// overlays. The manifest contains raster data URLs, so even a first visit with
// an empty browser cache works without reaching a public logo host.
export function resolveCatalogLogo(app: CatalogLogo): string | undefined {
  return browserSafeCatalogLogo(app.logo_url) || bundledCatalogLogo(app);
}

// An inline image can pass the URL check but fail to decode. Try the bundled
// image next, then the caller's placeholder. Stop if the placeholder also fails.
export function onCatalogLogoError(event: Event, app: CatalogLogo, placeholder: string): void {
  const image = event.target as HTMLImageElement | null;
  if (!image) return;
  const current = image.getAttribute('src');
  if (current === placeholder) return;
  const bundled = bundledCatalogLogo(app);
  image.src = bundled && current !== bundled ? bundled : placeholder;
}
