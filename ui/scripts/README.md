# Catalog logos

`catalog-logo-sources.json` records the upstream logo URL for each library and
chart name. It includes Application Collection Helm charts and the curated SUSE
AI catalog, including charts with no curated metadata overlay. The Application
Collection entries were collected from its public application listing on
2026-09-08. Logos remain the property of their respective projects.

To add a logo, add its `(library, slug_name)` and HTTPS image URL to the sources
file. Application Collection `/logos/...` paths must use the public
`https://apps.rancher.io` host. Use the chart name, which stays the same when a
repository moves to a private mirror.

With Node.js 20+, ImageMagick (`magick` or `convert`), Inkscape (for SVG sources),
and network access, run from `ui/`:

```sh
node scripts/refresh-catalog-logos.mjs
npm test -- pkg/aif-ui/utils/__tests__/catalog-logo.test.ts
```

The script downloads the recorded sources and converts them to static PNGs of
at most 128 × 128 pixels and 32 KiB each. It strips image metadata, deduplicates
downloads of shared URLs, and stores identical images once in an image array. The
manifest maps each library/chart name to an image index and is capped at the
repository's 500 KiB file limit. It is written only after all sources succeed.
Review and commit `pkg/aif-ui/assets/catalog-logos.json` together with
any source changes. SVG sources are rasterized during this maintenance step;
catalog-provided SVG data URLs remain disallowed at runtime.

The generated manifest is imported into the UI bundle. Normal builds,
deployments, and page loads do not download logos from upstream. Logo resolution
prefers a catalog-provided raster data URL, then the bundled logo, then the
view's existing placeholder. Decode failures advance through the same fallback
chain. External and relative catalog URLs go straight to the bundled lookup;
they never trigger a browser request, even with an empty cache.

An app without a manifest entry can supply an inline raster logo through its
chart or catalog metadata. Otherwise it uses the placeholder until its logo is
added to the manifest. NVIDIA's existing brand assets on the Apps page remain
in use.
