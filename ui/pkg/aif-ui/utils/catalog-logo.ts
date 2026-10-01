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
// Blueprint partner icons are the one deliberate exception; see
// browserSafeBlueprintIcon.
export function browserSafeCatalogLogo(logo?: string): string | undefined {
  const value = logo?.trim();
  if (!value) return undefined;
  if (/^data:image\/(?:png|gif|jpeg|webp);base64,[a-z0-9+/=]+$/i.test(value)) return value;
  return undefined;
}

// Same character class as the CRD, plus a host character right after the
// scheme: WHATWG URL would otherwise read `https:///logo.png` as host logo.png.
const HTTPS_ICON = /^https:\/\/[^\s"'<>/][^\s"'<>]*$/;

// Mirror the CRD exactly (case-sensitive, 16 KB) so the UI never renders an
// icon the API server would refuse to store.
const DATA_ICON = /^data:image\/(png|gif|jpeg|webp);base64,[A-Za-z0-9+/=]+$/;
const ICON_MAX_LENGTH = 16384;

// Names that resolve inside the cluster, the cloud provider or the LAN rather
// than to a partner's public host (e.g. kubernetes.default.svc,
// metadata.google.internal).
const PRIVATE_HOST_SUFFIX = /\.(localhost|local|internal|svc)$/;

// Blueprint partner icons may also be https URLs. Partner blueprints come from
// a SUSE-reviewed repository, and Rancher's partner-extensions index uses https
// icons the same way. Other writers (admin-added catalogs, the API) are bounded
// by the rules below. Plain http, IP literals and private names are rejected
// so a Blueprint cannot point a viewer's browser at mixed content, cloud
// metadata or internal services. BlueprintPartnerLogo renders with no-referrer
// and hides itself on error, so air-gapped sites degrade to the source badge.
// Air-gapped catalogs should use data: URIs. Values are not trimmed so the
// check stays within the CRD pattern (see blueprint-crd-parity.test.ts).
export function browserSafeBlueprintIcon(icon?: string): string | undefined {
  if (!icon || icon.length > ICON_MAX_LENGTH) return undefined;
  if (icon.startsWith('data:')) return DATA_ICON.test(icon) ? icon : undefined;
  if (!HTTPS_ICON.test(icon)) return undefined;

  let host: string;

  try {
    host = new URL(icon).hostname.toLowerCase();
  } catch {
    return undefined;
  }

  // Fully-qualified trailing dots (localhost., localhost..) must not dodge the checks.
  host = host.replace(/\.+$/, '');

  // WHATWG URL normalizes numeric IPv4 forms (e.g. 2130706433) to dotted
  // decimal and keeps brackets on IPv6, so these checks cover IP literals.
  if (!host || host.startsWith('[') || /^[\d.]+$/.test(host)) return undefined;
  if (!host.includes('.') || PRIVATE_HOST_SUFFIX.test(host)) return undefined;

  return icon;
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
