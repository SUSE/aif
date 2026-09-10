#!/usr/bin/env node
// Maintainer-only network operation. Builds and deployments use the committed
// manifest and never run this script.
import { execFile as execFileCallback, spawnSync } from 'node:child_process';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { promisify } from 'node:util';

const execFile = promisify(execFileCallback);
const sources = JSON.parse(await readFile(new URL('./catalog-logo-sources.json', import.meta.url), 'utf8'));
const output = new URL('../pkg/aif-ui/assets/catalog-logos.json', import.meta.url);
const maxSourceBytes = 2 * 1024 * 1024;
const maxLogoBytes = 32 * 1024;
const maxManifestBytes = 500 * 1024;
const imageCommand = ['magick', 'convert'].find(command => spawnSync(command, ['-version']).status === 0);
if (!imageCommand) throw new Error('Install ImageMagick to refresh catalog logos (magick or convert).');

const extensions = {
  'image/png': 'png',
  'image/jpeg': 'jpg',
  'image/gif': 'gif',
  'image/webp': 'webp',
  'image/svg+xml': 'svg',
};
const directory = await mkdtemp(join(tmpdir(), 'aif-catalog-logos-'));

async function downloadLogo(rawURL, index) {
  const url = new URL(rawURL);
  if (url.protocol !== 'https:' || url.username || url.password) {
    throw new Error(`Logo source must be an HTTPS URL without credentials: ${url.hostname}`);
  }
  const response = await fetch(url, { signal: AbortSignal.timeout(20_000) });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  const mime = response.headers.get('content-type')?.split(';')[0].trim();
  const extension = extensions[mime];
  if (!extension) throw new Error(`${url}: unsupported image type ${mime}`);

  const chunks = [];
  let size = 0;
  for await (const chunk of response.body) {
    size += chunk.length;
    if (size > maxSourceBytes) throw new Error(`${url}: image exceeds ${maxSourceBytes} bytes`);
    chunks.push(chunk);
  }
  const input = join(directory, `${index}.${extension}`);
  const converted = join(directory, `${index}.png`);
  await writeFile(input, Buffer.concat(chunks));
  let rasterInput = input;
  if (extension === 'svg') {
    // Use an SVG renderer explicitly: ImageMagick installations commonly disable
    // SVG decoding. No ImageMagick security-policy changes are needed.
    rasterInput = join(directory, `${index}-raster.png`);
    await execFile('inkscape', [
      input, '--export-type=png', '--export-width=128', `--export-filename=${rasterInput}`,
    ], { timeout: 20_000 });
  }
  await execFile(imageCommand, [
    '-limit', 'memory', '64MiB', '-limit', 'map', '128MiB',
    '-background', 'none', `${rasterInput}[0]`,
    '-thumbnail', '128x128>', '-strip',
    '-define', 'png:exclude-chunks=date,time', `PNG32:${converted}`,
  ], { timeout: 20_000 });
  const png = await readFile(converted);
  if (png.length > maxLogoBytes) throw new Error(`${url}: optimized logo exceeds ${maxLogoBytes} bytes`);
  return `data:image/png;base64,${png.toString('base64')}`;
}

try {
  const urls = [...new Set(Object.values(sources).flatMap(library => Object.values(library)))].sort();
  const logos = new Map();
  // Limit concurrent downloads/conversions, and finish each batch before cleanup
  // on error. No partial manifest is written if any source fails.
  for (let offset = 0; offset < urls.length; offset += 4) {
    const batch = await Promise.allSettled(urls.slice(offset, offset + 4).map(async (url, index) => {
      logos.set(url, await downloadLogo(url, offset + index));
    }));
    for (const result of batch) {
      if (result.status === 'rejected') throw result.reason;
    }
    console.log(`Fetched ${Math.min(offset + 4, urls.length)}/${urls.length} logo sources`);
  }
  const images = [];
  const imageIndexes = new Map();
  const apps = Object.fromEntries(Object.entries(sources).sort().map(([library, entries]) => [
    library,
    Object.fromEntries(Object.entries(entries).sort().map(([slug, url]) => {
      const logo = logos.get(url);
      let index = imageIndexes.get(logo);
      if (index === undefined) {
        index = images.length;
        images.push(logo);
        imageIndexes.set(logo, index);
      }
      return [slug, index];
    })),
  ]));
  const manifest = { apps, images };
  const json = `${JSON.stringify(manifest, null, 2)}\n`;
  if (Buffer.byteLength(json) > maxManifestBytes) throw new Error('Logo manifest exceeds the repository limit of 500 KiB');
  await writeFile(output, json);
  console.log(`Updated ${output.pathname} (${Buffer.byteLength(json)} bytes)`);
} finally {
  await rm(directory, { recursive: true, force: true });
}
