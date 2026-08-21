import { describe, it, expect } from 'vitest';
import fs from 'fs';
import path from 'path';
import yaml from 'js-yaml';

// @rancher/shell/pkg/auto-import.js only generates imports for `l10n/<locale>.yaml`:
//
//   const ext = (f === 'l10n') ? '.yaml' : '';
//
// A locale shipped as .json is silently skipped, no translations reach the
// Rancher i18n store, and every key renders as `%the.key%` in the UI.
const PKG = path.resolve(__dirname, '..');
const L10N = path.join(PKG, 'l10n');

function localeKeys(): Record<string, unknown> {
  const raw = fs.readFileSync(path.join(L10N, 'en-us.yaml'), 'utf8');

  return yaml.load(raw) as Record<string, unknown>;
}

function lookup(obj: unknown, dotted: string): unknown {
  return dotted.split('.').reduce<any>((cur, k) => (cur && cur[k] !== undefined ? cur[k] : undefined), obj);
}

function vueFiles(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, entry.name);

    if (entry.isDirectory() && entry.name !== 'node_modules') {
      vueFiles(p, out);
    } else if (entry.name.endsWith('.vue')) {
      out.push(p);
    }
  }

  return out;
}

describe('l10n locale files', () => {
  it('ships every locale as .yaml so auto-import picks it up', () => {
    const files = fs.readdirSync(L10N);

    expect(files.filter((f) => f.endsWith('.yaml')).length).toBeGreaterThan(0);
    expect(files.filter((f) => f.endsWith('.json'))).toEqual([]);
  });

  it('parses en-us.yaml', () => {
    expect(lookup(localeKeys(), 'suseai.pages.about.title')).toBeTypeOf('string');
  });
});

describe('components using the global t() helper', () => {
  // Components that import useT get `(key, fallback)` and degrade to the
  // fallback string on a miss. Components that do not are using @rancher/shell's
  // global `t(key, args)` — a miss there renders `%key%` (or, when a fallback
  // string is passed into the `args` slot, `%key%(0: A, 1: p, ...)`). Those keys
  // must exist in the locale file.
  const translations = localeKeys();
  const offenders: string[] = [];

  for (const file of vueFiles(PKG)) {
    const src = fs.readFileSync(file, 'utf8');

    if (src.includes('useT')) {
      continue;
    }

    for (const m of src.matchAll(/\bt\(\s*'([a-zA-Z0-9._]+)'/g)) {
      if (typeof lookup(translations, m[1]) !== 'string') {
        offenders.push(`${path.relative(PKG, file)}: ${m[1]}`);
      }
    }
  }

  it('resolves every key it references', () => {
    expect(offenders).toEqual([]);
  });
});
