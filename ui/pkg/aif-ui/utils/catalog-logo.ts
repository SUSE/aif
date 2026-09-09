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
